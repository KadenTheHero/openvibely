package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAugmentDesktopPATHPrependsExistingDirs(t *testing.T) {
	tmp := t.TempDir()
	goBin := filepath.Join(tmp, "go", "bin")
	brewBin := filepath.Join(tmp, "homebrew", "bin")
	missing := filepath.Join(tmp, "does-not-exist")

	for _, d := range []string{goBin, brewBin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	currentPATH := "/usr/bin" + string(os.PathListSeparator) + "/bin"
	got := augmentDesktopPATH(currentPATH, []string{brewBin, goBin, missing, "/usr/bin"}, false)

	parts := strings.Split(got, string(os.PathListSeparator))
	// First two entries should be the prepended existing dirs in candidate order.
	if parts[0] != filepath.Clean(brewBin) {
		t.Fatalf("expected first PATH entry to be %s, got %q (full=%q)", brewBin, parts[0], got)
	}
	if parts[1] != filepath.Clean(goBin) {
		t.Fatalf("expected second PATH entry to be %s, got %q (full=%q)", goBin, parts[1], got)
	}

	// Missing directory must not appear.
	if strings.Contains(got, missing) {
		t.Fatalf("PATH should not include missing directory %q: %s", missing, got)
	}

	// Existing /usr/bin must appear only once (dedup against currentPATH).
	count := strings.Count(got, "/usr/bin")
	if count != 1 {
		t.Fatalf("expected /usr/bin to appear exactly once, got %d in %q", count, got)
	}

	// Original PATH must be preserved at the end.
	if !strings.HasSuffix(got, currentPATH) {
		t.Fatalf("expected augmented PATH to end with original PATH; got %q", got)
	}
}

func TestAugmentDesktopPATHNoOpWhenAllPresentOrMissing(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	currentPATH := existing + string(os.PathListSeparator) + "/usr/bin"
	// Candidates: one already present, one missing — nothing to add.
	got := augmentDesktopPATH(currentPATH, []string{existing, filepath.Join(tmp, "missing")}, false)
	if got != currentPATH {
		t.Fatalf("expected PATH unchanged, got %q want %q", got, currentPATH)
	}
}

func TestAugmentDesktopPATHHandlesEmptyCurrentPATH(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got := augmentDesktopPATH("", []string{dir}, false)
	if got != filepath.Clean(dir) {
		t.Fatalf("expected PATH=%q got %q", dir, got)
	}
}

func TestEnsureDesktopPATHUpdatesProcessEnv(t *testing.T) {
	tmp := t.TempDir()
	fakeGoBin := filepath.Join(tmp, "go", "bin")
	if err := os.MkdirAll(fakeGoBin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Use a controlled PATH so the test is deterministic and doesn't depend
	// on which standard dirs happen to exist on the host.
	t.Setenv("PATH", "/nonexistent-stub-path")
	t.Setenv("HOME", tmp)

	// Inject the fake Go bin via a candidate list directly. We call the
	// unexported helper with setEnv=true to mimic EnsureDesktopPATH behavior
	// using deterministic candidates.
	newPATH := augmentDesktopPATH(os.Getenv("PATH"), []string{fakeGoBin}, true)

	if !strings.Contains(newPATH, filepath.Clean(fakeGoBin)) {
		t.Fatalf("expected new PATH to contain %s, got %q", fakeGoBin, newPATH)
	}
	if got := os.Getenv("PATH"); got != newPATH {
		t.Fatalf("expected process PATH to be updated to %q, got %q", newPATH, got)
	}
}

func TestDefaultDesktopPATHCandidatesIncludesGoLocations(t *testing.T) {
	t.Setenv("HOME", "/home/test-user")
	cands := defaultDesktopPATHCandidates()

	want := []string{"/usr/local/go/bin", "/home/test-user/go/bin"}
	for _, w := range want {
		found := false
		for _, c := range cands {
			if filepath.Clean(c) == filepath.Clean(w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected candidate list to include %q, got %v", w, cands)
		}
	}
}
