package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tensai75/fslock"
	"github.com/Tensai75/nntpDirectSearch"
	"github.com/Tensai75/nzbparser"
	progressbar "github.com/schollz/progressbar/v3"
)

var directSearch *nntpDirectSearch.DirectSearch
var startDate int64
var endDate int64

var FormatNumberWithApostrophe = func(n uint) string {
	return nntpDirectSearch.FormatNumberWithApostrophe(n)
}

func nzbdirectsearch(engine SearchEngine, name string) error {

	if len(results) > 0 && conf.Directsearch.Skip {
		Log.Info("Results already available. Skipping search based on config settings.")
		return nil
	}

	// validate config and arguments
	if conf.Directsearch.Username == "" || conf.Directsearch.Password == "" {
		return errors.New("no or incomplete credentials for usenet server")
	}
	if conf.Directsearch.Host == "" {
		return errors.New("no usenet server host provided")
	}
	if conf.Directsearch.Port <= 0 || conf.Directsearch.Port > 65535 {
		return errors.New("invalid usenet server port")
	}
	if len(args.Groups) == 0 {
		return errors.New("no groups provided")
	}
	if args.UnixDate == 0 {
		return errors.New("no date provided")
	}
	if conf.Directsearch.Connections <= 0 {
		conf.Directsearch.Connections = 20
	}
	if conf.Directsearch.Hours <= 0 {
		conf.Directsearch.Hours = 12
	}
	if conf.Directsearch.ForwardHours <= 0 {
		conf.Directsearch.ForwardHours = 12
	}
	if conf.Directsearch.Step <= 0 {
		conf.Directsearch.Step = 20000
	}
	if conf.Directsearch.OverviewTimeout <= 0 {
		conf.Directsearch.OverviewTimeout = 5
	}
	if conf.Directsearch.OverviewRetries <= 0 {
		conf.Directsearch.OverviewRetries = 3
	}
	if conf.Directsearch.BoundariesScannerStep <= 0 {
		conf.Directsearch.BoundariesScannerStep = 500
	}
	if conf.Directsearch.BoundariesScannerTolerance <= 0 {
		conf.Directsearch.BoundariesScannerTolerance = 30
	}
	if conf.Directsearch.Cache {
		if conf.Directsearch.CacheSize <= 0 {
			conf.Directsearch.CacheSize = 500
		}
		if conf.Directsearch.CachePath == "" {
			if cacheDir, err := os.UserCacheDir(); err == nil {
				conf.Directsearch.CachePath = filepath.Join(cacheDir, "nzb-monkey-go")
			} else {
				Log.Warn("Failed to determine user cache dir for directsearch cache - cache disabled")
				conf.Directsearch.Cache = false
			}
		}
		if rest, ok := strings.CutPrefix(conf.Directsearch.CachePath, "~/"); ok {
			if home, err := os.UserHomeDir(); err == nil {
				conf.Directsearch.CachePath = filepath.Join(home, rest)
			}
		}
	}

	// set start and end date for search
	startDate = args.UnixDate - int64(conf.Directsearch.Hours*60*60)
	endDate = args.UnixDate + int64(60*60*conf.Directsearch.ForwardHours)
	if !args.IsTimestamp {
		endDate += 60 * 60 * 24
	}

	// acquire lock if configured to allow only one instance of the direct search
	if conf.Directsearch.OneInstanceOnly {
		lock, err := acquireLock()
		if err != nil {
			return fmt.Errorf("failed to acquire lock: %v", err)
		}
		defer lock.Unlock()
	}

	// initialize nntp pool
	if err := initNntpPool(); err != nil {
		return err
	} else {
		defer pool.Close()
	}

	// initialize direct search
	directSearchCtx, directSearchCtxCancel := context.WithCancel(context.Background())
	defer directSearchCtxCancel()
	var err error
	directSearch, err = nntpDirectSearch.New(pool, directSearchCtx)
	if err != nil {
		return err
	}
	directSearchConfig := nntpDirectSearch.DirectSearchConfig{
		Connections:                uint(conf.Directsearch.Connections),
		Step:                       uint(conf.Directsearch.Step),
		OverviewRetries:            uint(conf.Directsearch.OverviewRetries),
		OverviewTimeout:            uint(conf.Directsearch.OverviewTimeout),
		BoundariesScannerStep:      uint(conf.Directsearch.BoundariesScannerStep),
		BoundariesScannerTolerance: uint(conf.Directsearch.BoundariesScannerTolerance),
	}
	err = directSearch.SetConfig(directSearchConfig)
	if err != nil {
		return fmt.Errorf("failed to set direct search config: %v", err)
	}

	// start debug log listener
	go func() {
		for {
			select {
			case <-directSearchCtx.Done():
				return
			case debugLog, ok := <-directSearch.Log:
				if !ok {
					return
				}
				Log.Debug("NNTPDirectSearch: %s", debugLog)
			}
		}
	}()

	// iterate over groups
	var searchInGroupError error
	for i, group := range args.Groups {
		if i > 0 && conf.Directsearch.FirstGroupOnly && searchInGroupError == nil {
			Log.Info("Skipping other groups based on config settings.")
			return nil
		}
		Log.Info("Searching in group '%s' ...", group)

		var nzbFiles []*nzbparser.Nzb
		nzbFiles, searchInGroupError = searchInGroup(group)
		if searchInGroupError != nil {
			Log.Error(searchInGroupError.Error())
			continue
		}
		if len(nzbFiles) == 0 {
			Log.Warn("No result found in group '%s'", group)
			continue
		}
		for _, nzb := range nzbFiles {
			nzbparser.MakeUnique(nzb)
			nzbparser.ScanNzbFile(nzb)
			sort.Sort(nzb.Files)
			for id := range nzb.Files {
				sort.Sort(nzb.Files[id].Segments)
			}
			processResult(nzb, name)
		}
	}
	return nil
}

