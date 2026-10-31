package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tensai75/nntp"
	"github.com/Tensai75/nntpDirectSearch"
	"github.com/Tensai75/nntpPool"
	"github.com/Tensai75/nzbparser"
	"github.com/Tensai75/subjectparser"
)

// overviewInterval describes one cached message range stored in a single
// gzip file containing the raw overview lines returned by the server.
type overviewInterval struct {
	first   uint
	last    uint
	file    string
	size    int64
	modTime time.Time
}

// cacheGroupDir returns the cache directory for a group. Group names from
// nzblnks/config should already be filesystem-safe, but anything outside
// [a-zA-Z0-9._-] is replaced to be on the safe side.
func cacheGroupDir(root string, group string) string {
	var b strings.Builder
	for _, r := range group {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return filepath.Join(root, b.String())
}

// cacheFileName builds the canonical file name for a cached message range.
func cacheFileName(first uint, last uint) string {
	return fmt.Sprintf("%020d-%020d.ov.gz", first, last)
}

// listCacheIntervals scans a group cache directory and returns all cached
// intervals sorted by first message number.
func listCacheIntervals(dir string) []overviewInterval {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var intervals []overviewInterval
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ov.gz") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".ov.gz")
		firstStr, lastStr, found := strings.Cut(base, "-")
		if !found {
			continue
		}
		first, err1 := strconv.ParseUint(firstStr, 10, 64)
		last, err2 := strconv.ParseUint(lastStr, 10, 64)
		if err1 != nil || err2 != nil || first == 0 || last < first {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		intervals = append(intervals, overviewInterval{
			first:   uint(first),
			last:    uint(last),
			file:    filepath.Join(dir, entry.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].first < intervals[j].first })
	return intervals
}

// messageRange is an inclusive message range [first, last].
type messageRange struct {
	first uint
	last  uint
}

// uncoveredRanges returns the parts of [first, last] not covered by the given
// sorted cache intervals.
func uncoveredRanges(first uint, last uint, intervals []overviewInterval) []messageRange {
	var gaps []messageRange
	next := first
	for _, iv := range intervals {
		if iv.last < next {
			continue
		}
		if iv.first > last {
			break
		}
		if iv.first > next {
			gaps = append(gaps, messageRange{next, min(iv.first-1, last)})
		}
		if iv.last >= next {
			next = iv.last + 1
		}
		if next > last {
			break
		}
	}
	if next <= last {
		gaps = append(gaps, messageRange{next, last})
	}
	return gaps
}

// effectiveReadRanges computes, for each cached file overlapping [first, last],
// the sub-range that still has to be replayed so that overlapping files do not
// contribute the same message twice (earlier files in sort order win).
func effectiveReadRanges(first uint, last uint, intervals []overviewInterval) map[string]messageRange {
	effective := make(map[string]messageRange)
	covered := uint(0)
	for _, iv := range intervals {
		overlapFirst := max(iv.first, first)
		overlapLast := min(iv.last, last)
		if overlapFirst > overlapLast {
			continue
		}
		if covered > 0 && overlapFirst <= covered {
			overlapFirst = covered + 1
		}
		if overlapFirst > overlapLast {
			continue
		}
		effective[iv.file] = messageRange{overlapFirst, overlapLast}
		covered = max(covered, overlapLast)
	}
	return effective
}

// articleNumber extracts the leading message number of a raw overview line.
// Returns 0 when the line does not start with a number.
func articleNumber(line string) uint {
	numStr, _, found := strings.Cut(line, "\t")
	if !found {
		return 0
	}
	num, err := strconv.ParseUint(numStr, 10, 64)
	if err != nil {
		return 0
	}
	return uint(num)
}

// overviewCacheWriter buffers a fetched chunk to disk so it becomes visible in
// the cache only after it was transferred completely (temp file + rename).
type overviewCacheWriter struct {
	tmpPath string
	final   string
	file    *os.File
	gz      *gzip.Writer
	ok      bool
}

func newOverviewCacheWriter(dir string, first uint, last uint) (*overviewCacheWriter, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".fetch-*.tmp")
	if err != nil {
		return nil, err
	}
	return &overviewCacheWriter{
		tmpPath: tmp.Name(),
		final:   filepath.Join(dir, cacheFileName(first, last)),
		file:    tmp,
		gz:      gzip.NewWriter(tmp),
	}, nil
}

func (w *overviewCacheWriter) WriteLine(line string) error {
	_, err := w.gz.Write([]byte(line + "\n"))
	return err
}

// finish flushes, closes and publishes the cache file atomically.
func (w *overviewCacheWriter) finish() error {
	if err := w.gz.Close(); err != nil {
		w.abort()
		return err
	}
	if err := w.file.Close(); err != nil {
		w.abort()
		return err
	}
	if err := os.Rename(w.tmpPath, w.final); err != nil {
		// a concurrent scan may have produced the same file - keep whichever exists
		if _, statErr := os.Stat(w.final); statErr == nil {
			os.Remove(w.tmpPath)
			w.ok = true
			return nil
		}
		w.abort()
		return err
	}
	w.ok = true
	return nil
}

// abort removes the partially written chunk.
func (w *overviewCacheWriter) abort() {
	w.gz.Close()
	w.file.Close()
	os.Remove(w.tmpPath)
}

// compactCacheDir consolidates runs of directly adjacent cache files into a
// single file each. Gzip members can simply be concatenated, so merging is a
// cheap copy - no recompression. Only strictly adjacent ranges
// (prev.last+1 == next.first) are merged; overlapping files are left alone
// because the effective read range logic handles them. maxMergedSize bounds
// the size of a merged file so that eviction can drop the oldest parts of a
// scan instead of having to delete one oversized file covering everything.
func compactCacheDir(dir string, maxMergedSize int64) {
	intervals := listCacheIntervals(dir)
	if len(intervals) < 2 {
		return
	}
	// group strictly-adjacent runs; an overlapping file starts its own group
	// and does not block adjacency between the surrounding files
	var groups [][]overviewInterval
	var groupSizes []int64
	for _, iv := range intervals {
		size := int64(0)
		if fi, err := os.Stat(iv.file); err == nil {
			size = fi.Size()
		}
		joined := -1
		for i := range groups {
			if iv.first == groups[i][len(groups[i])-1].last+1 && groupSizes[i]+size <= maxMergedSize {
				joined = i
				break
			}
		}
		if joined >= 0 {
			groups[joined] = append(groups[joined], iv)
			groupSizes[joined] += size
		} else {
			groups = append(groups, []overviewInterval{iv})
			groupSizes = append(groupSizes, size)
		}
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		tmp, err := os.CreateTemp(dir, ".merge-*.tmp")
		if err != nil {
			return
		}
		failed := false
		for _, iv := range group {
			in, err := os.Open(iv.file)
			if err != nil {
				failed = true
				break
			}
			_, copyErr := io.Copy(tmp, in)
			in.Close()
			if copyErr != nil {
				failed = true
				break
			}
		}
		if failed || tmp.Close() != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			continue
		}
		final := filepath.Join(dir, cacheFileName(group[0].first, group[len(group)-1].last))
		if err := os.Rename(tmp.Name(), final); err != nil {
			os.Remove(tmp.Name())
			continue
		}
		for _, iv := range group {
			os.Remove(iv.file)
		}
	}
}

