package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	desktopShellPATHTimeout = 10 * time.Second
	desktopPathBeginMarker  = "__OPENVIBELY_PATH_BEGIN__"
	desktopPathEndMarker    = "__OPENVIBELY_PATH_END__"
)

// EnsureDesktopPATH updates the process PATH so task shells spawned by the
// OpenVibely desktop app inherit the same PATH the user gets from their shell.
//
// macOS GUI applications launched from Finder/Dock inherit launchd's minimal
// PATH instead of the user's login/interactive shell PATH. That makes tools
// installed through Go, Homebrew, asdf, mise, nix, cargo, etc. disappear from
// task subprocesses even though they work in Terminal.
//
// Server/VPS mode is unaffected because cmd/server never calls this function;
// it lives in package config so it can be unit-tested without depending on
// Wails or the desktop entrypoint.
func EnsureDesktopPATH() string {
	currentPATH := os.Getenv("PATH")

	shellPATH, err := readUserShellPATH()
	if err != nil || strings.TrimSpace(shellPATH) == "" {
		return currentPATH
	}

	return mergeDesktopPATH(currentPATH, shellPATH, true)
}

func readUserShellPATH() (string, error) {
	if runtime.GOOS == "windows" {
		return "", errors.New("shell PATH bootstrap is not used on windows")
	}

	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = defaultDesktopShell()
	}
	if shell == "" {
		return "", errors.New("no user shell found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), desktopShellPATHTimeout)
	defer cancel()

	return readShellPATH(ctx, shell)
}

func defaultDesktopShell() string {
	candidates := []string{"/bin/zsh", "/bin/bash", "/bin/sh"}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func readShellPATH(ctx context.Context, shell string) (string, error) {
	command := fmt.Sprintf("printf '\\n%s%%s%s\\n' \"$PATH\"", desktopPathBeginMarker, desktopPathEndMarker)
	variants := [][]string{
		{"-lic", command},
		{"-lc", command},
		{"-c", command},
	}

	var lastErr error
	for _, args := range variants {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		cmd := exec.CommandContext(ctx, shell, args...)
		out, err := cmd.CombinedOutput()
		if path, ok := extractMarkedPATH(string(out)); ok {
			return path, nil
		}
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = fmt.Errorf("shell output did not contain PATH marker")
	}
	if lastErr == nil {
		lastErr = errors.New("failed to read shell PATH")
	}
	return "", lastErr
}

func extractMarkedPATH(output string) (string, bool) {
	start := strings.LastIndex(output, desktopPathBeginMarker)
	if start < 0 {
		return "", false
	}
	start += len(desktopPathBeginMarker)

	endRel := strings.Index(output[start:], desktopPathEndMarker)
	if endRel < 0 {
		return "", false
	}

	return strings.TrimSpace(output[start : start+endRel]), true
}

// mergeDesktopPATH places shell-initialized entries before the inherited GUI
// PATH, preserving all existing entries and removing duplicates. It does not
// guess or stat tool locations; the user's shell PATH is treated as the source
// of truth.
func mergeDesktopPATH(currentPATH, shellPATH string, setEnv bool) string {
	sep := string(os.PathListSeparator)
	mergedEntries := mergePATHEntries(strings.Split(shellPATH, sep), strings.Split(currentPATH, sep))
	newPATH := strings.Join(mergedEntries, sep)

	if setEnv {
		_ = os.Setenv("PATH", newPATH)
	}
	return newPATH
}

func mergePATHEntries(entryGroups ...[]string) []string {
	var merged []string
	seen := make(map[string]struct{})
	for _, entries := range entryGroups {
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}

			key := pathEntryKey(entry)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, entry)
		}
	}
	return merged
}

func pathEntryKey(entry string) string {
	key := filepath.Clean(entry)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}