func searchInGroup(group string) ([]*nzbparser.Nzb, error) {

	// switch to group
	if err := directSearch.SwitchToGroup(group); err != nil {
		return nil, fmt.Errorf("failed to switch to group '%s': %v", group, err)
	}

	// scan for first and last message
	Log.Info("Scanning for first and last message from %s to %s", time.Unix(startDate, 0).Format("02.01.2006 15:04:05 MST"), time.Unix(endDate, 0).Format("02.01.2006 15:04:05 MST"))
	boundaries, err := scanForBoundaries()
	if err != nil {
		return nil, fmt.Errorf("failed to scan for first and last message: %v", err)
	}
	Log.Info("First message ID set to: %s - Average date: %s", FormatNumberWithApostrophe(boundaries.FirstMessage.MessageID), boundaries.FirstMessage.Date.Local().Format("02.01.2006 15:04 MST"))
	Log.Info("Last message ID set to:  %s - Average date: %s", FormatNumberWithApostrophe(boundaries.LastMessage.MessageID), boundaries.LastMessage.Date.Local().Format("02.01.2006 15:04 MST"))

	totalSearchRange := boundaries.LastMessage.MessageID - boundaries.FirstMessage.MessageID + 1
	Log.Debug("Total search range: %s messages", FormatNumberWithApostrophe(totalSearchRange))

	// scan messages for header
	Log.Info("Scanning messages %v to %v (%v messages in total)", FormatNumberWithApostrophe(boundaries.FirstMessage.MessageID), FormatNumberWithApostrophe(boundaries.LastMessage.MessageID), FormatNumberWithApostrophe(totalSearchRange))
	startTime := time.Now()
	nzbFiles, stats, err := scanForHeader(group, boundaries)
	if err != nil {
		return nil, fmt.Errorf("failed to scan messages: %v", err)
	}

	// calculate duration
	duration := time.Since(startTime)
	linesRead := stats.linesRead
	bytesRead := stats.bytesRead
	peakMessagesPerSecond := stats.peakMessagesPerSecond
	peakBytesPerSecond := stats.peakBytesPerSecond
	if conf.Directsearch.Cache {
		Log.Info("Cache: %s of %s messages served from local cache", FormatNumberWithApostrophe(uint(stats.cachedRead)), FormatNumberWithApostrophe(uint(stats.linesRead)))
	}
	formattedDuration := fmt.Sprintf("%02dm %02ds %03dms", int(duration/time.Minute), int((duration%time.Minute)/time.Second), int((duration%time.Second)/time.Millisecond))
	// get lines read
	formattedLinesRead := FormatNumberWithApostrophe(uint(linesRead))
	// calculate average rates (guard against zero duration)
	var averageMessagesPerSecond uint64
	var averageBytesPerSecond uint64
	if duration.Milliseconds() > 0 {
		averageMessagesPerSecond = uint64((float64(linesRead) / float64(duration.Milliseconds()) * 1000))
		averageBytesPerSecond = uint64((float64(bytesRead) / float64(duration.Milliseconds())) * 1000)
	}
	formattedAverageMessagesPerSecond := FormatNumberWithApostrophe(uint(averageMessagesPerSecond))
	averageMbitPerSecond := float64(averageBytesPerSecond) * 8 / 1000000
	// calculate peake messages per second
	var formatedPeakMessagesPerSecond string
	if peakMessagesPerSecond < averageMessagesPerSecond {
		formatedPeakMessagesPerSecond = formattedAverageMessagesPerSecond
	} else {
		formatedPeakMessagesPerSecond = FormatNumberWithApostrophe(uint(peakMessagesPerSecond))
	}
	// calculate peak Mbit/s
	var peakMbitPerSecond float64
	if peakBytesPerSecond < averageBytesPerSecond {
		peakMbitPerSecond = float64(averageBytesPerSecond) * 8 / 1000000
	} else {
		peakMbitPerSecond = float64(peakBytesPerSecond) * 8 / 1000000
	}
	Log.Info("Scan completed in %s with %s messages processed", formattedDuration, formattedLinesRead)
	Log.Info("Average rate: %s messages/s / %.2f Mbit/s", formattedAverageMessagesPerSecond, averageMbitPerSecond)
	Log.Info("Peak rate:    %s messages/s / %.2f Mbit/s", formatedPeakMessagesPerSecond, peakMbitPerSecond)
	fmt.Println()

	return nzbFiles, nil
}