// cachedScan holds the state of one cached message scan.
type cachedScan struct {
	ctx           context.Context
	ctxCancel     context.CancelFunc
	pool          nntpPool.ConnectionPool
	group         string
	cacheDir      string
	header        string
	headerLower   string
	step          uint
	connections   uint
	retries       uint
	timeout       time.Duration
	maxMergedSize int64

	linesChan       chan string
	results         map[string]map[string]nzbparser.NzbFile
	resultsMutex    sync.Mutex
	reportErrorOnce sync.Once
	lastError       atomic.Value

	messagesRead atomic.Uint64
	bytesRead    atomic.Uint64
	cachedRead   atomic.Uint64
	iteration    func()
}

func newCachedScan(ctx context.Context, pool nntpPool.ConnectionPool, group string, cacheDir string, header string, step uint, connections uint, retries uint, timeoutSeconds uint, maxMergedSize int64, iterationFunc func()) *cachedScan {
	scanCtx, cancel := context.WithCancel(ctx)
	if iterationFunc == nil {
		iterationFunc = func() {}
	}
	return &cachedScan{
		ctx:           scanCtx,
		ctxCancel:     cancel,
		pool:          pool,
		group:         group,
		cacheDir:      cacheDir,
		header:        header,
		headerLower:   strings.ToLower(header),
		step:          step,
		connections:   connections,
		retries:       retries,
		timeout:       time.Duration(timeoutSeconds) * time.Second,
		maxMergedSize: maxMergedSize,
		linesChan:     make(chan string, 10000),
		results:       make(map[string]map[string]nzbparser.NzbFile),
		iteration:     iterationFunc,
	}
}

