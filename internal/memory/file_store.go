package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileStore performs minimal filesystem setup for the per-project managed
// memory directory. The Memory Curator agent owns all subsequent reads and
// writes via its scoped-file tools, so this store only owns directory
// creation and the legacy MEMORY.md -> MEMORIES.md migration.
type FileStore struct {
	resolver *PathResolver
}

// NewFileStore returns a file store backed by the given resolver.
func NewFileStore(resolver *PathResolver) *FileStore {
	return &FileStore{resolver: resolver}
}

// Resolver returns the underlying path resolver.
func (s *FileStore) Resolver() *PathResolver { return s.resolver }

const defaultIndexContents = `# Project Memory

This index tracks durable top-level memory topic files in this directory.

- No topic files have been created yet.
`

// EnsureProject creates the per-project directory layout and initializes the
// MEMORIES.md index when it is missing. If the previous MEMORY.md index exists
// and MEMORIES.md does not, the legacy file is renamed to the canonical name.
// Idempotent.
func (s *FileStore) EnsureProject(projectID string) (string, error) {
	dir, err := s.resolver.EnsureProjectDir(projectID)
	if err != nil {
		return "", err
	}
	indexPath := filepath.Join(dir, IndexFileName)
	if _, err := os.Stat(indexPath); err == nil {
		return dir, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("memory: stat index: %w", err)
	}
	legacyIndexPath := filepath.Join(dir, LegacyIndexFileName)
	if _, err := os.Stat(legacyIndexPath); err == nil {
		if err := os.Rename(legacyIndexPath, indexPath); err != nil {
			return "", fmt.Errorf("memory: migrate legacy index: %w", err)
		}
		return dir, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("memory: stat legacy index: %w", err)
	}
	if err := atomicWrite(indexPath, []byte(defaultIndexContents)); err != nil {
		return "", err
	}
	return dir, nil
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
