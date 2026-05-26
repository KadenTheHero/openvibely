package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/agentskills"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func writeServiceSkill(t *testing.T, root, skill, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", skill)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: " + skill + "\ndescription: " + skill + " desc\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	appendIndexHeader(t, agentskills.SkillsIndexPath(root), skill)
}

func writeServiceAgentSkill(t *testing.T, root, agent, skill, body string) {
	t.Helper()
	dir := filepath.Join(root, "agents", agent, "skills", skill)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: " + skill + "\ndescription: " + skill + " desc\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	appendIndexHeader(t, agentskills.AgentsIndexPath(root), agent)
	appendIndexHeader(t, agentskills.AgentSkillsIndexPath(root, agent), agent+"/"+skill)
}

func appendIndexHeader(t *testing.T, path, header string) {
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

func TestCatalogSkillResolver_UsesOwningAgentSkill(t *testing.T) {
	db := testutil.NewTestDB(t)
	agents := repository.NewAgentRepo(db)
	ctx := context.Background()
	a := &models.Agent{Name: "Router", Key: "router", Enabled: true, SelectableAsPrimary: true}
	if err := agents.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeServiceAgentSkill(t, root, "router", "route_task", "route body")
	cat, err := agentskills.BuildCatalog("turn", root, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewCatalogSkillResolver(agents, func() *agentskills.Catalog { return cat }, root, nil)
	body, err := resolver.ResolveSkill(ctx, models.AgentLifecycleHook{AgentID: a.ID, SkillKey: "route_task"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "route body") {
		t.Fatalf("expected skill body, got %q", body)
	}
}

func TestCatalogSkillResolver_DoesNotFallbackToTaskCatalogForOwnedHook(t *testing.T) {
	db := testutil.NewTestDB(t)
	agents := repository.NewAgentRepo(db)
	ctx := context.Background()
	owner := &models.Agent{Name: "Router", Key: "router", Enabled: true, SelectableAsPrimary: true}
	if err := agents.Create(ctx, owner); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeServiceAgentSkill(t, root, "task_agent", "route_task", "wrong task-agent body")
	cat, err := agentskills.BuildAgentCatalog("turn", root, "", "task_agent")
	if err != nil {
		t.Fatal(err)
	}
	ctx = withLifecycleTurnContext(ctx, lifecycleTurnContext{Catalog: cat})
	resolver := NewCatalogSkillResolver(agents, func() *agentskills.Catalog { return cat }, root, nil)
	body, err := resolver.ResolveSkill(ctx, models.AgentLifecycleHook{AgentID: owner.ID, SkillKey: "route_task"})
	if err == nil {
		t.Fatalf("expected missing owning-agent skill error, got body %q", body)
	}
	if strings.Contains(body, "wrong task-agent body") || !strings.Contains(err.Error(), "owning agent") {
		t.Fatalf("expected owning-agent-only resolution error, body=%q err=%v", body, err)
	}
}

func TestLifecycleRuntimeTools_ComposesReadAndWriteTools(t *testing.T) {
	root := t.TempDir()
	writeServiceSkill(t, root, "skill", "skill body")
	cat, err := agentskills.BuildCatalog("turn", root, "")
	if err != nil {
		t.Fatal(err)
	}
	tools := lifecycleRuntimeTools(cat, nil, nil, nil, root, "", "", "")
	if tools == nil || !tools.HasDefinition("skill_view") || !tools.HasDefinition("skills_list") {
		t.Fatalf("missing read tool definitions: %+v", tools)
	}
	out, handled, isErr, err := tools.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"skill"}`))
	if err != nil || !handled || isErr || !strings.Contains(out, "skill body") {
		t.Fatalf("skill_view failed output=%q handled=%v isErr=%v err=%v", out, handled, isErr, err)
	}
	_, handled, _, _ = tools.Executor(context.Background(), "unknown", json.RawMessage(`{}`))
	if handled {
		t.Fatal("unknown tool should not be handled")
	}
	base := &llmcontracts.RuntimeTools{Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "base"}}}
	merged := llmcontracts.CompositeRuntimeTools(base, tools)
	if !merged.HasDefinition("base") || !merged.HasDefinition("skill_view") {
		t.Fatalf("composite lost definitions: %+v", merged.Definitions)
	}
}