func (ms *cachedScan) reportError(err error) {
	if err == nil {
		return
	}
	ms.reportErrorOnce.Do(func() {
		ms.lastError.Store(err)
		ms.ctxCancel()
	})
}

// countMessage advances the processed-message counters and progress callback.
func (ms *cachedScan) countMessage() {
	ms.messagesRead.Add(1)
	ms.iteration()
}

// lineScanner mirrors nntpDirectSearch's message processing: parse the
// overview line, match the subject and collect results per release.
func (ms *cachedScan) lineScanner() {
	for line := range ms.linesChan {
		overview, err := nntp.ParseOverviewLine(line)
		if err != nil {
			Log.Debug("Failed to parse message line %q: %v", line, err)
			ms.countMessage()
			continue
		}
		subject := html.UnescapeString(strings.ToValidUTF8(overview.Subject, ""))
		if strings.Contains(strings.ToLower(subject), ms.headerLower) {
			if parsed, err := subjectparser.Parse(subject); err == nil {
				ms.addResult(overview, parsed)
			}
		}
		ms.countMessage()
	}
}

func (ms *cachedScan) addResult(overview nntp.MessageOverview, subject subjectparser.Subject) {
	var date int64
	if date = overview.Date.Unix(); date < 0 {
		date = 0
	}
	poster := strings.ToValidUTF8(overview.From, "")
	escapedGroup := html.EscapeString(ms.group)
	headerHash := nntpDirectSearch.GetMD5Hash(subject.Header + poster + strconv.Itoa(subject.TotalFiles))
	fileHash := nntpDirectSearch.GetMD5Hash(headerHash + subject.Filename + strconv.Itoa(subject.File) + strconv.Itoa(subject.TotalSegments))
	ms.resultsMutex.Lock()
	defer ms.resultsMutex.Unlock()
	if _, ok := ms.results[headerHash]; !ok {
		ms.results[headerHash] = make(map[string]nzbparser.NzbFile)
	}
	if _, ok := ms.results[headerHash][fileHash]; !ok {
		ms.results[headerHash][fileHash] = nzbparser.NzbFile{
			Groups:       []string{escapedGroup},
			Subject:      subject.Subject,
			Poster:       poster,
			Number:       subject.File,
			Filename:     subject.Filename,
			Basefilename: subject.Basefilename,
		}
	}
	file := ms.results[headerHash][fileHash]
	if file.Groups[len(file.Groups)-1] != escapedGroup {
		file.Groups = append(file.Groups, escapedGroup)
	}
	if subject.Segment == 1 {
		file.Subject = subject.Subject
	}
	if int(date) > file.Date {
		file.Date = int(date)
	}
	file.Segments = append(file.Segments, nzbparser.NzbSegment{
		Number: subject.Segment,
		Id:     html.EscapeString(strings.Trim(overview.MessageId, "<>")),
		Bytes:  overview.Bytes,
	})
	ms.results[headerHash][fileHash] = file
}

