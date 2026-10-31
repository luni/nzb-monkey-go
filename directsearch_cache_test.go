package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tensai75/nntpPool"
)

func TestCacheFileName(t *testing.T) {
	name := cacheFileName(100, 200)
	if name != "00000000000000000100-00000000000000000200.ov.gz" {
		t.Fatalf("unexpected cache file name: %s", name)
	}
}

func TestCacheGroupDirSanitizes(t *testing.T) {
	dir := cacheGroupDir("/tmp/root", "alt.binaries.a51")
	if filepath.Base(dir) != "alt.binaries.a51" {
		t.Fatalf("unexpected group dir: %s", dir)
	}
	dir = cacheGroupDir("/tmp/root", "we:ird/gr*up")
	if filepath.Base(dir) != "we_ird_gr_up" {
		t.Fatalf("unexpected sanitized group dir: %s", dir)
	}
}

func writeCacheFile(t *testing.T, dir string, first uint, last uint, lines []string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, cacheFileName(first, last))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	for _, l := range lines {
		if _, err := gz.Write([]byte(l + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListCacheIntervals(t *testing.T) {
	dir := t.TempDir()
	writeCacheFile(t, dir, 200, 300, nil)
	writeCacheFile(t, dir, 100, 150, nil)
	// non-matching files are ignored
	if err := os.WriteFile(filepath.Join(dir, "garbage.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad-name.ov.gz"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	ivs := listCacheIntervals(dir)
	if len(ivs) != 2 {
		t.Fatalf("expected 2 intervals, got %d", len(ivs))
	}
	if ivs[0].first != 100 || ivs[0].last != 150 || ivs[1].first != 200 || ivs[1].last != 300 {
		t.Fatalf("unexpected intervals: %+v", ivs)
	}
}

func TestUncoveredRanges(t *testing.T) {
	tests := []struct {
		name      string
		first     uint
		last      uint
		intervals []overviewInterval
		want      []messageRange
	}{
		{"no cache", 100, 200, nil, []messageRange{{100, 200}}},
		{"fully covered", 100, 200, []overviewInterval{{first: 50, last: 250}}, nil},
		{"exact cover", 100, 200, []overviewInterval{{first: 100, last: 200}}, nil},
		{"head gap", 100, 200, []overviewInterval{{first: 150, last: 200}}, []messageRange{{100, 149}}},
		{"tail gap", 100, 200, []overviewInterval{{first: 100, last: 150}}, []messageRange{{151, 200}}},
		{"middle covered", 100, 300, []overviewInterval{{first: 150, last: 250}}, []messageRange{{100, 149}, {251, 300}}},
		{"two files", 100, 300, []overviewInterval{{first: 100, last: 150}, {first: 250, last: 300}}, []messageRange{{151, 249}}},
		{"overlapping files", 100, 300, []overviewInterval{{first: 100, last: 250}, {first: 200, last: 280}}, []messageRange{{281, 300}}},
		{"disjoint file before", 100, 200, []overviewInterval{{first: 10, last: 50}}, []messageRange{{100, 200}}},
		{"adjacent files", 100, 300, []overviewInterval{{first: 100, last: 200}, {first: 201, last: 300}}, nil},
	}
	for _, tc := range tests {
		got := uncoveredRanges(tc.first, tc.last, tc.intervals)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: expected %d gaps, got %v", tc.name, len(tc.want), got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: gap %d = %v, want %v", tc.name, i, got[i], tc.want[i])
			}
		}
	}
}

func TestEffectiveReadRanges(t *testing.T) {
	intervals := []overviewInterval{
		{first: 100, last: 200, file: "a"},
		{first: 150, last: 250, file: "b"},
		{first: 400, last: 500, file: "c"},
	}
	eff := effectiveReadRanges(120, 260, intervals)
	if len(eff) != 2 {
		t.Fatalf("expected 2 effective ranges, got %v", eff)
	}
	if eff["a"] != (messageRange{120, 200}) {
		t.Fatalf("unexpected effective range for a: %v", eff["a"])
	}
	if eff["b"] != (messageRange{201, 250}) {
		t.Fatalf("unexpected effective range for b (shadowed part skipped): %v", eff["b"])
	}
}

func TestArticleNumber(t *testing.T) {
	if n := articleNumber("12345\tsubj\tfrom\tdate\tid\tref\t10\t5"); n != 12345 {
		t.Fatalf("expected 12345, got %d", n)
	}
	if n := articleNumber("no-tab-here"); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
	if n := articleNumber("abc\tsubj"); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestOverviewCacheWriter(t *testing.T) {
	dir := t.TempDir()
	w, err := newOverviewCacheWriter(dir, 100, 150)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteLine("100\tsub\tfrom\tdate\tid\tref\t1\t1"); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	// file is visible in the cache listing
	ivs := listCacheIntervals(dir)
	if len(ivs) != 1 || ivs[0].first != 100 || ivs[0].last != 150 {
		t.Fatalf("cache file not registered: %v", ivs)
	}
	// content roundtrips through gzip
	f, err := os.Open(ivs[0].file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(gz)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 1 || lines[0] != "100\tsub\tfrom\tdate\tid\tref\t1\t1" {
		t.Fatalf("unexpected cached lines: %v", lines)
	}
}

func TestOverviewCacheWriterAbort(t *testing.T) {
	dir := t.TempDir()
	w, err := newOverviewCacheWriter(dir, 100, 150)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteLine("100\tsub\tfrom\tdate\tid\tref\t1\t1"); err != nil {
		t.Fatal(err)
	}
	w.abort()
	// no temp or cache files left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Fatalf("unexpected file after abort: %s", e.Name())
	}
}

func TestEvictCache(t *testing.T) {
	dir := t.TempDir()
	old := writeCacheFile(t, dir, 100, 150, []string{"100\ta\tb\tc\td\te\t1\t1"})
	time.Sleep(10 * time.Millisecond)
	newer := writeCacheFile(t, dir, 200, 250, []string{"200\ta\tb\tc\td\te\t1\t1"})
	info, err := os.Stat(newer)
	if err != nil {
		t.Fatal(err)
	}
	// cap equal to the newest file's size: oldest must go, newest stays
	evictCache(dir, info.Size())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("oldest cache file was not evicted")
	}
	ivs := listCacheIntervals(dir)
	if len(ivs) != 1 || ivs[0].first != 200 {
		t.Fatalf("unexpected remaining cache: %v", ivs)
	}
	// a generous cap keeps everything
	evictCache(dir, 1<<30)
	if len(listCacheIntervals(dir)) != 1 {
		t.Fatal("cache file evicted despite sufficient cap")
	}
}

func TestCompactCacheDir(t *testing.T) {
	dir := t.TempDir()
	l1 := writeCacheFile(t, dir, 100, 150, []string{"100\ta\tb\tc\td\te\t1\t1"})
	l2 := writeCacheFile(t, dir, 151, 200, []string{"151\ta\tb\tc\td\te\t1\t1"})
	l3 := writeCacheFile(t, dir, 201, 250, []string{"201\ta\tb\tc\td\te\t1\t1"})
	// overlapping file must NOT be merged into the run
	writeCacheFile(t, dir, 140, 160, []string{"140\ta\tb\tc\td\te\t1\t1"})

	compactCacheDir(dir)

	ivs := listCacheIntervals(dir)
	if len(ivs) != 2 {
		t.Fatalf("expected 2 files after compaction, got %v", ivs)
	}
	var merged *overviewInterval
	for i, iv := range ivs {
		if iv.first == 100 && iv.last == 250 {
			merged = &ivs[i]
		}
	}
	if merged == nil {
		t.Fatalf("adjacent files were not merged: %v", ivs)
	}
	for _, f := range []string{l1, l2, l3} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("source file %s still exists after merge", f)
		}
	}
	// merged gzip stream contains all three members' lines
	f, err := os.Open(merged.file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(gz)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines from merged stream, got %v", lines)
	}
}

