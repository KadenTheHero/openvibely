package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/builtinskills"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func TestAgentLibraryMaintenanceService_SyncRootDeclarationsAppliesAgentMetadata(t *testing.T) {
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	root := t.TempDir()
	agentDir := filepath.Join(root, "agents", "backend")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
kind: openvibely.agent_skill
version: 1
agent:
  key: backend
  name: Backend
  description: Backend work
  scope: project
  selectable_as_primary: true
tools:
  - skill_view
permissions:
  read_task_prompt: true
lifecycle_hooks:
  after_complete:
    skill: learn_backend
    output_contract: learning_summary
---
# Backend Skills

## backend/learn_backend

[Learn Backend](skills/learn_backend/SKILL.md) — learns from backend work.
`
	if err := os.WriteFile(filepath.Join(agentDir, "SKILLS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILLS.md: %v", err)
	}
	svc := &AgentLibraryMaintenanceService{agentRepo: agentRepo, lifecycleRepo: lifecycleRepo, agentsRootPath: root}
	if err := svc.SyncRootDeclarations(context.Background(), ""); err != nil {
		t.Fatalf("SyncRootDeclarations: %v", err)
	}
	agent, err := agentRepo.GetByKey(context.Background(), "backend")
	if err != nil || agent == nil {
		t.Fatalf("GetByKey: %v agent=%#v", err, agent)
	}
	if agent.Name != "Backend" || !agent.SelectableAsPrimary || len(agent.Tools) != 1 || agent.Tools[0] != "skill_view" || !agent.PermissionDefaults.ReadTaskPrompt {
		t.Fatalf("metadata not applied from root SKILLS.md: %#v", agent)
	}
	if !strings.Contains(agent.SystemPrompt, "# Backend Skills") {
		t.Fatalf("expected markdown body used as prompt/overview, got %q", agent.SystemPrompt)
	}
	hooks, err := lifecycleRepo.HooksByAgent(context.Background(), agent.ID)
	if err != nil {
		t.Fatalf("HooksByAgent: %v", err)
	}
	if len(hooks) != 1 || hooks[0].SkillKey != "learn_backend" || hooks[0].OutputContract != models.OutputContractLearningSummary {
		t.Fatalf("hook not applied from root SKILLS.md: %#v", hooks)
	}
}

func TestAgentLibraryMaintenanceService_SyncRootDeclarationsSanitizesSystemSkillCurator(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	root := t.TempDir()
	if err := builtinskills.SyncTo(root); err != nil {
		t.Fatalf("SyncTo: %v", err)
	}
	declPath := filepath.Join(root, bundledSkillCuratorDeclarationPath)
	legacy, err := os.ReadFile(declPath)
	if err != nil {
		t.Fatalf("read declaration: %v", err)
	}
	legacyText := strings.Replace(string(legacy), "  - skill_view", "  - ScopedFiles\n  - agent_manage\n  - skill_view", 1)
	legacyText = strings.Replace(legacyText, "permissions:\n  read_task_prompt: true", "tool_config:\n  scoped_files:\n    - directory: .openvibely/agents\n      permissions:\n        - read\n        - write\n        - delete\n  skip_default_tools: true\n  disable_runtime_worktree: true\npermissions:\n  read_task_prompt: true", 1)
	legacyText = strings.Replace(legacyText, "  write_skills: true", "  write_skills: true\n  write_agents: true\n  read_repository_files: true\n  write_repository_files: true", 1)
	if err := os.WriteFile(declPath, []byte(legacyText), 0o644); err != nil {
		t.Fatalf("write legacy declaration: %v", err)
	}

	svc := &AgentLibraryMaintenanceService{agentRepo: agentRepo, lifecycleRepo: lifecycleRepo, agentsRootPath: root}
	if err := svc.SyncRootDeclarations(ctx, ""); err != nil {
		t.Fatalf("SyncRootDeclarations: %v", err)
	}
	agent, err := agentRepo.GetBySystemKind(ctx, models.AgentSystemKindSkillCurator)
	if err != nil || agent == nil {
		t.Fatalf("load system agent: %v %#v", err, agent)
	}
	for _, denied := range []string{models.AgentToolScopedFiles, "agent_manage"} {
		if AgentAllowsTool(agent, denied) {
			t.Fatalf("synced system agent must not grant %s, tools=%v", denied, agent.Tools)
		}
	}
	if len(agent.ToolConfig.ScopedFiles) != 0 || agent.ToolConfig.SkipDefaultTools || agent.ToolConfig.DisableRuntimeWorktree {
		t.Fatalf("synced system agent must not keep scoped files config: %#v", agent.ToolConfig)
	}
	if agent.PermissionDefaults.WriteAgents || agent.PermissionDefaults.ReadRepositoryFiles || agent.PermissionDefaults.WriteRepositoryFiles {
		t.Fatalf("synced system agent must not keep agent/repo write permissions: %#v", agent.PermissionDefaults)
	}
}

func TestAgentLibraryMaintenanceService_EnsureProjectCreatesVisibleScheduledTask(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	agentsRoot := t.TempDir()
	if err := builtinskills.SyncTo(agentsRoot); err != nil {
		t.Fatalf("SyncTo: %v", err)
	}
	svc := NewAgentLibraryMaintenanceService(taskRepo, scheduleRepo, agentRepo)
	svc.SetLifecycleRepo(lifecycleRepo)
	svc.SetAgentsRootPath(agentsRoot)

	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "agent-library-maintenance", Description: "test"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectID := project.ID
	if err := svc.EnsureProject(ctx, projectID); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	agent, err := agentRepo.GetBySystemKind(ctx, models.AgentSystemKindSkillCurator)
	if err != nil {
		t.Fatalf("GetBySystemKind: %v", err)
	}
	if agent == nil {
		t.Fatal("expected skill curator system agent")
	}
	if agent.GeneratedStatus != models.AgentStatusProtected || agent.SelectableAsPrimary {
		t.Fatalf("expected protected non-selectable agent, got status=%q selectable=%v", agent.GeneratedStatus, agent.SelectableAsPrimary)
	}
	for _, want := range []string{"skill_view", "skills_list", "agent_view", "skill_manage"} {
		if !AgentAllowsTool(agent, want) {
			t.Fatalf("expected Skill Curator to grant %s, tools=%v", want, agent.Tools)
		}
	}
	if AgentAllowsTool(agent, models.AgentToolScopedFiles) || len(agent.ToolConfig.ScopedFiles) != 0 || agent.ToolConfig.SkipDefaultTools || agent.ToolConfig.DisableRuntimeWorktree {
		t.Fatalf("Skill Curator must not get scoped-file bypasses for agent roots, tools=%v config=%#v", agent.Tools, agent.ToolConfig)
	}
	if !agent.PermissionDefaults.ReadAgents || agent.PermissionDefaults.WriteAgents || !agent.PermissionDefaults.ReadSkills || !agent.PermissionDefaults.WriteSkills || agent.PermissionDefaults.ReadRepositoryFiles || agent.PermissionDefaults.WriteRepositoryFiles {
		t.Fatalf("expected skill-only declaration-backed permission defaults, got %#v", agent.PermissionDefaults)
	}
	if strings.Contains(agent.SystemPrompt, "system_prompt:") {
		t.Fatalf("agent prompt should come from SKILLS.md markdown body, not YAML system_prompt: %q", agent.SystemPrompt)
	}
	for _, want := range []string{"route_task", "observe_task_for_learning", "maintain_skill_library"} {
		found := false
		for _, skill := range agent.Skills {
			if skill.Name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected Skill Curator skill %q loaded from root SKILLS.md index, got %#v", want, agent.Skills)
		}
	}
	if len(agent.SourceRefs) != 1 || agent.SourceRefs[0] != bundledSkillCuratorDeclarationPath {
		t.Fatalf("expected declaration source ref, got %#v", agent.SourceRefs)
	}
	hooks, err := lifecycleRepo.HooksByAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("HooksByAgent: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("expected two declaration-backed lifecycle hooks, got %d: %#v", len(hooks), hooks)
	}
	have := map[string]models.AgentLifecycleHook{}
	for _, hook := range hooks {
		have[string(hook.When)+"/"+hook.SkillKey] = hook
	}
	if hook, ok := have["route_task/route_task"]; !ok || hook.OutputContract != models.OutputContractSelectedSkills || !hook.Blocking || !hook.Enabled {
		t.Fatalf("bad route_task hook: %#v", hook)
	}
	if hook, ok := have["after_complete/observe_task_for_learning"]; !ok || hook.OutputContract != models.OutputContractLearningSummary || hook.Blocking || !hook.Enabled {
		t.Fatalf("bad observe hook: %#v", hook)
	}
	task, err := taskRepo.GetByProjectAndTitle(ctx, projectID, agentLibraryMaintenanceTaskTitle)
	if err != nil {
		t.Fatalf("GetByProjectAndTitle: %v", err)
	}
	if task == nil {
		t.Fatal("expected scheduled maintenance task")
	}
	if task.Category != models.CategoryScheduled || task.AgentDefinitionID == nil || *task.AgentDefinitionID != agent.ID {
		t.Fatalf("unexpected task category/agent: category=%q agent=%v want=%s", task.Category, task.AgentDefinitionID, agent.ID)
	}
	schedules, err := scheduleRepo.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected one normal schedule row, got %d", len(schedules))
	}
	if schedules[0].RepeatType != models.RepeatDaily || !schedules[0].Enabled {
		t.Fatalf("unexpected schedule: repeat=%q enabled=%v", schedules[0].RepeatType, schedules[0].Enabled)
	}

	if err := svc.EnsureProject(ctx, projectID); err != nil {
		t.Fatalf("EnsureProject second: %v", err)
	}
	schedules, err = scheduleRepo.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListByTask second: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected idempotent schedule creation, got %d schedules", len(schedules))
	}
	hooks, err = lifecycleRepo.HooksByAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("HooksByAgent second: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("expected idempotent hook repair, got %d hooks", len(hooks))
	}
}
func TestAgentLibraryMaintenanceService_EnsureProjectSanitizesLegacySystemDeclarationGrants(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	agentsRoot := t.TempDir()
	if err := builtinskills.SyncTo(agentsRoot); err != nil {
		t.Fatalf("SyncTo: %v", err)
	}
	declPath := filepath.Join(agentsRoot, bundledSkillCuratorDeclarationPath)
	legacy, err := os.ReadFile(declPath)
	if err != nil {
		t.Fatalf("read declaration: %v", err)
	}
	legacyText := strings.Replace(string(legacy), "  - skill_view", "  - ScopedFiles\n  - agent_manage\n  - skill_view", 1)
	legacyText = strings.Replace(legacyText, "permissions:\n  read_task_prompt: true", "tool_config:\n  scoped_files:\n    - directory: .openvibely/agents\n      permissions:\n        - read\n        - write\n        - delete\n  skip_default_tools: true\n  disable_runtime_worktree: true\npermissions:\n  read_task_prompt: true", 1)
	legacyText = strings.Replace(legacyText, "  write_skills: true", "  write_skills: true\n  write_agents: true\n  read_repository_files: true\n  write_repository_files: true", 1)
	if err := os.WriteFile(declPath, []byte(legacyText), 0o644); err != nil {
		t.Fatalf("write legacy declaration: %v", err)
	}
	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "legacy-system-declaration", Description: "test"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	svc := NewAgentLibraryMaintenanceService(taskRepo, scheduleRepo, agentRepo)
	svc.SetLifecycleRepo(lifecycleRepo)
	svc.SetAgentsRootPath(agentsRoot)
	if err := svc.EnsureProject(ctx, project.ID); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	agent, err := agentRepo.GetBySystemKind(ctx, models.AgentSystemKindSkillCurator)
	if err != nil || agent == nil {
		t.Fatalf("load system agent: %v %#v", err, agent)
	}
	for _, denied := range []string{models.AgentToolScopedFiles, "agent_manage"} {
		if AgentAllowsTool(agent, denied) {
			t.Fatalf("sanitized system agent must not grant %s, tools=%v", denied, agent.Tools)
		}
	}
	if len(agent.ToolConfig.ScopedFiles) != 0 || agent.ToolConfig.SkipDefaultTools || agent.ToolConfig.DisableRuntimeWorktree {
		t.Fatalf("sanitized system agent must not keep scoped files config: %#v", agent.ToolConfig)
	}
	if agent.PermissionDefaults.WriteAgents || agent.PermissionDefaults.ReadRepositoryFiles || agent.PermissionDefaults.WriteRepositoryFiles {
		t.Fatalf("sanitized system agent must not keep agent/repo write permissions: %#v", agent.PermissionDefaults)
	}
}