// dispatchLine applies the same cheap pre-filter the library uses before the
// expensive parse: only lines containing the needle reach the line scanners.
func (ms *cachedScan) dispatchLine(line string) {
	if strings.Contains(strings.ToLower(line), ms.headerLower) {
		select {
		case <-ms.ctx.Done():
		case ms.linesChan <- line:
		}
	} else {
		ms.countMessage()
	}
}

// replayFile streams a cached overview file and feeds the lines of the given
// effective range into the scanner pipeline.
func (ms *cachedScan) replayFile(path string, eff messageRange) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		num := articleNumber(line)
		if num != 0 && (num < eff.first || num > eff.last) {
			continue
		}
		ms.cachedRead.Add(1)
		ms.dispatchLine(line)
		if ms.ctx.Err() != nil {
			return nil
		}
	}
	return scanner.Err()
}

// fetchChunk downloads one message range from the server, writes it into the
// cache and feeds the lines into the scanner pipeline.
func (ms *cachedScan) fetchChunk(first uint, last uint) {
	var lastErr error
	for attempt := uint(0); attempt <= ms.retries; attempt++ {
		if err := ms.fetchChunkOnce(first, last); err != nil {
			lastErr = err
			Log.Debug("Overview fetch for range %d-%d failed (attempt %d/%d): %v", first, last, attempt+1, ms.retries+1, err)
			continue
		}
		return
	}
	ms.reportError(fmt.Errorf("failed to retrieve message overview for range %d-%d after %d attempts: %v", first, last, ms.retries+1, lastErr))
}

func (ms *cachedScan) fetchChunkOnce(first uint, last uint) error {
	// acquire the connection with the non-cancelable context: a scan-level
	// cancel must not close the shared pool (pool.Get closes the pool when
	// the passed context is done)
	conn, err := ms.pool.Get(context.WithoutCancel(ms.ctx))
	if err != nil {
		return err
	}
	// select the group on every acquired connection - pooled connections
	// keep no group state between users
	if _, _, _, err := conn.Group(ms.group); err != nil {
		conn.Close()
		ms.pool.Put(conn)
		return err
	}
	reader, err := conn.OverviewReader(int(first), int(last))
	if err != nil {
		ms.pool.Put(conn)
		return err
	}
	largeReader := bufio.NewReaderSize(reader, 128*1024)

	writer, err := newOverviewCacheWriter(ms.cacheDir, first, last)
	if err != nil {
		ms.pool.Put(conn)
		return err
	}

	type readResult struct {
		line string
		err  error
	}
	resultChan := make(chan readResult, 1)
	done := make(chan struct{})
	go func(r *bufio.Reader, out chan<- readResult, stop <-chan struct{}) {
		defer close(out)
		for {
			line, err := r.ReadString('\n')
			ms.bytesRead.Add(uint64(len(line)))
			trimmedLine := strings.TrimSpace(line)
			select {
			case <-stop:
				return
			case out <- readResult{line: trimmedLine, err: err}:
			}
			// terminate on NNTP terminator/empty line so the connection can be reused
			if err != nil || trimmedLine == "." || trimmedLine == "" {
				return
			}
		}
	}(largeReader, resultChan, done)
	defer close(done)

	timer := time.NewTimer(ms.timeout)
	stopTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	defer stopTimer()

	for {
		timer.Reset(ms.timeout)
		select {
		case <-ms.ctx.Done():
			writer.abort()
			conn.Close()
			ms.pool.Put(conn)
			return ms.ctx.Err()
		case result, ok := <-resultChan:
			stopTimer()
			if !ok || result.err != nil {
				writer.abort()
				conn.Close()
				ms.pool.Put(conn)
				if !ok {
					return fmt.Errorf("connection closed while reading overview")
				}
				return result.err
			}
			if result.line == "." || result.line == "" {
				if err := writer.finish(); err != nil {
					ms.pool.Put(conn)
					return err
				}
				ms.pool.Put(conn)
				return nil
			}
			if err := writer.WriteLine(result.line); err != nil {
				writer.abort()
				conn.Close()
				ms.pool.Put(conn)
				return err
			}
			ms.dispatchLine(result.line)
		case <-timer.C:
			writer.abort()
			conn.Close()
			ms.pool.Put(conn)
			return fmt.Errorf("overview reader timeout for range %d - %d", first, last)
		}
	}
}

