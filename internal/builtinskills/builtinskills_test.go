package builtinskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/agentskills"
)

func TestSyncTo_SeedsDefaultIndexesAndSkillBodies(t *testing.T) {
	root := t.TempDir()
	if err := SyncTo(root); err != nil {
		t.Fatalf("SyncTo: %v", err)
	}

	mustExist := []string{
		filepath.Join(root, "agents", "AGENTS.md"),
		filepath.Join(root, "agents", "skill_curator", "SKILLS.md"),
		filepath.Join(root, "agents", "skill_curator", "skills", "route_task", "SKILL.md"),
		filepath.Join(root, "agents", "skill_curator", "skills", "observe_task_for_learning", "SKILL.md"),
		filepath.Join(root, "agents", "skill_curator", "skills", "maintain_skill_library", "SKILL.md"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s after SyncTo: %v", p, err)
		}
	}

	agents, err := os.ReadFile(filepath.Join(root, "agents", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agents), "## skill_curator") {
		t.Fatalf("AGENTS.md missing ## skill_curator header:\n%s", agents)
	}

	skills, err := os.ReadFile(filepath.Join(root, "agents", "skill_curator", "SKILLS.md"))
	if err != nil {
		t.Fatalf("read SKILLS.md: %v", err)
	}
	for _, want := range []string{
		"## skill_curator/route_task",
		"skills/route_task/SKILL.md",
		"## skill_curator/observe_task_for_learning",
		"skills/observe_task_for_learning/SKILL.md",
		"## skill_curator/maintain_skill_library",
		"skills/maintain_skill_library/SKILL.md",
	} {
		if !strings.Contains(string(skills), want) {
			t.Fatalf("SKILLS.md missing %q:\n%s", want, skills)
		}
	}

	observe, err := os.ReadFile(filepath.Join(root, "agents", "skill_curator", "skills", "observe_task_for_learning", "SKILL.md"))
	if err != nil {
		t.Fatalf("read observe_task_for_learning SKILL.md: %v", err)
	}
	for _, want := range []string{"Missing skill coverage is not a no-op reason", "What Belongs In A Skill", "consolidate immediately", "skill_manage(action=archive"} {
		if !strings.Contains(string(observe), want) {
			t.Fatalf("observe_task_for_learning missing %q:\n%s", want, observe)
		}
	}
}

func TestSyncTo_PreservesUserManagedIndexesButRefreshesSystemDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := SyncTo(root); err != nil {
		t.Fatalf("initial SyncTo: %v", err)
	}

	agentsPath := filepath.Join(root, "agents", "AGENTS.md")
	skillsPath := filepath.Join(root, "agents", "skill_curator", "SKILLS.md")
	custom := "# Custom Index\n\n## skill_curator\n\nEdited by the LLM.\n"
	if err := os.WriteFile(agentsPath, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom AGENTS.md: %v", err)
	}
	customSkills := "# Custom Skills Index\n\n## skill_curator/route_task\n\nEdited.\n"
	if err := os.WriteFile(skillsPath, []byte(customSkills), 0o644); err != nil {
		t.Fatalf("write custom SKILLS.md: %v", err)
	}

	if err := SyncTo(root); err != nil {
		t.Fatalf("second SyncTo: %v", err)
	}

	got, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("re-read AGENTS.md: %v", err)
	}
	if string(got) != custom {
		t.Fatalf("AGENTS.md clobbered by SyncTo.\nwant:\n%s\ngot:\n%s", custom, got)
	}
	gotSkills, err := os.ReadFile(skillsPath)
	if err != nil {
		t.Fatalf("re-read SKILLS.md: %v", err)
	}
	if string(gotSkills) == customSkills {
		t.Fatalf("protected system skill_curator/SKILLS.md was not refreshed by SyncTo")
	}
	if !strings.Contains(string(gotSkills), "System: Skill Curator") || !strings.Contains(string(gotSkills), "skill_manage") {
		t.Fatalf("refreshed system SKILLS.md missing expected policy/tools:\n%s", gotSkills)
	}

	userAgentDir := filepath.Join(root, "agents", "backend")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	userSkillsPath := filepath.Join(userAgentDir, "SKILLS.md")
	userSkills := "# Backend Skills\n\n## backend/build\n\nEdited by user.\n"
	if err := os.WriteFile(userSkillsPath, []byte(userSkills), 0o644); err != nil {
		t.Fatalf("write backend SKILLS.md: %v", err)
	}
	if err := SyncTo(root); err != nil {
		t.Fatalf("third SyncTo: %v", err)
	}
	gotUserSkills, err := os.ReadFile(userSkillsPath)
	if err != nil {
		t.Fatalf("re-read backend SKILLS.md: %v", err)
	}
	if string(gotUserSkills) != userSkills {
		t.Fatalf("user-managed SKILLS.md clobbered by SyncTo.\nwant:\n%s\ngot:\n%s", userSkills, gotUserSkills)
	}
}

func TestSyncTo_OverwritesSkillBodies(t *testing.T) {
	root := t.TempDir()
	if err := SyncTo(root); err != nil {
		t.Fatalf("initial SyncTo: %v", err)
	}

	skillPath := filepath.Join(root, "agents", "skill_curator", "skills", "route_task", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("stale content"), 0o644); err != nil {
		t.Fatalf("write stale SKILL.md: %v", err)
	}

	if err := SyncTo(root); err != nil {
		t.Fatalf("second SyncTo: %v", err)
	}

	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("re-read SKILL.md: %v", err)
	}
	if string(got) == "stale content" {
		t.Fatalf("SKILL.md not refreshed by SyncTo; still contains stale content")
	}
}

func TestSyncTo_EmptyRootIsNoop(t *testing.T) {
	if err := SyncTo(""); err != nil {
		t.Fatalf("SyncTo(\"\"): %v", err)
	}
}

// TestSyncTo_InstallsSystemHookSkillsButKeepsRoutableCatalogEmpty verifies that
// built-in system hook skills are agent-owned implementation details, not
// standalone generated skills selected by the router.
func TestSyncTo_InstallsSystemHookSkillsButKeepsRoutableCatalogEmpty(t *testing.T) {
	root := t.TempDir()
	if err := agentskills.EnsureAgentsRoot(root); err != nil {
		t.Fatalf("EnsureAgentsRoot: %v", err)
	}
	if err := SyncTo(root); err != nil {
		t.Fatalf("SyncTo: %v", err)
	}

	cat, err := agentskills.BuildCatalog("test-turn", root, "")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if len(cat.Entries()) != 0 {
		t.Fatalf("built-in system hook skills must not appear in standalone catalog: %+v", cat.Entries())
	}

	block := agentskills.RenderAvailableSkillsMarkdown(root, "")
	if !strings.Contains(block, "_No standalone skills indexed in this turn._") {
		t.Fatalf("RenderAvailableSkillsMarkdown should show empty standalone catalog:\n%s", block)
	}

	// Sanity: agent-owned SKILL.md bodies are present so lifecycle hooks can read them.
	for _, skill := range []string{"route_task", "observe_task_for_learning", "maintain_skill_library"} {
		p := filepath.Join(root, "agents", "skill_curator", "skills", skill, "SKILL.md")
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("missing SKILL.md %s: %v", p, err)
		}
		if info.Size() == 0 {
			t.Fatalf("SKILL.md %s is empty", p)
		}
	}
}