func TestCompactCacheDirOverlapping(t *testing.T) {
	dir := t.TempDir()
	writeCacheFile(t, dir, 100, 150, []string{"100\ta\tb\tc\td\te\t1\t1"})
	writeCacheFile(t, dir, 140, 200, []string{"140\ta\tb\tc\td\te\t1\t1"})

	compactCacheDir(dir)

	// overlapping files are not strictly adjacent -> untouched
	if got := len(listCacheIntervals(dir)); got != 2 {
		t.Fatalf("expected overlapping files untouched, got %d files", got)
	}
}

// fakeNNTPServer implements the minimal NNTP subset used by the pool: greeting,
// AUTHINFO, MODE, GROUP, DATE and OVER for a fixed message range.
type fakeNNTPServer struct {
	listener    net.Listener
	overRanges  atomic.Int64
	overLog     chan string
	overHandler func(first, last int) []string
}

func newFakeNNTPServer(t *testing.T, overHandler func(first, last int) []string) *fakeNNTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeNNTPServer{
		listener:    listener,
		overLog:     make(chan string, 100),
		overHandler: overHandler,
	}
	go s.serve()
	t.Cleanup(func() {
		s.listener.Close()
	})
	return s
}

func (s *fakeNNTPServer) port() uint32 {
	return uint32(s.listener.Addr().(*net.TCPAddr).Port)
}

