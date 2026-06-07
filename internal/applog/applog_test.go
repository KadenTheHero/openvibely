package applog

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// captureLog redirects the standard logger output to a buffer for the duration
// of f, then restores it. Returns the captured output.
func captureLog(f func()) string {
	var buf bytes.Buffer
	orig := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0) // suppress date/time prefix so assertions are stable
	defer func() {
		log.SetOutput(orig)
		log.SetFlags(origFlags)
	}()
	f()
	return buf.String()
}

func TestIsDebug_DefaultFalse(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(false)
	if IsDebug() {
		t.Error("expected IsDebug() == false when debug is not enabled")
	}
}

func TestIsDebug_TrueWhenEnabled(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(true)
	if !IsDebug() {
		t.Error("expected IsDebug() == true when debug is enabled")
	}
}

func TestDebugf_SuppressedAtInfoLevel(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(false)
	out := captureLog(func() {
		Debugf("raw token content: %q", "hello world")
	})
	if out != "" {
		t.Errorf("expected no log output at info level, got %q", out)
	}
}

func TestDebugf_EmittedAtDebugLevel(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(true)
	out := captureLog(func() {
		Debugf("raw token content: %q", "hello world")
	})
	if !strings.Contains(out, "raw token content") {
		t.Errorf("expected debug log output, got %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected log output to contain token content, got %q", out)
	}
}

func TestInfof_AlwaysEmittedAtInfoLevel(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(false)
	out := captureLog(func() {
		Infof("[handler] ChatSend created exec=%s status=%s", "exec-abc", "pending")
	})
	if !strings.Contains(out, "exec-abc") {
		t.Errorf("expected Infof to emit at info level, got %q", out)
	}
}

func TestInfof_AlwaysEmittedAtDebugLevel(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(true)
	out := captureLog(func() {
		Infof("[handler] ChatSend created exec=%s status=%s", "exec-xyz", "pending")
	})
	if !strings.Contains(out, "exec-xyz") {
		t.Errorf("expected Infof to emit at debug level, got %q", out)
	}
}

func TestDebugf_FormatArgs(t *testing.T) {
	orig := debugEnabled
	defer func() { debugEnabled = orig }()

	setDebug(true)
	out := captureLog(func() {
		Debugf("[agent-svc] streamingWriter received %d bytes exec=%s", 42, "exec-123")
	})
	if !strings.Contains(out, "42 bytes") {
		t.Errorf("expected formatted output, got %q", out)
	}
	if !strings.Contains(out, "exec-123") {
		t.Errorf("expected exec id in output, got %q", out)
	}
}
