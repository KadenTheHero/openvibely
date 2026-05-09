package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileMeta captures the YAML frontmatter associated with a memory markdown file.
type FileMeta struct {
	Name       string     `yaml:"name"`
	Type       MemoryType `yaml:"type"`
	Created    string     `yaml:"created,omitempty"`
	Updated    string     `yaml:"updated,omitempty"`
	Source     string     `yaml:"source,omitempty"`
	SourceID   string     `yaml:"source_id,omitempty"`
	Confidence string     `yaml:"confidence,omitempty"`
	Title      string     `yaml:"title,omitempty"`
}

// MemoryFile is a parsed memory entry with its metadata + body.
type MemoryFile struct {
	// RelPath is the path relative to the per-project memory directory,
	// for example "naming_preferences.md".
	RelPath string
	Meta    FileMeta
	Body    string
	// SizeBytes is the on-disk size at read time.
	SizeBytes int64
}

// FileStore performs safe filesystem operations against the per-project
// memory directory. All writes go through ResolveSafe and use atomic
// temp-file + rename. Concurrent operations are serialized per project to
// avoid markdown write races.
type FileStore struct {
	resolver *PathResolver

	mu       sync.Mutex
	projLock map[string]*sync.Mutex
}

// NewFileStore returns a file store backed by the given resolver.
func NewFileStore(resolver *PathResolver) *FileStore {
	return &FileStore{resolver: resolver, projLock: map[string]*sync.Mutex{}}
}

// Resolver returns the underlying path resolver.
func (s *FileStore) Resolver() *PathResolver { return s.resolver }

// projectMutex returns the per-project serialization mutex.
func (s *FileStore) projectMutex(projectID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.projLock[projectID]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.projLock[projectID] = m
	return m
}

// WithProjectLock runs fn while holding the per-project memory mutex. Use
// this to serialize multi-step memory operations (extraction, consolidation).
func (s *FileStore) WithProjectLock(projectID string, fn func() error) error {
	m := s.projectMutex(projectID)
	m.Lock()
	defer m.Unlock()
	return fn()
}

// EnsureProject creates the per-project directory layout. Idempotent.
func (s *FileStore) EnsureProject(projectID string) (string, error) {
	return s.resolver.EnsureProjectDir(projectID)
}

// ListFiles returns top-level memory markdown files for a project, excluding
// MEMORY.md itself.
func (s *FileStore) ListFiles(projectID string) ([]MemoryFile, error) {
	dir, err := s.resolver.ProjectDir(projectID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if _, err := s.resolver.EnsureProjectDir(projectID); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := make([]MemoryFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == IndexFileName {
			continue
		}
		mf, err := s.ReadFile(projectID, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, mf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// ReadFile reads a single memory file by its project-relative path.
func (s *FileStore) ReadFile(projectID, relPath string) (MemoryFile, error) {
	abs, err := s.resolver.ResolveSafe(projectID, relPath)
	if err != nil {
		return MemoryFile{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return MemoryFile{}, fmt.Errorf("stat %s: %w", abs, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return MemoryFile{}, fmt.Errorf("read %s: %w", abs, err)
	}
	meta, body := ParseFrontmatter(string(data))
	return MemoryFile{
		RelPath:   filepath.ToSlash(relPath),
		Meta:      meta,
		Body:      body,
		SizeBytes: info.Size(),
	}, nil
}

// ReadIndex returns the contents of MEMORY.md, or "" when it does not exist.
func (s *FileStore) ReadIndex(projectID string) (string, error) {
	p, err := s.resolver.IndexPath(projectID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read index: %w", err)
	}
	return string(data), nil
}

// WriteFile creates or replaces a top-level memory file under the project
// memory directory. Path is project-relative (for example "foo.md") and is
// validated by ResolveSafe. Body is redacted before persistence.
func (s *FileStore) WriteFile(projectID, relPath string, meta FileMeta, body string) error {
	if !strings.HasSuffix(relPath, ".md") {
		return fmt.Errorf("memory: file must end in .md: %s", relPath)
	}
	if strings.Contains(filepath.ToSlash(filepath.Clean(relPath)), "/") {
		return fmt.Errorf("memory: memory files must be top-level markdown files: %s", relPath)
	}
	abs, err := s.resolver.ResolveSafe(projectID, relPath)
	if err != nil {
		return err
	}
	if _, err := s.resolver.EnsureProjectDir(projectID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("memory: mkdir parent: %w", err)
	}

	if meta.Created == "" {
		meta.Created = time.Now().UTC().Format("2006-01-02")
	}
	meta.Updated = time.Now().UTC().Format("2006-01-02")
	if meta.Type == "" {
		meta.Type = TypeProject
	}
	if meta.Name == "" {
		meta.Name = strings.TrimSuffix(filepath.Base(relPath), ".md")
	}

	body = Redact(body)
	rendered := RenderFrontmatter(meta) + body
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	return atomicWrite(abs, []byte(rendered))
}

// DeleteFile removes a memory file. No-op if the file does not exist.
func (s *FileStore) DeleteFile(projectID, relPath string) error {
	abs, err := s.resolver.ResolveSafe(projectID, relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("memory: delete %s: %w", relPath, err)
	}
	return nil
}

// WriteIndex atomically replaces MEMORY.md with the provided contents.
func (s *FileStore) WriteIndex(projectID, contents string) error {
	p, err := s.resolver.IndexPath(projectID)
	if err != nil {
		return err
	}
	if _, err := s.resolver.EnsureProjectDir(projectID); err != nil {
		return err
	}
	return atomicWrite(p, []byte(contents))
}

// IndexSizeBytes returns the size of MEMORY.md or 0 if it does not exist.
func (s *FileStore) IndexSizeBytes(projectID string) (int64, error) {
	p, err := s.resolver.IndexPath(projectID)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

// atomicWrite writes data to path via a temp file in the same directory and
// renames into place.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("memory: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := io.Copy(tmp, strings.NewReader(string(data))); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("memory: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("memory: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("memory: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("memory: rename temp -> target: %w", err)
	}
	return nil
}