func (s *fakeNNTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeNNTPServer) handleConn(conn net.Conn) {
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "200 fake NNTP server ready\r\n"); err != nil {
		return
	}
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		cmd := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(upper, "AUTHINFO USER"):
			fmt.Fprintf(conn, "381 password required\r\n")
		case strings.HasPrefix(upper, "AUTHINFO PASS"):
			fmt.Fprintf(conn, "281 authenticated\r\n")
		case strings.HasPrefix(upper, "AUTHINFO"):
			fmt.Fprintf(conn, "281 authenticated\r\n")
		case strings.HasPrefix(upper, "MODE"):
			fmt.Fprintf(conn, "200 hello\r\n")
		case strings.HasPrefix(upper, "DATE"):
			fmt.Fprintf(conn, "111 %s\r\n", time.Now().UTC().Format("20060102150405"))
		case strings.HasPrefix(upper, "CAPABILITIES"):
			fmt.Fprintf(conn, "101 capabilities\r\n.\r\n")
		case strings.HasPrefix(upper, "GROUP"):
			fmt.Fprintf(conn, "211 1000 1 1000 %s\r\n", strings.TrimSpace(cmd[5:]))
		case strings.HasPrefix(upper, "OVER ") || strings.HasPrefix(upper, "XOVER "):
			parts := strings.Fields(cmd)
			if len(parts) < 2 {
				fmt.Fprintf(conn, "502 error\r\n")
				continue
			}
			s.overRanges.Add(1)
			s.overLog <- parts[1]
			rangeParts := strings.SplitN(parts[1], "-", 2)
			first, _ := strconv.Atoi(rangeParts[0])
			last := first
			if len(rangeParts) == 2 {
				last, _ = strconv.Atoi(rangeParts[1])
			}
			fmt.Fprintf(conn, "224 overview follows\r\n")
			for _, line := range s.overHandler(first, last) {
				fmt.Fprintf(conn, "%s\r\n", line)
			}
			fmt.Fprintf(conn, ".\r\n")
		case strings.HasPrefix(upper, "QUIT"):
			fmt.Fprintf(conn, "205 bye\r\n")
			return
		default:
			fmt.Fprintf(conn, "502 unsupported\r\n")
		}
	}
}

// overviewLine builds a minimal RFC 3977 overview line.
func overviewLine(num int, subject string, from string, msgid string, bytes int) string {
	return fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t\t%d\t10", num, subject, from, "Sat, 25 Jan 2025 12:00:00 +0100", msgid, bytes)
}

