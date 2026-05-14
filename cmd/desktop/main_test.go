package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/config"
)

// TestEnsureDesktopPATHMakesGoLocatable reproduces the "bash: go: command not
// found" runtime issue: macOS GUI launches of Wails apps inherit launchd's
// minimal PATH which excludes /usr/local/go/bin, /opt/homebrew/bin, and
// $HOME/go/bin. After EnsureDesktopPATH runs at startup, `go` (when installed
// in a standard location) must be locatable by subprocesses.
func TestEnsureDesktopPATHMakesGoLocatable(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go not installed on host; skipping: %v", err)
	}
	goDir, err := filepath.EvalSymlinks(filepath.Dir(goPath))
	if err != nil {
		goDir = filepath.Dir(goPath)
	}

	// This test is meaningful only when the host's Go install lives in one of
	// the standard candidate dirs our bootstrap knows about. Otherwise the
	// fix would correctly rely on the user's shell init, which is out of
	// scope for an in-process unit test.
	standard := false
	for _, c := range []string{
		"/usr/local/go/bin",
		"/opt/homebrew/bin",
		filepath.Join(os.Getenv("HOME"), "go", "bin"),
	} {
		if resolved, err := filepath.EvalSymlinks(c); err == nil && resolved == goDir {
			standard = true
			break
		}
		if filepath.Clean(c) == goDir {
			standard = true
			break
		}
	}
	if !standard {
		t.Skipf("host go (%s) is not in a standard desktop-PATH candidate dir; skipping", goDir)
	}

	// Simulate a Wails GUI launch on macOS: launchd's minimal PATH.
	minimal := "/usr/bin" + string(os.PathListSeparator) + "/bin" +
		string(os.PathListSeparator) + "/usr/sbin" + string(os.PathListSeparator) + "/sbin"
	t.Setenv("PATH", minimal)

	if _, err := exec.LookPath("go"); err == nil {
		t.Skipf("host has `go` available via minimal PATH=%q; cannot reproduce", minimal)
	}

	// Run the desktop PATH bootstrap exactly as cmd/desktop/main.go does.
	newPATH := config.EnsureDesktopPATH()
	if !strings.Contains(newPATH, goDir) {
		t.Fatalf("expected EnsureDesktopPATH to add %q to PATH; got %q", goDir, newPATH)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("after EnsureDesktopPATH, expected `go` to be locatable; PATH=%q err=%v", os.Getenv("PATH"), err)
	}
}

func TestRunDesktopLaunchesNativeWindow(t *testing.T) {
	cfg := &config.Config{Mode: config.ModeDesktop}

	started := false
	launched := false
	shutdownCalled := false
	launchedURL := ""

	err := runDesktop(
		cfg,
		func(context.Context, *config.Config) (*desktopBackend, error) {
			started = true
			return &desktopBackend{
				BaseURL: "http://127.0.0.1:43210",
				Shutdown: func() {
					shutdownCalled = true
				},
			}, nil
		},
		func(url string, onShutdown func()) error {
			launched = true
			launchedURL = url
			onShutdown()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runDesktop returned error: %v", err)
	}
	if !started {
		t.Fatalf("expected backend start to be called")
	}
	if !launched {
		t.Fatalf("expected native window launcher to be called")
	}
	if launchedURL != "http://127.0.0.1:43210" {
		t.Fatalf("expected launcher URL to be backend base URL, got %q", launchedURL)
	}
	if !shutdownCalled {
		t.Fatalf("expected backend shutdown to be called")
	}
}

func TestRunDesktopStartFailure(t *testing.T) {
	cfg := &config.Config{Mode: config.ModeDesktop}

	startErr := errors.New("boom")
	launched := false

	err := runDesktop(
		cfg,
		func(context.Context, *config.Config) (*desktopBackend, error) {
			return nil, startErr
		},
		func(string, func()) error {
			launched = true
			return nil
		},
	)
	if !errors.Is(err, startErr) {
		t.Fatalf("expected start error, got %v", err)
	}
	if launched {
		t.Fatalf("expected launcher not to run when backend fails")
	}
}

func TestLoadDesktopConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.env"
	if err := os.WriteFile(path, []byte("OPENVIBELY_APP_DATA_DIR=/tmp/shared-openvibely\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("OPENVIBELY_DESKTOP_CONFIG_FILE", path)
	unsetEnv(t, "OPENVIBELY_APP_DATA_DIR")

	loadDesktopConfigFile()

	if got := os.Getenv("OPENVIBELY_APP_DATA_DIR"); got != "/tmp/shared-openvibely" {
		t.Fatalf("OPENVIBELY_APP_DATA_DIR=%q", got)
	}
}

func TestSetDesktopOAuthDefaults(t *testing.T) {
	t.Run("defaults oauth redirect mode to auto when unset", func(t *testing.T) {
		unsetEnv(t, "OAUTH_REDIRECT_MODE")

		setDesktopOAuthDefaults()

		if got := os.Getenv("OAUTH_REDIRECT_MODE"); got != "auto" {
			t.Fatalf("expected OAUTH_REDIRECT_MODE=auto, got %q", got)
		}
	})

	t.Run("does not override explicitly configured oauth redirect mode", func(t *testing.T) {
		t.Setenv("OAUTH_REDIRECT_MODE", "hosted")

		setDesktopOAuthDefaults()

		if got := os.Getenv("OAUTH_REDIRECT_MODE"); got != "hosted" {
			t.Fatalf("expected OAUTH_REDIRECT_MODE to stay hosted, got %q", got)
		}
	})
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if had {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
