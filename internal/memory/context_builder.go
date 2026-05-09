package memory

import (
	"fmt"
	"sort"
	"strings"
)

// ContextOptions bounds how much memory is injected into a prompt.
type ContextOptions struct {
	// MaxFiles is the maximum number of memory files to include. 0 means
	// the default (8).
	MaxFiles int
	// MaxBytes is the soft cap on the total memory block size (frontmatter
	// + body). 0 means the default (16384).
	MaxBytes int
	// Keywords is a free-form list of search hints (task title words,
	// recent prompt tokens). Files whose path/title/body match any
	// keyword score higher.
	Keywords []string
	// IncludeIndex always includes the project index when true.
	IncludeIndex bool
}

// ContextBuilder produces a bounded prompt context block using a project's
// memory directory. It is shared by chat (web/API), task execution, task
// thread follow-ups, and external-channel surfaces.
type ContextBuilder struct {
	store *FileStore
}

// NewContextBuilder returns a builder backed by the given file store.
func NewContextBuilder(store *FileStore) *ContextBuilder {
	return &ContextBuilder{store: store}
}

// Build returns a memory block to inject into a system prompt. When memory
// is disabled or no files exist, an empty string is returned.
func (b *ContextBuilder) Build(projectID string, enabled bool, opts ContextOptions) (string, error) {
	if !enabled {
		return "", nil
	}
	if opts.MaxFiles == 0 {
		opts.MaxFiles = 8
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 16384
	}

	idx, err := b.store.ReadIndex(projectID)
	if err != nil {
		return "", err
	}
	files, err := b.store.ListFiles(projectID)
	if err != nil {
		return "", err
	}
	if idx == "" && len(files) == 0 {
		return "", nil
	}

	selected := selectRelevant(files, opts)

	var sb strings.Builder
	sb.WriteString("# Project memory\n\n")
	sb.WriteString("Background context from prior tasks/chats. Memory can be stale; verify against current code when relevant.\n\n")
	if opts.IncludeIndex && strings.TrimSpace(idx) != "" {
		sb.WriteString("## Memory index\n\n")
		sb.WriteString(strings.TrimRight(idx, "\n"))
		sb.WriteString("\n\n")
	}

	used := sb.Len()
	for _, f := range selected {
		section := renderMemorySection(f)
		if used+len(section) > opts.MaxBytes {
			break
		}
		sb.WriteString(section)
		used += len(section)
	}
	return sb.String(), nil
}

func renderMemorySection(f MemoryFile) string {
	label := f.Meta.Title
	if label == "" {
		label = f.Meta.Name
	}
	if label == "" {
		label = f.RelPath
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s (%s) - %s\n\n", label, f.Meta.Type, f.RelPath)
	body := strings.TrimSpace(f.Body)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	return b.String()
}

// selectRelevant ranks files by type priority, keyword match, and recency.
// Always-include rules: feedback and user types lead, then keyword-matched
// project files, then everything else up to MaxFiles.
func selectRelevant(files []MemoryFile, opts ContextOptions) []MemoryFile {
	if len(files) == 0 {
		return nil
	}

	keywords := normalizeKeywords(opts.Keywords)

	type scored struct {
		file  MemoryFile
		score int
	}
	all := make([]scored, 0, len(files))
	for _, f := range files {
		s := 0
		switch f.Meta.Type {
		case TypeFeedback:
			s += 100
		case TypeUser:
			s += 90
		case TypeProject:
			s += 50
		}
		if matchesAnyKeyword(f, keywords) {
			s += 25
		}
		// Confidence preferences: high > medium > low.
		switch strings.ToLower(strings.TrimSpace(f.Meta.Confidence)) {
		case "high":
			s += 5
		case "low":
			s -= 5
		}
		all = append(all, scored{file: f, score: s})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].file.RelPath < all[j].file.RelPath
	})

	limit := opts.MaxFiles
	if limit <= 0 || limit > len(all) {
		limit = len(all)
	}
	out := make([]MemoryFile, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, all[i].file)
	}
	return out
}

func normalizeKeywords(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(s) < 3 {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func matchesAnyKeyword(f MemoryFile, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	hay := strings.ToLower(f.RelPath + " " + f.Meta.Title + " " + f.Meta.Name + " " + f.Body)
	for _, k := range keywords {
		if strings.Contains(hay, k) {
			return true
		}
	}
	return false
}
