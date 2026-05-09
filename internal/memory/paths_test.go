package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRegisteredResolver(t *testing.T, projectID string) (*PathResolver, string) {
	t.Helper()
	r, err := NewPathResolver("", "")
	if err != nil {
		t.Fatalf("NewPathResolver: %v", err)
	}
	dir := filepath.Join(t.TempDir(), ".openvibely", "memory")
	if err := r.SetProjectDirOverride(projectID, dir); err != nil {
		t.Fatalf("SetProjectDirOverride: %v", err)
	}
	return r, dir
}

func TestPathResolver_ProjectDirRequiresRegisteredRepoMemoryDir(t *testing.T) {
	r, err := NewPathResolver(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewPathResolver: %v", err)
	}
	if _, err := r.ProjectDir("proj1"); err == nil {
		t.Fatal("expected error before project memory dir is registered")
	}
}

func TestPathResolver_ProjectDirUsesRegisteredRepoMemoryDir(t *testing.T) {
	r, shared := newRegisteredResolver(t, "proj1")
	if got, err := r.ProjectDir("proj1"); err != nil || got != shared {
		t.Fatalf("ProjectDir override = %q, %v; want %q", got, err, shared)
	}
	if err := r.SetProjectDirOverride("proj1", ""); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if _, err := r.ProjectDir("proj1"); err == nil {
		t.Fatal("expected error after clearing project memory dir")
	}
}

func TestSharedRepoMemoryDirUsesRepoLocalMemoryPath(t *testing.T) {
	repo := t.TempDir()
	got, err := SharedRepoMemoryDir(repo)
	if err != nil {
		t.Fatalf("SharedRepoMemoryDir: %v", err)
	}
	want := filepath.Join(repo, ".openvibely", "memory")
	if got != want {
		t.Fatalf("dir = %q want %q", got, want)
	}
	idx, err := SharedRepoMemoryIndexPath(repo)
	if err != nil {
		t.Fatalf("SharedRepoMemoryIndexPath: %v", err)
	}
	if idx != filepath.Join(want, IndexFileName) {
		t.Fatalf("index = %q want %q", idx, filepath.Join(want, IndexFileName))
	}
}

func TestEnsureProjectDir_CreatesFlatLayout(t *testing.T) {
	r, want := newRegisteredResolver(t, "proj1")
	dir, err := r.EnsureProjectDir("proj1")
	if err != nil {
		t.Fatalf("EnsureProjectDir: %v", err)
	}
	if dir != want {
		t.Fatalf("dir=%q want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("missing memory dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("not a directory: %s", dir)
	}
	for _, sub := range []string{"user", "feedback", "project", "runs"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); !os.IsNotExist(err) {
			t.Fatalf("unexpected subdir %s", sub)
		}
	}
}

func TestResolveSafe_RejectsAbsolutePath(t *testing.T) {
	r, _ := newRegisteredResolver(t, "proj")
	_, err := r.ResolveSafe("proj", "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestResolveSafe_RejectsTraversal(t *testing.T) {
	r, _ := newRegisteredResolver(t, "proj")
	for _, p := range []string{"../escape.md", "user/../../escape.md", ".."} {
		if _, err := r.ResolveSafe("proj", p); err == nil {
			t.Fatalf("expected error for %q", p)
		}
	}
}

func TestResolveSafe_AllowsValidRelative(t *testing.T) {
	r, _ := newRegisteredResolver(t, "proj")
	if _, err := r.EnsureProjectDir("proj"); err != nil {
		t.Fatalf("EnsureProjectDir: %v", err)
	}
	abs, err := r.ResolveSafe("proj", "foo.md")
	if err != nil {
		t.Fatalf("ResolveSafe: %v", err)
	}
	if !strings.HasSuffix(abs, "foo.md") {
		t.Fatalf("unexpected absolute path: %s", abs)
	}
}

func TestResolveSafe_RejectsSymlinkEscape(t *testing.T) {
	r, projDir := newRegisteredResolver(t, "proj")
	if _, err := r.EnsureProjectDir("proj"); err != nil {
		t.Fatalf("EnsureProjectDir: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	link := filepath.Join(projDir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		// Symlinks may not be available on all CI systems; skip when not.
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := r.ResolveSafe("proj", "escape/foo.md")
	if err == nil {
		t.Fatal("expected symlink-escape rejection")
	}
}

func TestProjectIDValidation(t *testing.T) {
	r, _ := NewPathResolver("", "")
	bad := []string{"", "../proj", "proj/with/slash", "proj space", "proj?bad"}
	for _, id := range bad {
		if _, err := r.ProjectDir(id); err == nil {
			t.Fatalf("expected error for project id %q", id)
		}
	}
	good := []string{"a", "abc-123", "abc_123", "1234567890abcdef1234567890abcdef"}
	for _, id := range good {
		if err := r.SetProjectDirOverride(id, filepath.Join(t.TempDir(), ".openvibely", "memory")); err != nil {
			t.Fatalf("register %q: %v", id, err)
		}
		if _, err := r.ProjectDir(id); err != nil {
			t.Fatalf("unexpected error for project id %q: %v", id, err)
		}
	}
}
