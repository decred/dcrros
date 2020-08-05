// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"decred.org/dcrros/backend"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
	"github.com/matheusd/middlelogger"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	logRotator.Write(p)
	return len(p), nil
}

// Loggers per subsystem.  A single backend logger is created and all subsytem
// loggers created from it will write to the backend.  When adding new
// subsystems, add the subsystem logger variable here and to the
// subsystemLoggers map.
//
// Loggers can not be used before the log rotator has been initialized with a
// log file.  This must be performed early during application startup by calling
// initLogRotator.
var (

	// backendLog is the logging backend used to create all subsystem loggers.
	// The backend must not be used before the log rotator has been initialized,
	// or data races and/or nil pointer dereferences will occur.
	backendLog = slog.NewBackend(logWriter{})

	// logRotator is one of the logging outputs.  It should be closed on
	// application shutdown.
	logRotator *rotator.Rotator

	log     = backendLog.Logger("DROS")
	bdgrLog = backendLog.Logger("BDGR")
	httpLog = backendLog.Logger("HTTP")
)

// Initialize package-global logger variables.
func init() {
	backend.UseLogger(log)
	backend.UseBadgerLogger(bdgrLog)
}

// subsystemLoggers maps each subsystem identifier to its associated logger.
var subsystemLoggers = map[string]slog.Logger{
	"DROS": log,
	"BDGR": bdgrLog,
	"HTTP": httpLog,
}

// initLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func initLogRotator(logFile string) {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 10*1024, false, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}

	logRotator = r
}

// setLogLevel sets the logging level for provided subsystem.  Invalid
// subsystems are ignored.  Uninitialized subsystems are dynamically created as
// needed.
func setLogLevel(subsystemID string, logLevel string) {
	// Ignore invalid subsystems.
	logger, ok := subsystemLoggers[subsystemID]
	if !ok {
		return
	}

	// Defaults to info if the log level is invalid.
	level, _ := slog.LevelFromString(logLevel)
	logger.SetLevel(level)
}

// setLogLevels sets the log level for all subsystem loggers to the passed
// level.  It also dynamically creates the subsystem loggers as needed, so it
// can be used to initialize the logging system.
func setLogLevels(logLevel string) {
	// Configure all sub-systems with the new logging level.  Dynamically
	// create loggers as needed.
	for subsystemID := range subsystemLoggers {
		setLogLevel(subsystemID, logLevel)
	}
}

// requestLogger is an http logger middleware that logs using a local slog
// instance.
type requestLogger struct{}

type niceBytes int64

func (n niceBytes) String() string {
	switch {
	case n < 1e3:
		return fmt.Sprintf("%dB", n)
	case n < 1e6:
		return fmt.Sprintf("%.2fkB", float64(n)/1e3)
	case n < 1e9:
		return fmt.Sprintf("%.2fMB", float64(n)/1e6)
	case n < 1e12:
		return fmt.Sprintf("%.2fGB", float64(n)/1e9)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// LogRequest is part of the Logger interface.
func (l requestLogger) LogRequest(ld middlelogger.LogData) {
	httpLog.Debugf(
		"%s %s %d %s %s",
		ld.R.Method,
		ld.R.RequestURI,
		ld.Status,
		ld.TotalTime,
		niceBytes(ld.BytesWritten),
	)
}

// LogPanic is part of the PanicLogger interface.
func (l requestLogger) LogPanic(ld middlelogger.LogData, err interface{}) {
	httpLog.Errorf(
		"%s %s %d %s %s (PANIC %v)",
		ld.R.Method,
		ld.R.RequestURI,
		ld.Status,
		ld.TotalTime,
		niceBytes(ld.BytesWritten),
		err,
	)
	httpLog.Errorf(string(debug.Stack()))
}

func (l requestLogger) Cutoff(*http.Request) time.Duration {
	return time.Second
}

func (l requestLogger) MultipleLogs(*http.Request) bool {
	return true
}

func (l requestLogger) LogSlowRequest(ld middlelogger.LogData, i int) {
	httpLog.Infof(
		"%s %s %d %s %s (slow %d)",
		ld.R.Method,
		ld.R.RequestURI,
		ld.Status,
		ld.TotalTime,
		niceBytes(ld.BytesWritten),
		i,
	)
}