func scanForBoundaries() (nntpDirectSearch.BoundariesScannerResult, error) {

	maxIterations := directSearch.MaxBoundariesScannerIterations

	// setup progress bar
	bar := progressbar.NewOptions(int(maxIterations),
		progressbar.OptionSetDescription("   Scanning ... "),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionThrottle(time.Millisecond*100),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionUseANSICodes(conf.Directsearch.UseANSICodes),
	)
	iterationFunc := func() {
		bar.Add(1)
	}

	// scan for boundaries
	boundaries, err := directSearch.BoundariesScanner(time.Unix(startDate, 0), time.Unix(endDate, 0), iterationFunc)
	bar.Finish()
	fmt.Println()
	if err != nil {
		return boundaries, err
	}
	if boundaries.LastMessage.MessageID < boundaries.FirstMessage.MessageID {
		return boundaries, fmt.Errorf("invalid message range: first message ID %d is greater than last message ID %d", boundaries.FirstMessage.MessageID, boundaries.LastMessage.MessageID)
	}

	return boundaries, nil
}

type scanStats struct {
	linesRead             uint64
	bytesRead             uint64
	cachedRead            uint64
	peakMessagesPerSecond uint64
	peakBytesPerSecond    uint64
}

func scanForHeader(group string, boundaries nntpDirectSearch.BoundariesScannerResult) ([]*nzbparser.Nzb, scanStats, error) {

	var stats scanStats
	firstMessageID := boundaries.FirstMessage.MessageID
	lastMessageID := boundaries.LastMessage.MessageID
	maxIterations := int(lastMessageID - firstMessageID + 1)

	// setup progress bar
	progressbarOptions := []progressbar.Option{
		progressbar.OptionSetDescription("   Scanning ... "),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionThrottle(time.Millisecond * 100),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionUseANSICodes(conf.Directsearch.UseANSICodes),
	}
	if conf.Directsearch.ShowCounter {
		progressbarOptions = append(progressbarOptions, progressbar.OptionShowCount())
	}
	bar := progressbar.NewOptions(maxIterations, progressbarOptions...)
	iterationFunc := func() {
		bar.Add(1)
	}

	// select the counter source and scan implementation
	var getLinesRead, getBytesRead func() uint64
	var nzbFiles []*nzbparser.Nzb
	var scanErr error
	var scan func()
	if conf.Directsearch.Cache {
		scanCtx, scanCancel := context.WithCancel(context.Background())
		defer scanCancel()
		cs := newCachedScan(
			scanCtx,
			pool,
			group,
			cacheGroupDir(conf.Directsearch.CachePath, group),
			args.Header,
			uint(conf.Directsearch.Step),
			uint(conf.Directsearch.Connections),
			uint(conf.Directsearch.OverviewRetries),
			uint(conf.Directsearch.OverviewTimeout),
			// cap merged files at a quarter of the cache limit so eviction
			// can drop the oldest parts of an oversized scan instead of
			// deleting one merged file covering everything
			int64(conf.Directsearch.CacheSize)*1024*1024/4,
			iterationFunc,
		)
		getLinesRead = cs.messagesRead.Load
		getBytesRead = cs.bytesRead.Load
		scan = func() {
			nzbFiles, scanErr = cs.run(firstMessageID, lastMessageID)
			stats.cachedRead = cs.cachedRead.Load()
		}
	} else {
		getLinesRead = func() uint64 { return directSearch.GetLinesRead() }
		getBytesRead = func() uint64 { return directSearch.GetBytesRead() }
		scan = func() {
			nzbFiles, scanErr = directSearch.MessageScanner(args.Header, firstMessageID, lastMessageID, iterationFunc)
		}
	}

	// start peak rate measurement
	Log.Debug("Starting peak rate measurement.")
	ticker := time.NewTicker(1000 * time.Millisecond)
	peakRatesCtx, stopPeakRates := context.WithCancel(context.Background())
	var peakRatesWg sync.WaitGroup
	peakRatesWg.Go(func() {
		measurePeakRates(peakRatesCtx, ticker, getLinesRead, getBytesRead, &stats.peakMessagesPerSecond, &stats.peakBytesPerSecond)
	})

	scan()
	ticker.Stop()
	stopPeakRates()
	peakRatesWg.Wait()

	stats.linesRead = getLinesRead()
	stats.bytesRead = getBytesRead()
	bar.ChangeMax64(int64(stats.linesRead))
	bar.Finish()
	fmt.Println()
	if scanErr != nil {
		return nzbFiles, stats, scanErr
	}
	if conf.Directsearch.Cache {
		evictCache(conf.Directsearch.CachePath, int64(conf.Directsearch.CacheSize)*1024*1024)
	}

	return nzbFiles, stats, nil
}

