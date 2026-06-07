// Package applog provides a minimal process-wide log-level gate that reads
// OPENVIBELY_LOG_LEVEL at startup. The default level is "info"; setting the
// env var to "debug" (case-insensitive) enables debug-level logging.
//
// Use Infof for all normal operational log lines (errors, status, state
// transitions, CRUD results). Use Debugf for lines that are only useful when
// actively debugging — especially high-frequency or content-carrying lines.
package applog

import (
	"log"
	"os"
	"strings"
)

// debugEnabled is set once at process start from OPENVIBELY_LOG_LEVEL.
// Tests may call setDebug to override it within the package.
var debugEnabled = strings.EqualFold(
	strings.TrimSpace(os.Getenv("OPENVIBELY_LOG_LEVEL")), "debug",
)

// IsDebug reports whether debug-level logging is currently enabled.
func IsDebug() bool { return debugEnabled }

// Infof emits a formatted log message at info level.
// Info is always emitted regardless of the configured log level.
func Infof(format string, args ...any) {
	log.Printf(format, args...)
}

// Debugf emits a formatted log message only when debug level is active.
// Use this for high-frequency or content-carrying lines (e.g. raw LLM token
// chunks, per-action-check traces) that are too noisy for normal operation.
func Debugf(format string, args ...any) {
	if debugEnabled {
		log.Printf(format, args...)
	}
}

// setDebug allows test code within this package to override the level.
func setDebug(v bool) { debugEnabled = v }
