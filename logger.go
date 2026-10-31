package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/acarl005/stripansi"
	"github.com/fatih/color"
)

type Logger struct {
	Error func(string, ...interface{})
	Warn  func(string, ...interface{})
	Info  func(string, ...interface{})
	Succ  func(string, ...interface{})
	Debug func(string, ...interface{})
}

// fileWriter serializes writes to the log file so that logClose can
// safely close it while another goroutine (e.g. the signal handler)
// may still be writing
type fileWriter struct {
	mu     sync.Mutex
	f      *os.File
	closed bool
}

func (w *fileWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	return w.f.Write(b)
}

func (w *fileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return w.f.Close()
}

// global error logger variables
var (
	logFile     *os.File
	logWriter   *fileWriter
	logger      *log.Logger
	logInitOnce sync.Once
	Log         = Logger{
		Error: logError,
		Warn:  logWarn,
		Info:  logInfo,
		Succ:  logSuccess,
		Debug: logDebug,
	}
)

// error logger function
func logError(logText string, vars ...interface{}) {
	logEntry("error", logText, vars...)
}

func logWarn(logText string, vars ...interface{}) {
	logEntry("warn", logText, vars...)
}

func logInfo(logText string, vars ...interface{}) {
	logEntry("info", logText, vars...)
}

func logSuccess(logText string, vars ...interface{}) {
	logEntry("success", logText, vars...)
}

func logDebug(logText string, vars ...interface{}) {
	logEntry("debug", logText, vars...)
}

func logEntry(logType string, logText string, vars ...interface{}) {

	// init logger once if debug is enabled
	if args.Debug {
		logInitOnce.Do(func() {
			initLogger(logFilePath)
		})
	}

	// log error
	if logType == "error" {
		if args.Debug {
			logger.Printf("ERROR: %s\n", stripansi.Strip(strings.Trim(fmt.Sprintf(logText, vars...), "\n")))
		}
		color.Set(color.FgRed)
		fmt.Printf("   ERROR:    %s\n", fmt.Sprintf(logText, vars...))
		color.Unset()
	}

	// log warn
	if logType == "warn" {
		if args.Debug {
			logger.Printf("WARNING: %s\n", stripansi.Strip(strings.Trim(fmt.Sprintf(logText, vars...), "\n")))
		}
		color.Set(color.FgYellow)
		fmt.Printf("   WARNING:  %s\n", fmt.Sprintf(logText, vars...))
		color.Unset()
	}

	// log info
	if logType == "info" {
		if args.Debug {
			logger.Printf("INFO: %s\n", stripansi.Strip(strings.Trim(fmt.Sprintf(logText, vars...), "\n")))
		}
		fmt.Printf("   %s\n", fmt.Sprintf(logText, vars...))
	}

	// log success
	if logType == "success" {
		if args.Debug {
			logger.Printf("SUCCESS: %s\n", stripansi.Strip(strings.Trim(fmt.Sprintf(logText, vars...), "\n")))
		}
		color.Set(color.FgGreen)
		fmt.Printf("   SUCCESS:  %s\n", fmt.Sprintf(logText, vars...))
		color.Unset()
	}

	// log debug
	if logType == "debug" && args.Debug {
		logger.Printf("DEBUG: %s\n", stripansi.Strip(strings.Trim(fmt.Sprintf(logText, vars...), "\n")))
	}

}

func initLogger(file string) {
	var err error
	if logFile, err = os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		logWriter = &fileWriter{f: logFile}
		logger = log.New(logWriter, "", log.Ldate|log.Ltime)
		logger.Printf("INFO: %s %s started", appName, appVersion)
	} else {
		color.Set(color.FgRed)
		fmt.Printf("   ERROR: Fatal error while opening log file '%s': %s\n", file, err.Error())
		color.Unset()
		exit(1)
	}
}

func logClose() {
	// clean up
	if logWriter != nil {
		logWriter.Close()
	}
}
