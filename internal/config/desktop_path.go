package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EnsureDesktopPATH augments the process PATH environment variable so that
// task shells spawned by the OpenVibely desktop app can find common developer
// tools (go, node, python, claude, codex, etc.).
//
// macOS GUI applications launched from Finder/Dock inherit only launchd's
// minimal PATH (typically /usr/bin:/bin:/usr/sbin:/sbin), which omits Go,
// Homebrew, and user-local binary directories. As a result, subprocesses
// (including the Claude/Codex CLI shelling out to `bash -c "go ..."`) report
// "command not found" even though the tool is installed.
//
// Server/VPS mode is unaffected because cmd/server never calls this function;
// it lives in package config so it can be unit-tested without depending on
// Wails or the desktop entrypoint.
//
// The function only prepends candidate directories that:
//   - actually exist on disk, and
//   - are not already present in PATH.
//
// It always returns the resulting PATH value (for logging/testing). Callers
// that want to mutate the live environment should pass setEnv=true.
func EnsureDesktopPATH() string {
	return augmentDesktopPATH(os.Getenv("PATH"), defaultDesktopPATHCandidates(), true)
}

// defaultDesktopPATHCandidates returns the ordered list of directories the
// desktop app should make available on PATH for spawned task shells.
// Earlier entries take precedence (prepended first).
func defaultDesktopPATHCandidates() []string {
	home := os.Getenv("HOME")
	if home == "" {
		// On Windows os.UserHomeDir is more reliable than HOME.
		if hd, err := os.UserHomeDir(); err == nil {
			home = hd
		}
	}

	// Common cross-platform developer-tool directories.
	common := []string{
		"/usr/local/bin",
		"/usr/local/sbin",
	}

	// Go toolchain default install locations.
	goPaths := []string{
		"/usr/local/go/bin", // official installer
	}
	if home != "" {
		goPaths = append(goPaths, filepath.Join(home, "go", "bin"))
	}

	var platform []string
	switch runtime.GOOS {
	case "darwin":
		platform = []string{
			"/opt/homebrew/bin", // Apple Silicon Homebrew
			"/opt/homebrew/sbin",
			"/usr/local/bin", // Intel Homebrew (already in common, dedup below)
		}
	case "linux":
		platform = []string{
			"/home/linuxbrew/.linuxbrew/bin",
			"/snap/bin",
		}
	case "windows":
		// Windows uses ; as separator and rarely needs PATH adjustments for
		// GUI launches, but include the typical Go install location for
		// completeness. PATH manipulation is still safe here.
		platform = []string{
			`C:\Program Files\Go\bin`,
			`C:\Go\bin`,
		}
	}

	var userLocal []string
	if home != "" {
		userLocal = []string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".nvm", "versions", "node"), // not a bin itself; harmless if missing
			filepath.Join(home, "bin"),
		}
	}

	// Order: platform package managers first (Homebrew on macOS, etc.),
	// then Go toolchain, then user-local bins, then generic /usr/local.
	candidates := make([]string, 0, len(platform)+len(goPaths)+len(userLocal)+len(common))
	candidates = append(candidates, platform...)
	candidates = append(candidates, goPaths...)
	candidates = append(candidates, userLocal...)
	candidates = append(candidates, common...)

	return candidates
}

// augmentDesktopPATH prepends candidate directories to currentPATH if they
// exist on disk and are not already present. It returns the resulting PATH
// string and, when setEnv is true, also writes it to the process environment.
func augmentDesktopPATH(currentPATH string, candidates []string, setEnv bool) string {
	sep := string(os.PathListSeparator)

	// Build a set of existing entries (after trimming) for quick lookup.
	existing := strings.Split(currentPATH, sep)
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		seen[filepath.Clean(e)] = struct{}{}
	}

	var prepend []string
	for _, dir := range candidates {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			continue
		}
		// Only add directories that exist on disk to keep PATH tidy.
		if info, err := os.Stat(clean); err != nil || !info.IsDir() {
			continue
		}
		prepend = append(prepend, clean)
		seen[clean] = struct{}{}
	}

	if len(prepend) == 0 {
		return currentPATH
	}

	var newPATH string
	if strings.TrimSpace(currentPATH) == "" {
		newPATH = strings.Join(prepend, sep)
	} else {
		newPATH = strings.Join(prepend, sep) + sep + currentPATH
	}

	if setEnv {
		_ = os.Setenv("PATH", newPATH)
	}
	return newPATH
}
