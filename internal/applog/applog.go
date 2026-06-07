// Package applog provides a minimal process-wide log-level gate that reads
// OPENVIBELY_LOG_LEVEL at startup. The default level is "info"; setting the
// env var to "debug" (case-insensitive) enables debug-level logging.
//
// Only noisy per-token / raw-content log lines should use Debugf or IsDebug.
// Structured operational log lines (errors, status, counts, durations) continue
// to use the standard log package directly and are always emitted.
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

// Debugf emits a formatted log message only when debug level is active.
// Use this for high-frequency or content-carrying lines (e.g. raw LLM token
// chunks) that are too noisy for normal info-level operation.
func Debugf(format string, args ...any) {
	if debugEnabled {
		log.Printf(format, args...)
	}
}

// setDebug allows test code within this package to override the level.
func setDebug(v bool) { debugEnabled = v }