func measurePeakRates(ctx context.Context, ticker *time.Ticker, getLinesRead func() uint64, getBytesRead func() uint64, peakMessagesPerSecond *uint64, peakBytesPerSecond *uint64) {
	var lastMessages uint64
	var lastBytes uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentMessages := getLinesRead()
			currentBytes := getBytesRead()
			messagesThisSecond := currentMessages - lastMessages
			bytesThisSecond := currentBytes - lastBytes
			if messagesThisSecond > *peakMessagesPerSecond {
				*peakMessagesPerSecond = messagesThisSecond
			}
			if bytesThisSecond > *peakBytesPerSecond {
				*peakBytesPerSecond = bytesThisSecond
			}
			lastMessages = currentMessages
			lastBytes = currentBytes
		}
	}
}

func acquireLock() (*fslock.Lock, error) {
	lockFileName := "directSearch.lock"
	// include the uid so the shared lock file does not collide with one
	// created by another user in the shared temp dir
	if u, err := user.Current(); err == nil && u.Uid != "" {
		lockFileName = fmt.Sprintf("directSearch-%s.lock", u.Uid)
	}
	lockFilePath := filepath.Join(tempPath, lockFileName)
	lock := fslock.New(lockFilePath)
	err := lock.TryLock()
	if err == nil {
		return lock, nil
	}
	if !errors.Is(err, fslock.ErrLocked) {
		return nil, err
	}
	Log.Warn("Another instance of the direct search is already running. Waiting for lock to be released...")
	err = lock.Lock()
	if err != nil {
		return nil, err
	}
	return lock, nil
}
