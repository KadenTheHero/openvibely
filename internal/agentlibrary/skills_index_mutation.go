package agentlibrary

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openvibely/openvibely/internal/agentskills"
	"gopkg.in/yaml.v3"
)

// SetSkillAlwaysUse idempotently adds (alwaysUse=true) or removes (alwaysUse=false)
// a standalone skill handle from the always_use list in the SKILLS.md frontmatter
// at indexPath.
//
// Behavior:
//   - Reads the current SKILLS.md content (file need not pre-exist when adding).
//   - Parses and updates the frontmatter always_use list without disturbing the
//     body (existing H2 entries, narrative text).
//   - When the resulting always_use list is empty, removes the frontmatter block
//     entirely so files stay clean.
//   - Writes back only when the content actually changes (idempotent).
//   - If the file does not exist and alwaysUse is false, returns nil (no-op).
func SetSkillAlwaysUse(indexPath, handle string, alwaysUse bool) error {
	handle = strings.TrimSpace(handle)
	if !isSlug(handle) {
		return fmt.Errorf("skills_index: invalid handle %q", handle)
	}

	// Read current content. Missing file is treated as empty.
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			if !alwaysUse {
				return nil // no-op: nothing to remove
			}
			data = []byte{}
		} else {
			return fmt.Errorf("skills_index: read %s: %w", indexPath, err)
		}
	}
	content := string(data)

	// Parse existing meta and separate the body text.
	meta := agentskills.ParseSkillsIndexMeta(content)
	body := stripSkillsIndexFrontmatter(content)

	// Update the always_use list.
	newList := updateAlwaysUseList(meta.AlwaysUse, handle, alwaysUse)

	// Idempotency: if the always_use list hasn't changed semantically, don't rewrite.
	if sameStringSlice(meta.AlwaysUse, newList) {
		return nil
	}
	meta.AlwaysUse = newList

	// Render updated content.
	newContent := renderSkillsIndexContent(meta, body)

	// Ensure parent directory exists (e.g., when the file is new).
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("skills_index: mkdir: %w", err)
	}
	return os.WriteFile(indexPath, []byte(newContent), 0o644)
}

// sameStringSlice reports whether a and b contain the same elements in the same order.
func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// updateAlwaysUseList adds or removes handle from the list, de-duplicating.
func updateAlwaysUseList(current []string, handle string, add bool) []string {
	seen := make(map[string]struct{}, len(current)+1)
	out := make([]string, 0, len(current)+1)
	for _, h := range current {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if h == handle && !add {
			continue // remove this handle
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if add {
		if _, ok := seen[handle]; !ok {
			out = append(out, handle)
		}
	}
	return out
}

// stripSkillsIndexFrontmatter returns the SKILLS.md body without the leading
// frontmatter block. If no frontmatter is present, returns content unchanged.
func stripSkillsIndexFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return content
	}
	body := rest[end+len("\n---"):]
	// Strip the CRLF/LF separator that follows the closing ---; any number of
	// blank lines between the frontmatter and body are normalized away so that
	// renderSkillsIndexContent can reliably add back exactly one blank line.
	body = strings.TrimLeft(body, "\r\n")
	return body
}

// renderSkillsIndexContent assembles the final SKILLS.md file from meta + body.
// When meta is empty the frontmatter block is omitted entirely.
func renderSkillsIndexContent(meta agentskills.SkillsIndexMeta, body string) string {
	if meta.IsEmpty() {
		// No frontmatter: return body as-is (stripped of any old frontmatter above).
		return body
	}

	// Marshal only the fields we own. Use a local struct so yaml.Marshal
	// produces a predictable key order (always_use first).
	type indexMetaYAML struct {
		AlwaysUse []string `yaml:"always_use,omitempty"`
	}
	raw, err := yaml.Marshal(indexMetaYAML{AlwaysUse: meta.AlwaysUse})
	if err != nil {
		return body // defensive fallback
	}
	frontmatter := strings.TrimRight(string(raw), "\n")
	result := "---\n" + frontmatter + "\n---\n"
	if body != "" {
		result += "\n" + body
	}
	return result
}
