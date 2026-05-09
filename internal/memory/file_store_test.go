package memory

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	r, err := NewPathResolver("", "")
	if err != nil {
		t.Fatalf("NewPathResolver: %v", err)
	}
	for _, projectID := range []string{"p1", "proj", "a", "b"} {
		dir := t.TempDir() + string(os.PathSeparator) + ".openvibely" + string(os.PathSeparator) + "memory"
		if err := r.SetProjectDirOverride(projectID, dir); err != nil {
			t.Fatalf("SetProjectDirOverride(%s): %v", projectID, err)
		}
	}
	return NewFileStore(r)
}

func TestFileStore_WriteThenRead(t *testing.T) {
	s := newStore(t)
	if _, err := s.EnsureProject("p1"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	meta := FileMeta{Name: "naming", Type: TypeUser, Title: "Naming"}
	if err := s.WriteFile("p1", "naming.md", meta, "User dislikes dream terminology."); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := s.ReadFile("p1", "naming.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got.Meta.Name != "naming" || got.Meta.Type != TypeUser {
		t.Fatalf("meta lost: %+v", got.Meta)
	}
	if !strings.Contains(got.Body, "dream terminology") {
		t.Fatalf("body missing: %q", got.Body)
	}
	if got.Meta.Updated == "" {
		t.Fatal("Updated should be auto-set")
	}
}

func TestFileStore_ListFilesReturnsTopLevelMemoryFiles(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	if err := s.WriteFile("p1", "foo.md", FileMeta{Name: "foo", Type: TypeUser}, "body"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.WriteIndex("p1", "# Memory Index\n"); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
	files, err := s.ListFiles("p1")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 1 || files[0].RelPath != "foo.md" {
		t.Fatalf("unexpected list: %+v", files)
	}
}

func TestFileStore_AtomicWrite(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	if err := s.WriteFile("p1", "foo.md", FileMeta{Name: "foo", Type: TypeUser}, "v1"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.WriteFile("p1", "foo.md", FileMeta{Name: "foo", Type: TypeUser}, "v2"); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, err := s.ReadFile("p1", "foo.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(got.Body, "v2") {
		t.Fatalf("expected v2: %q", got.Body)
	}
	// Confirm no temp leftovers in the flat project memory dir.
	dir, _ := s.Resolver().ProjectDir("p1")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".memory-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestFileStore_DeleteFileMissingIsOk(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	if err := s.DeleteFile("p1", "missing.md"); err != nil {
		t.Fatalf("delete missing should not error: %v", err)
	}
}

func TestFileStore_WriteIndexAndRead(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	idx := "# Memory Index\n\n- nothing yet\n"
	if err := s.WriteIndex("p1", idx); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
	got, err := s.ReadIndex("p1")
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if got != idx {
		t.Fatalf("index mismatch: %q", got)
	}
	size, err := s.IndexSizeBytes("p1")
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if size != int64(len(idx)) {
		t.Fatalf("size=%d want=%d", size, len(idx))
	}
}

func TestFileStore_WithProjectLockSerializes(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	var concurrent int32
	var maxConcurrent int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.WithProjectLock("p1", func() error {
				cur := atomic.AddInt32(&concurrent, 1)
				for {
					m := atomic.LoadInt32(&maxConcurrent)
					if cur <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&concurrent, -1)
				return nil
			})
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&maxConcurrent) > 1 {
		t.Fatalf("expected serialized execution, observed %d concurrent", maxConcurrent)
	}
}

func TestFileStore_WriteFileRedactsSecrets(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	body := "do not leak sk-AAAAAAAAAAAAAAAAAAAAAAAA"
	if err := s.WriteFile("p1", "leak.md", FileMeta{Name: "leak", Type: TypeProject}, body); err != nil {
		t.Fatalf("write: %v", err)
	}
	mf, err := s.ReadFile("p1", "leak.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(mf.Body, "sk-AAAA") {
		t.Fatalf("secret persisted: %q", mf.Body)
	}
	if !strings.Contains(mf.Body, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker: %q", mf.Body)
	}
}