// run executes the scan over [firstMessage, lastMessage], serving covered
// parts from the cache and fetching the gaps from the server.
func (ms *cachedScan) run(firstMessage uint, lastMessage uint) ([]*nzbparser.Nzb, error) {

	// start line scanners
	var lineWG sync.WaitGroup
	for range 5 {
		lineWG.Go(func() {
			ms.lineScanner()
		})
	}

	// split the range into cached parts and gaps to fetch
	intervals := listCacheIntervals(ms.cacheDir)
	effective := effectiveReadRanges(firstMessage, lastMessage, intervals)
	gaps := uncoveredRanges(firstMessage, lastMessage, intervals)

	// replay cached files
	for _, iv := range intervals {
		if ms.ctx.Err() != nil {
			break
		}
		eff, ok := effective[iv.file]
		if !ok {
			continue
		}
		if err := ms.replayFile(iv.file, eff); err != nil {
			Log.Warn("Failed to read cache file '%s': %v - the range will be re-fetched", iv.file, err)
			gaps = append(gaps, eff)
		}
	}

	// fetch the remaining gaps in step-sized chunks
	var fetchWG sync.WaitGroup
	limiter := make(chan struct{}, 2*max(uint64(ms.connections), 1))
schedule:
	for _, gap := range gaps {
		for first := gap.first; first <= gap.last; {
			last := min(first+ms.step-1, gap.last)
			select {
			case <-ms.ctx.Done():
				break schedule
			case limiter <- struct{}{}:
				f, l := first, last
				fetchWG.Go(func() {
					defer func() { <-limiter }()
					ms.fetchChunk(f, l)
				})
			}
			first = last + 1
		}
	}
	fetchWG.Wait()
	close(ms.linesChan)
	lineWG.Wait()

	// consolidate adjacent cache files into continuous ranges
	compactCacheDir(ms.cacheDir, ms.maxMergedSize)

	if err := ms.lastError.Load(); err != nil {
		return nil, err.(error)
	}
	if ms.ctx.Err() != nil {
		return nil, ms.ctx.Err()
	}

	// collect results (same assembly as nntpDirectSearch.MessageScanner)
	var nzbFiles []*nzbparser.Nzb
	for _, hit := range ms.results {
		var nzb = &nzbparser.Nzb{}
		for _, files := range hit {
			nzb.Files = append(nzb.Files, files)
		}
		nzbparser.MakeUnique(nzb)
		nzbparser.ScanNzbFile(nzb)
		sort.Sort(nzb.Files)
		for id := range nzb.Files {
			sort.Sort(nzb.Files[id].Segments)
		}
		nzbFiles = append(nzbFiles, nzb)
	}
	return nzbFiles, nil
}

// evictCache removes the oldest cache files until the total size is within the
// configured limit.
func evictCache(root string, maxBytes int64) {
	type cacheFile struct {
		path    string
		size    int64
		modTime time.Time
	}
	var files []cacheFile
	var total int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".ov.gz") {
			return nil
		}
		files = append(files, cacheFile{path, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	if total <= maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, f := range files {
		if total <= maxBytes {
			return
		}
		if err := os.Remove(f.path); err != nil {
			Log.Warn("Failed to evict cache file '%s': %v", f.path, err)
			continue
		}
		Log.Debug("Evicted cache file '%s'", f.path)
		total -= f.size
	}
}
