package agentlibrary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/agentskills"
)

func TestSetSkillAlwaysUse_AddToEmptyFile(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	body := "\n# Standalone Skills\n\n## my_skill\n"
	if err := os.WriteFile(indexPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := SetSkillAlwaysUse(indexPath, "my_skill", true); err != nil {
		t.Fatalf("SetSkillAlwaysUse: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)
	meta := agentskills.ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 1 || meta.AlwaysUse[0] != "my_skill" {
		t.Fatalf("expected [my_skill] in always_use, got %v; content:\n%s", meta.AlwaysUse, content)
	}
	// Body content preserved
	if !strings.Contains(content, "## my_skill") {
		t.Fatalf("body entry should be preserved, got:\n%s", content)
	}
}

func TestSetSkillAlwaysUse_IdempotentAdd(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	initial := "---\nalways_use:\n  - my_skill\n---\n\n## my_skill\n"
	if err := os.WriteFile(indexPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := SetSkillAlwaysUse(indexPath, "my_skill", true); err != nil {
		t.Fatalf("SetSkillAlwaysUse: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	meta := agentskills.ParseSkillsIndexMeta(string(data))
	if len(meta.AlwaysUse) != 1 {
		t.Fatalf("expected exactly 1 entry (idempotent), got %v", meta.AlwaysUse)
	}
}

func TestSetSkillAlwaysUse_RemoveExisting(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	initial := "---\nalways_use:\n  - my_skill\n  - other_skill\n---\n\n## my_skill\n\n## other_skill\n"
	if err := os.WriteFile(indexPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := SetSkillAlwaysUse(indexPath, "my_skill", false); err != nil {
		t.Fatalf("SetSkillAlwaysUse: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)
	meta := agentskills.ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 1 || meta.AlwaysUse[0] != "other_skill" {
		t.Fatalf("expected [other_skill], got %v; content:\n%s", meta.AlwaysUse, content)
	}
	// Body preserved
	if !strings.Contains(content, "## my_skill") {
		t.Fatalf("body H2 entry should be preserved even after removing from always_use; got:\n%s", content)
	}
}

func TestSetSkillAlwaysUse_RemoveLastDropsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	initial := "---\nalways_use:\n  - only_skill\n---\n\n# Standalone Skills\n\n## only_skill\n"
	if err := os.WriteFile(indexPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := SetSkillAlwaysUse(indexPath, "only_skill", false); err != nil {
		t.Fatalf("SetSkillAlwaysUse: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)
	// Frontmatter should be gone since always_use list is now empty
	if strings.HasPrefix(content, "---") {
		t.Fatalf("expected frontmatter to be removed when always_use is empty; got:\n%s", content)
	}
	// Body content should still be present
	if !strings.Contains(content, "## only_skill") {
		t.Fatalf("body content should be preserved; got:\n%s", content)
	}
}

func TestSetSkillAlwaysUse_IdempotentRemoveNotPresent(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	initial := "---\nalways_use:\n  - other_skill\n---\n\n## other_skill\n"
	if err := os.WriteFile(indexPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := SetSkillAlwaysUse(indexPath, "nonexistent", false); err != nil {
		t.Fatalf("SetSkillAlwaysUse: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	if string(data) != initial {
		t.Fatalf("expected no change when removing handle not in list; got:\n%s", string(data))
	}
}

func TestSetSkillAlwaysUse_RemoveFromMissingFile(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "NONEXISTENT", "SKILLS.md")

	// Should be a no-op (not an error) when file doesn't exist and we're removing
	if err := SetSkillAlwaysUse(indexPath, "some_skill", false); err != nil {
		t.Fatalf("expected no error removing from missing file, got: %v", err)
	}
}

func TestSetSkillAlwaysUse_InvalidHandle(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	if err := SetSkillAlwaysUse(indexPath, "../traversal", true); err == nil {
		t.Fatal("expected error for invalid handle")
	}
}

func TestSetSkillAlwaysUse_PreservesBodyContent(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	// SKILLS.md with no frontmatter initially
	initial := "# Standalone Skills\n\n## skill_a\n\n[Skill A](skill_a/SKILL.md) — Does A.\n\n## skill_b\n\n[Skill B](skill_b/SKILL.md) — Does B.\n"
	if err := os.WriteFile(indexPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := SetSkillAlwaysUse(indexPath, "skill_a", true); err != nil {
		t.Fatalf("SetSkillAlwaysUse: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)
	// All body entries should be preserved
	for _, want := range []string{"## skill_a", "## skill_b", "Does A.", "Does B.", "Skill A", "Skill B"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected body to contain %q after adding always_use; got:\n%s", want, content)
		}
	}
	meta := agentskills.ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 1 || meta.AlwaysUse[0] != "skill_a" {
		t.Fatalf("expected [skill_a] in always_use; got %v", meta.AlwaysUse)
	}
}

func TestSetSkillAlwaysUse_MultipleAdds(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "SKILLS.md")
	body := "## skill_a\n\n## skill_b\n\n## skill_c\n"
	if err := os.WriteFile(indexPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	for _, handle := range []string{"skill_a", "skill_b", "skill_c"} {
		if err := SetSkillAlwaysUse(indexPath, handle, true); err != nil {
			t.Fatalf("add %s: %v", handle, err)
		}
	}

	data, _ := os.ReadFile(indexPath)
	meta := agentskills.ParseSkillsIndexMeta(string(data))
	if len(meta.AlwaysUse) != 3 {
		t.Fatalf("expected 3 always_use entries, got %v", meta.AlwaysUse)
	}
}

func TestStripSkillsIndexFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# Standalone Skills\n\n## skill_a\n"
	got := stripSkillsIndexFrontmatter(content)
	if got != content {
		t.Fatalf("expected unchanged content, got %q", got)
	}
}

func TestStripSkillsIndexFrontmatter_WithFrontmatter(t *testing.T) {
	content := "---\nalways_use:\n  - skill_a\n---\n\n## skill_a\n"
	got := stripSkillsIndexFrontmatter(content)
	want := "## skill_a\n"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRenderSkillsIndexContent_EmptyMeta(t *testing.T) {
	meta := agentskills.SkillsIndexMeta{}
	body := "## skill_a\n"
	got := renderSkillsIndexContent(meta, body)
	if strings.HasPrefix(got, "---") {
		t.Fatalf("expected no frontmatter for empty meta, got:\n%s", got)
	}
	if got != body {
		t.Fatalf("expected body unchanged, got %q", got)
	}
}

func TestRenderSkillsIndexContent_WithAlwaysUse(t *testing.T) {
	meta := agentskills.SkillsIndexMeta{AlwaysUse: []string{"skill_a", "skill_b"}}
	body := "## skill_a\n\n## skill_b\n"
	got := renderSkillsIndexContent(meta, body)
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("expected frontmatter, got:\n%s", got)
	}
	if !strings.Contains(got, "always_use:") {
		t.Fatalf("expected always_use in frontmatter, got:\n%s", got)
	}
	if !strings.Contains(got, "- skill_a") {
		t.Fatalf("expected skill_a in always_use, got:\n%s", got)
	}
	if !strings.Contains(got, "## skill_a") {
		t.Fatalf("expected body preserved, got:\n%s", got)
	}
}
