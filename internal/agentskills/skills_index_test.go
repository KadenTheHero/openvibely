package agentskills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillsIndexMeta_NoFrontmatter(t *testing.T) {
	content := "# Standalone Skills\n\n## debug_go_tests\n\n[Debug Go Tests](debug_go_tests/SKILL.md)\n"
	meta := ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 0 {
		t.Fatalf("expected empty always_use, got %v", meta.AlwaysUse)
	}
	if !meta.IsEmpty() {
		t.Fatal("expected IsEmpty() to be true with no frontmatter")
	}
}

func TestParseSkillsIndexMeta_WithAlwaysUseList(t *testing.T) {
	content := "---\nalways_use:\n  - openvibely_project_guidance\n  - debug_go_tests\n---\n\n# Standalone Skills\n\n## openvibely_project_guidance\n"
	meta := ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 2 {
		t.Fatalf("expected 2 always_use entries, got %v", meta.AlwaysUse)
	}
	if meta.AlwaysUse[0] != "openvibely_project_guidance" {
		t.Errorf("expected first entry openvibely_project_guidance, got %q", meta.AlwaysUse[0])
	}
	if meta.AlwaysUse[1] != "debug_go_tests" {
		t.Errorf("expected second entry debug_go_tests, got %q", meta.AlwaysUse[1])
	}
	if meta.IsEmpty() {
		t.Fatal("expected IsEmpty() to be false with entries")
	}
}

func TestParseSkillsIndexMeta_DeduplicatesHandles(t *testing.T) {
	content := "---\nalways_use:\n  - skill_a\n  - skill_b\n  - skill_a\n---\n"
	meta := ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 2 {
		t.Fatalf("expected 2 deduped handles, got %v", meta.AlwaysUse)
	}
}

func TestParseSkillsIndexMeta_DropsInvalidSlugs(t *testing.T) {
	content := "---\nalways_use:\n  - valid_skill\n  - ../traversal\n  - \"\"\n  - skill with spaces\n---\n"
	meta := ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 1 || meta.AlwaysUse[0] != "valid_skill" {
		t.Fatalf("expected only valid_skill, got %v", meta.AlwaysUse)
	}
}

func TestParseSkillsIndexMeta_MalformedFrontmatter(t *testing.T) {
	// Invalid YAML
	content := "---\nalways_use: [unclosed\n---\n"
	meta := ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 0 {
		t.Fatalf("expected empty always_use on malformed YAML, got %v", meta.AlwaysUse)
	}
}

func TestParseSkillsIndexMeta_EmptyFrontmatterBlock(t *testing.T) {
	content := "---\n---\n\n# Standalone Skills\n"
	meta := ParseSkillsIndexMeta(content)
	if len(meta.AlwaysUse) != 0 {
		t.Fatalf("expected empty always_use for empty frontmatter, got %v", meta.AlwaysUse)
	}
}

func TestReadSkillsIndexMeta_MissingFile(t *testing.T) {
	meta := ReadSkillsIndexMeta(filepath.Join(t.TempDir(), "MISSING.md"))
	if !meta.IsEmpty() {
		t.Fatal("expected empty meta for missing file")
	}
}

func TestReadSkillsIndexMeta_ReadsRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILLS.md")
	content := "---\nalways_use:\n  - guidance_skill\n---\n\n## guidance_skill\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	meta := ReadSkillsIndexMeta(path)
	if len(meta.AlwaysUse) != 1 || meta.AlwaysUse[0] != "guidance_skill" {
		t.Fatalf("expected [guidance_skill], got %v", meta.AlwaysUse)
	}
}

func TestAlwaysUseHandlesFromRoots_EmptyRoots(t *testing.T) {
	handles := AlwaysUseHandlesFromRoots("", "")
	if len(handles) != 0 {
		t.Fatalf("expected no handles for empty roots, got %v", handles)
	}
}

func TestAlwaysUseHandlesFromRoots_MergesAndDeduplicates(t *testing.T) {
	globalRoot := t.TempDir()
	projectRoot := t.TempDir()

	// Global: has skill_a, skill_b
	writeMeta(t, globalRoot, "---\nalways_use:\n  - skill_a\n  - skill_b\n---\n\n## skill_a\n\n## skill_b\n")
	// Project: has skill_b (duplicate), skill_c
	writeMeta(t, projectRoot, "---\nalways_use:\n  - skill_b\n  - skill_c\n---\n\n## skill_b\n\n## skill_c\n")

	handles := AlwaysUseHandlesFromRoots(globalRoot, projectRoot)
	if len(handles) != 3 {
		t.Fatalf("expected 3 handles (skill_a, skill_b, skill_c), got %v", handles)
	}
	wantOrder := []string{"skill_a", "skill_b", "skill_c"}
	for i, want := range wantOrder {
		if handles[i] != want {
			t.Errorf("position %d: expected %q, got %q", i, want, handles[i])
		}
	}
}

