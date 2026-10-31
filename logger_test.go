package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// debug logging must initialize the log file exactly once, even from
// concurrent goroutines (regression test for the init data race; run
// with -race to verify)
func TestLoggerConcurrentInit(t *testing.T) {
	// redirect the log file into a temp dir and reset globals
	logFilePath = filepath.Join(t.TempDir(), "test.log")
	logInitOnce = sync.Once{}
	logFile = nil
	logWriter = nil
	logger = nil
	args.Debug = true
	defer func() { args.Debug = false; logClose() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			Log.Debug("concurrent debug %d", n)
			Log.Info("concurrent info %d", n)
		}(i)
	}
	wg.Wait()

	if logFile == nil {
		t.Fatal("log file was not initialized")
	}
	if _, err := os.Stat(logFilePath); err != nil {
		t.Errorf("log file %q does not exist: %v", logFilePath, err)
	}
}

// with debug disabled nothing is written to a file
func TestLoggerDebugDisabled(t *testing.T) {
	logFilePath = filepath.Join(t.TempDir(), "test.log")
	logInitOnce = sync.Once{}
	logFile = nil
	logWriter = nil
	logger = nil
	args.Debug = false

	Log.Debug("should not be logged")
	Log.Info("console only")

	if logFile != nil {
		t.Error("log file was initialized despite debug being disabled")
	}
	if _, err := os.Stat(logFilePath); !os.IsNotExist(err) {
		t.Error("log file exists despite debug being disabled")
	}
}

// closing the log while another goroutine writes must not race on the
// underlying *os.File (regression test for the signal-handler logClose;
// run with -race to verify)
func TestLoggerConcurrentClose(t *testing.T) {
	logFilePath = filepath.Join(t.TempDir(), "test.log")
	logInitOnce = sync.Once{}
	logFile = nil
	logWriter = nil
	logger = nil
	args.Debug = true
	defer func() { args.Debug = false }()

	Log.Debug("init")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			Log.Debug("write %d", i)
		}
	}()

	logClose()
	<-done

	// writes after close must not panic - the guarded writer returns
	// os.ErrClosed which the log package discards
	Log.Debug("after close")
}

// logClose without an initialized logger must be a no-op
func TestLogCloseUninitialized(t *testing.T) {
	logFile = nil
	logWriter = nil
	logger = nil
	logClose()
}
