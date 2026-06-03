package agentskills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, skill, body string) string {
	t.Helper()
	dir := filepath.Join(root, SkillsDir, skill)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, SkillFile)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	appendHeader(t, SkillsIndexPath(root), skill)
	return path
}

func writeAgentSkill(t *testing.T, root, agent, skill, body string) string {
	t.Helper()
	dir := filepath.Join(root, AgentRootsDir, agent, SkillsDir, skill)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, SkillFile)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	appendHeader(t, AgentSkillsIndexPath(root, agent), agent+"/"+skill)
	return path
}

func appendHeader(t *testing.T, path, header string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing, _ := os.ReadFile(path)
	if strings.Contains(string(existing), "\n## "+header+"\n") || strings.HasPrefix(string(existing), "## "+header+"\n") {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("## " + header + "\n\n"); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCatalog_EnumeratesStandaloneHandlesFromIndexFiles(t *testing.T) {
	globalRoot := t.TempDir()
	projectRoot := t.TempDir()

	writeSkill(t, globalRoot, "implement_change", "---\n---\n\nbody")
	writeSkill(t, globalRoot, "review_auth", "---\n---\n\nbody")
	writeSkill(t, projectRoot, "debug_go_tests", "---\n---\n\nbody")

	cat, err := BuildCatalog("turn-1", globalRoot, projectRoot)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if cat.TurnID() != "turn-1" {
		t.Fatalf("want turn-1, got %q", cat.TurnID())
	}
	got := cat.Entries()
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d (%+v)", len(got), got)
	}
	dbg, ok := cat.Lookup("debug_go_tests")
	if !ok {
		t.Fatalf("expected debug_go_tests entry")
	}
	if dbg.Source != SourceProject {
		t.Fatalf("expected project source, got %s", dbg.Source)
	}
	if _, err := os.Stat(dbg.AbsolutePath); err != nil {
		t.Fatalf("AbsolutePath should exist on disk: %v", err)
	}
	if !strings.HasPrefix(dbg.AbsolutePath, projectRoot) {
		t.Fatalf("project entry should resolve under projectRoot, got %s", dbg.AbsolutePath)
	}
	raw, _ := json.Marshal(got)
	_ = raw
}

func TestBuildAgentCatalog_EnumeratesAssignedAgentSkills(t *testing.T) {
	globalRoot := t.TempDir()
	projectRoot := t.TempDir()

	globalPath := writeAgentSkill(t, globalRoot, "task_agent", "debug", "global")
	projectPath := writeAgentSkill(t, projectRoot, "task_agent", "debug", "project")
	writeAgentSkill(t, projectRoot, "other_agent", "other", "other")
	writeSkill(t, projectRoot, "standalone", "standalone")

	cat, err := BuildAgentCatalog("turn-agent", globalRoot, projectRoot, "task_agent")
	if err != nil {
		t.Fatalf("build agent catalog: %v", err)
	}
	if !cat.IsAgentOwned() {
		t.Fatal("expected agent-owned catalog")
	}
	entry, ok := cat.Lookup("debug")
	if !ok {
		t.Fatal("expected debug agent skill")
	}
	if entry.Source != SourceAgent || entry.AgentKey != "task_agent" || entry.AbsolutePath != projectPath {
		t.Fatalf("bad agent entry: %+v projectPath=%s", entry, projectPath)
	}
	if entry.AbsolutePath == globalPath {
		t.Fatal("project agent skill should override global agent skill")
	}
	if _, ok := cat.Lookup("standalone"); ok {
		t.Fatal("standalone skill must not be in assigned-agent catalog")
	}
	if _, ok := cat.Lookup("other"); ok {
		t.Fatal("other agent skill must not be in assigned-agent catalog")
	}
}

func TestBuildAgentCatalog_EmptyCatalogStillReportsAgentOwned(t *testing.T) {
	cat, err := BuildAgentCatalog("turn-agent-empty", "", t.TempDir(), "task_agent")
	if err != nil {
		t.Fatalf("build empty agent catalog: %v", err)
	}
	if !cat.IsAgentOwned() {
		t.Fatal("empty assigned-agent catalog should still be scoped as agent-owned")
	}
	filtered := cat.Filter("turn-agent-empty:selected", []string{"missing"})
	if !filtered.IsAgentOwned() {
		t.Fatal("filtered empty assigned-agent catalog should preserve agent-owned scope")
	}
}

func TestBuildCatalog_ProjectOverridesGlobalForSameHandle(t *testing.T) {
	globalRoot := t.TempDir()
	projectRoot := t.TempDir()

	globalPath := writeSkill(t, globalRoot, "debug_go_tests", "---\n---\n\nglobal")
	projectPath := writeSkill(t, projectRoot, "debug_go_tests", "---\n---\n\nproject")

	cat, err := BuildCatalog("t", globalRoot, projectRoot)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	entry, ok := cat.Lookup("debug_go_tests")
	if !ok {
		t.Fatalf("expected entry")
	}
	if entry.Source != SourceProject {
		t.Fatalf("project should win, got %s", entry.Source)
	}
	if entry.AbsolutePath != projectPath {
		t.Fatalf("expected project path %s, got %s", projectPath, entry.AbsolutePath)
	}
	if entry.AbsolutePath == globalPath {
		t.Fatalf("should not have resolved to global path")
	}
}

func TestBuildCatalog_IgnoresMissingRoots(t *testing.T) {
	cat, err := BuildCatalog("t", "/nonexistent/global", "/nonexistent/project")
	if err != nil {
		t.Fatalf("missing roots should be tolerated, got %v", err)
	}
	if len(cat.Entries()) != 0 {
		t.Fatalf("expected empty catalog, got %d entries", len(cat.Entries()))
	}
}

func TestBuildCatalog_ErrorsOnUnreadableSkillsIndex(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "ok", "---\n---\n\nbody")

	skillsPath := SkillsIndexPath(root)
	if err := os.Remove(skillsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillsPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildCatalog("t", root, ""); err == nil {
		t.Fatalf("expected read error for invalid SKILLS.md path")
	}
}

func TestBuildCatalog_RejectsHiddenTraversalAndAgentOwnedNames(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "ok", "---\n---\n\nbody")

	hidden := filepath.Join(root, SkillsDir, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, SkillFile), []byte("---\n---\n\nx"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendHeader(t, SkillsIndexPath(root), ".hidden")
	appendHeader(t, SkillsIndexPath(root), "agent/owned")

	cat, err := BuildCatalog("t", root, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := cat.Lookup(".hidden"); ok {
		t.Fatalf("hidden skill should be rejected")
	}
	if _, ok := cat.Lookup("agent/owned"); ok {
		t.Fatalf("agent-owned handle should not be routed as standalone")
	}
	if _, ok := cat.Lookup("ok"); !ok {
		t.Fatalf("valid handle should be present")
	}
}

func TestBuildCatalog_RequiresSkillBody(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "real", "---\n---\n\nbody")
	data, _ := os.ReadFile(SkillsIndexPath(root))
	tampered := string(data) + "\n## forged\n\n"
	if err := os.WriteFile(SkillsIndexPath(root), []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := BuildCatalog("t", root, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := cat.Lookup("forged"); ok {
		t.Fatalf("forged handle without on-disk SKILL.md must not be authorized")
	}
	if _, ok := cat.Lookup("real"); !ok {
		t.Fatalf("real handle should still be authorized")
	}
}

func TestBuildCatalog_ExcludesDisabledSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "enabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: enabled_skill\n    enabled: true\n---\nbody")
	writeSkill(t, root, "disabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: disabled_skill\n    enabled: false\n---\nbody")
	writeSkill(t, root, "nil_enabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: nil_enabled_skill\n---\nbody")

	cat, err := BuildCatalog("t", root, "")
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if _, ok := cat.Lookup("enabled_skill"); !ok {
		t.Fatalf("enabled skill must be in catalog")
	}
	if _, ok := cat.Lookup("nil_enabled_skill"); !ok {
		t.Fatalf("skill without enabled field must be in catalog")
	}
	if _, ok := cat.Lookup("disabled_skill"); ok {
		t.Fatalf("disabled skill must NOT be in runtime catalog")
	}
}

func TestBuildCatalogAll_IncludesDisabledSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "enabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: enabled_skill\n    enabled: true\n---\nbody")
	writeSkill(t, root, "disabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: disabled_skill\n    enabled: false\n---\nbody")

	cat, err := BuildCatalogAll("t", root, "")
	if err != nil {
		t.Fatalf("build catalog all: %v", err)
	}
	if _, ok := cat.Lookup("enabled_skill"); !ok {
		t.Fatalf("enabled skill must be in catalog")
	}
	if _, ok := cat.Lookup("disabled_skill"); !ok {
		t.Fatalf("disabled skill must appear in BuildCatalogAll for management UI")
	}
	if len(cat.Entries()) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(cat.Entries()))
	}
}