func testPool(t *testing.T, port uint32) nntpPool.ConnectionPool {
	t.Helper()
	pool, err := nntpPool.New(&nntpPool.Config{
		Host:         "127.0.0.1",
		Port:         port,
		SSL:          false,
		User:         "user",
		Pass:         "pass",
		MaxConns:     4,
		ConnWaitTime: 2 * time.Second,
		IdleTimeout:  5 * time.Second,
		HealthCheck:  false,
	}, 0)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

func TestCachedScanFetchesAndCaches(t *testing.T) {
	// serve 300 messages; two of them contain the needle
	server := newFakeNNTPServer(t, func(first, last int) []string {
		var lines []string
		for i := first; i <= last; i++ {
			subject := fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i)
			if i == 120 || i == 260 {
				subject = fmt.Sprintf("cache-needle [01/01] - \"cache-needle-%d.part01.rar\" yEnc (1/1)", i)
			}
			lines = append(lines, overviewLine(i, subject, "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
		}
		return lines
	})
	pool := testPool(t, server.port())
	cacheDir := filepath.Join(t.TempDir(), "alt.binaries.test")

	scan := newCachedScan(context.Background(), pool, "alt.binaries.test", cacheDir, "cache-needle", 100, 4, 0, 5, nil)
	nzbFiles, err := scan.run(100, 300)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if server.overRanges.Load() == 0 {
		t.Fatal("expected server OVER requests on first scan")
	}
	if len(nzbFiles) != 1 || len(nzbFiles[0].Files) != 2 {
		t.Fatalf("expected 1 NZB with 2 files, got %+v", nzbFiles)
	}
	if scan.messagesRead.Load() != 201 {
		t.Fatalf("expected 201 messages processed, got %d", scan.messagesRead.Load())
	}
	// the fetched ranges are now cached
	ivs := listCacheIntervals(cacheDir)
	total := uint(0)
	for _, iv := range ivs {
		total += iv.last - iv.first + 1
	}
	if total < 201 {
		t.Fatalf("expected >= 201 cached messages, got %d", total)
	}
}

func TestCachedScanServesFromCache(t *testing.T) {
	server := newFakeNNTPServer(t, func(first, last int) []string {
		var lines []string
		for i := first; i <= last; i++ {
			subject := fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i)
			if i == 120 {
				subject = "first-needle [01/01] - \"first-needle.part01.rar\" yEnc (1/1)"
			}
			if i == 140 {
				subject = "second-needle [01/01] - \"second-needle.part01.rar\" yEnc (1/1)"
			}
			lines = append(lines, overviewLine(i, subject, "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
		}
		return lines
	})
	pool := testPool(t, server.port())
	cacheDir := filepath.Join(t.TempDir(), "alt.binaries.test")

	// first run fills the cache
	scan := newCachedScan(context.Background(), pool, "alt.binaries.test", cacheDir, "first-needle", 100, 4, 0, 5, nil)
	if _, err := scan.run(100, 200); err != nil {
		t.Fatalf("first scan failed: %v", err)
	}
	server.overRanges.Store(0)

	// second run over the same range with a DIFFERENT needle - must be served
	// entirely from the cache without any new OVER request
	scan2 := newCachedScan(context.Background(), pool, "alt.binaries.test", cacheDir, "second-needle", 100, 4, 0, 5, nil)
	nzbFiles, err := scan2.run(100, 200)
	if err != nil {
		t.Fatalf("cached scan failed: %v", err)
	}
	if got := server.overRanges.Load(); got != 0 {
		t.Fatalf("expected 0 OVER requests on cache hit, got %d", got)
	}
	if len(nzbFiles) != 1 || len(nzbFiles[0].Files) != 1 {
		t.Fatalf("expected the second needle's file from cache, got %+v", nzbFiles)
	}
	if scan2.cachedRead.Load() != 101 {
		t.Fatalf("expected 101 cache-served messages, got %d", scan2.cachedRead.Load())
	}
	if nzbFiles[0].Files[0].Filename != "second-needle.part01.rar" {
		t.Fatalf("unexpected file from cache: %s", nzbFiles[0].Files[0].Filename)
	}
}

func TestCachedScanPartialCoverage(t *testing.T) {
	var requested []string
	server := newFakeNNTPServer(t, func(first, last int) []string {
		var lines []string
		for i := first; i <= last; i++ {
			subject := fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i)
			lines = append(lines, overviewLine(i, subject, "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
		}
		return lines
	})
	pool := testPool(t, server.port())
	cacheDir := filepath.Join(t.TempDir(), "alt.binaries.test")

	// seed the cache with [100,150]
	var seed []string
	for i := 100; i <= 150; i++ {
		seed = append(seed, overviewLine(i, fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i), "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
	}
	writeCacheFile(t, cacheDir, 100, 150, seed)

	scan := newCachedScan(context.Background(), pool, "alt.binaries.test", cacheDir, "nothing-matches", 100, 4, 0, 5, nil)
	if _, err := scan.run(100, 250); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	// drain the request log after the scan completed
	drain := true
	for drain {
		select {
		case r := <-server.overLog:
			requested = append(requested, r)
		default:
			drain = false
		}
	}
	// only the uncovered range [151,250] may have been fetched (step=100 -> one chunk)
	if len(requested) != 1 || requested[0] != "151-250" {
		t.Fatalf("expected exactly one OVER request for 151-250, got %v", requested)
	}
	if scan.cachedRead.Load() != 51 {
		t.Fatalf("expected 51 cache-served messages, got %d", scan.cachedRead.Load())
	}
}

func TestCachedScanNoDuplicateSegmentsOnOverlap(t *testing.T) {
	server := newFakeNNTPServer(t, func(first, last int) []string {
		return nil // never reached - everything must come from cache
	})
	pool := testPool(t, server.port())
	cacheDir := filepath.Join(t.TempDir(), "alt.binaries.test")

	// two overlapping files covering [100,150] both contain the needle at 120
	mkLines := func(from, to int) []string {
		var lines []string
		for i := from; i <= to; i++ {
			subject := fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i)
			if i == 120 {
				subject = "dup-needle [01/01] - \"dup-needle.part01.rar\" yEnc (1/1)"
			}
			lines = append(lines, overviewLine(i, subject, "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
		}
		return lines
	}
	writeCacheFile(t, cacheDir, 100, 150, mkLines(100, 150))
	writeCacheFile(t, cacheDir, 120, 160, mkLines(120, 160))

	scan := newCachedScan(context.Background(), pool, "alt.binaries.test", cacheDir, "dup-needle", 100, 4, 0, 5, nil)
	nzbFiles, err := scan.run(100, 160)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(nzbFiles) != 1 || len(nzbFiles[0].Files) != 1 {
		t.Fatalf("expected exactly 1 file, got %+v", nzbFiles)
	}
	// the message at 120 must appear only once despite being in both files
	if got := len(nzbFiles[0].Files[0].Segments); got != 1 {
		t.Fatalf("expected 1 segment, got %d", got)
	}
	if got := server.overRanges.Load(); got != 0 {
		t.Fatalf("expected no OVER requests, got %d", got)
	}
}