func TestAlwaysUseHandlesFromRoots_GlobalOnlyNoProjectIndex(t *testing.T) {
	globalRoot := t.TempDir()
	writeMeta(t, globalRoot, "---\nalways_use:\n  - skill_a\n---\n\n## skill_a\n")

	handles := AlwaysUseHandlesFromRoots(globalRoot, t.TempDir())
	if len(handles) != 1 || handles[0] != "skill_a" {
		t.Fatalf("expected [skill_a], got %v", handles)
	}
}

func TestMergeAlwaysUseIntoSelected_InjectsAlwaysUseHandles(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "always_skill", "---\n---\n\nbody")
	writeSkill(t, root, "curator_skill", "---\n---\n\nbody")
	writeMeta(t, root, "---\nalways_use:\n  - always_skill\n---\n\n## always_skill\n\n## curator_skill\n")

	catalog, err := BuildCatalog("test", root, "")
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}

	selected := []string{"curator_skill"}
	merged, prov := MergeAlwaysUseIntoSelected(catalog, root, "", selected)

	if len(merged) != 2 {
		t.Fatalf("expected 2 handles, got %v", merged)
	}
	if prov["curator_skill"] != ProvenanceSkillCurator {
		t.Errorf("curator_skill provenance: expected %q, got %q", ProvenanceSkillCurator, prov["curator_skill"])
	}
	if prov["always_skill"] != ProvenanceAlwaysUse {
		t.Errorf("always_skill provenance: expected %q, got %q", ProvenanceAlwaysUse, prov["always_skill"])
	}
}

func TestMergeAlwaysUseIntoSelected_SameHandleInBoth(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "shared_skill", "---\n---\n\nbody")
	writeMeta(t, root, "---\nalways_use:\n  - shared_skill\n---\n\n## shared_skill\n")

	catalog, _ := BuildCatalog("test", root, "")
	selected := []string{"shared_skill"}
	merged, prov := MergeAlwaysUseIntoSelected(catalog, root, "", selected)

	// De-duplicated: should appear only once
	if len(merged) != 1 {
		t.Fatalf("expected 1 handle (de-duped), got %v", merged)
	}
	if prov["shared_skill"] != ProvenanceBoth {
		t.Errorf("shared_skill provenance: expected %q, got %q", ProvenanceBoth, prov["shared_skill"])
	}
}

func TestMergeAlwaysUseIntoSelected_ExcludesDisabledAlwaysUse(t *testing.T) {
	root := t.TempDir()
	// Write a disabled skill
	writeSkill(t, root, "disabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: disabled_skill\n    enabled: false\n---\n\nbody")
	writeSkill(t, root, "enabled_skill", "---\n---\n\nbody")
	writeMeta(t, root, "---\nalways_use:\n  - disabled_skill\n  - enabled_skill\n---\n\n## disabled_skill\n\n## enabled_skill\n")

	// BuildCatalog excludes disabled skills from the runtime catalog
	catalog, _ := BuildCatalog("test", root, "")

	selected := []string{}
	merged, prov := MergeAlwaysUseIntoSelected(catalog, root, "", selected)

	// disabled_skill must NOT appear (it was excluded by BuildCatalog)
	for _, h := range merged {
		if h == "disabled_skill" {
			t.Fatalf("disabled_skill must not appear in merged handles, got %v", merged)
		}
	}
	if len(merged) != 1 || merged[0] != "enabled_skill" {
		t.Fatalf("expected [enabled_skill], got %v", merged)
	}
	if prov["enabled_skill"] != ProvenanceAlwaysUse {
		t.Errorf("expected always_use provenance for enabled_skill, got %q", prov["enabled_skill"])
	}
}

func TestMergeAlwaysUseIntoSelected_NoAlwaysUseIsNoop(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "some_skill", "---\n---\n\nbody")
	// No frontmatter in SKILLS.md (no always_use)
	catalog, _ := BuildCatalog("test", root, "")

	selected := []string{"some_skill"}
	merged, _ := MergeAlwaysUseIntoSelected(catalog, root, "", selected)

	if len(merged) != 1 || merged[0] != "some_skill" {
		t.Fatalf("expected no change without always_use, got %v", merged)
	}
}

func TestMergeAlwaysUseIntoSelected_UnindexedAlwaysUseHandleSkipped(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "real_skill", "---\n---\n\nbody")
	// phantom_skill appears in always_use but has no SKILL.md (not in catalog)
	writeMeta(t, root, "---\nalways_use:\n  - real_skill\n  - phantom_skill\n---\n\n## real_skill\n")

	catalog, _ := BuildCatalog("test", root, "")
	merged, _ := MergeAlwaysUseIntoSelected(catalog, root, "", nil)

	for _, h := range merged {
		if h == "phantom_skill" {
			t.Fatalf("phantom_skill should not appear since it is not in the catalog, got %v", merged)
		}
	}
}

// writeMeta writes SKILLS.md at the root's skills directory with the given content.
func writeMeta(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, SkillsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillsIndexFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILLS.md: %v", err)
	}
}