func TestBuildAgentCatalog_ExcludesDisabledAgentSkills(t *testing.T) {
	root := t.TempDir()
	writeAgentSkill(t, root, "task_agent", "enabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: enabled_skill\n    enabled: true\n---\nbody")
	writeAgentSkill(t, root, "task_agent", "disabled_skill", "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n    key: disabled_skill\n    enabled: false\n---\nbody")

	cat, err := BuildAgentCatalog("t", root, "", "task_agent")
	if err != nil {
		t.Fatalf("build agent catalog: %v", err)
	}
	if _, ok := cat.Lookup("enabled_skill"); !ok {
		t.Fatalf("enabled agent skill must be in catalog")
	}
	if _, ok := cat.Lookup("disabled_skill"); ok {
		t.Fatalf("disabled agent skill must NOT be in runtime catalog")
	}
}

func TestRenderAvailableSkillsMarkdown_ReturnsRawIndexContent(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()
	writeSkill(t, global, "implement_change", "body")
	writeSkill(t, project, "debug_go_tests", "body")

	out := RenderAvailableSkillsMarkdown(global, project)
	if !strings.Contains(out, "<available_skills>") || !strings.Contains(out, "## implement_change") || !strings.Contains(out, "## debug_go_tests") {
		t.Fatalf("rendered block missing index content:\n%s", out)
	}
	if strings.Contains(out, filepath.Join(global, SkillsDir)) || strings.Contains(out, filepath.Join(project, SkillsDir)) {
		t.Fatalf("rendered block leaked filesystem path:\n%s", out)
	}
}
