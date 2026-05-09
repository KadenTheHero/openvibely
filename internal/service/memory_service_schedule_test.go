package service

import (
	"context"
	"os"
	"testing"

	"github.com/openvibely/openvibely/internal/memory"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type memoryScheduleTestRepos struct {
	tasks     *repository.TaskRepo
	schedules *repository.ScheduleRepo
	agents    *repository.AgentRepo
	projectID string
}

func newMemoryScheduleTestService(t *testing.T) (*MemoryService, memoryScheduleTestRepos) {
	t.Helper()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "Memory test", RepoPath: t.TempDir()}
	if err := projectRepo.Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	agentRepo := repository.NewAgentRepo(db)
	memoryRepo := repository.NewMemoryRepo(db)
	resolver, err := memory.NewPathResolver(t.TempDir(), "")
	if err != nil {
		t.Fatalf("path resolver: %v", err)
	}
	store := memory.NewFileStore(resolver)
	svc := NewMemoryService(memoryRepo, taskRepo, scheduleRepo, agentRepo, nil, projectRepo, nil, nil, store, resolver)
	return svc, memoryScheduleTestRepos{tasks: taskRepo, schedules: scheduleRepo, agents: agentRepo, projectID: project.ID}
}

func TestMemoryServiceEnsureProjectUsesRepoMemoryDir(t *testing.T) {
	ctx := context.Background()
	svc, repos := newMemoryScheduleTestService(t)
	project, err := svc.projectRepo.GetByID(ctx, repos.projectID)
	if err != nil || project == nil {
		t.Fatalf("get project: %v", err)
	}
	wantDir, err := memory.SharedRepoMemoryDir(project.RepoPath)
	if err != nil {
		t.Fatalf("shared dir: %v", err)
	}
	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project: %v", err)
	}
	got, err := svc.ProjectDir(repos.projectID)
	if err != nil {
		t.Fatalf("project dir: %v", err)
	}
	if got != wantDir {
		t.Fatalf("project dir = %q, want repo memory dir %q", got, wantDir)
	}
	if info, err := os.Stat(wantDir); err != nil || !info.IsDir() {
		t.Fatalf("expected repo memory dir to exist: info=%v err=%v", info, err)
	}
}

func TestMemoryServiceEnsureProjectCreatesMemoryConsolidatorAgentAndSchedule(t *testing.T) {
	ctx := context.Background()
	svc, repos := newMemoryScheduleTestService(t)

	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project: %v", err)
	}

	agent, err := repos.agents.GetBySystemKind(ctx, models.AgentSystemKindMemoryConsolidator)
	if err != nil {
		t.Fatalf("get memory consolidator agent: %v", err)
	}
	if agent == nil {
		t.Fatal("expected memory consolidator agent")
	}
	if !agentHasTool(agent.Tools, models.AgentToolScopedFiles) {
		t.Fatalf("expected memory consolidator agent to show %q tool in modal, got %v", models.AgentToolScopedFiles, agent.Tools)
	}
	if len(agent.ToolConfig.ScopedFiles) != 1 || agent.ToolConfig.ScopedFiles[0].Directory != ".openvibely/memory" || !agent.ToolConfig.SkipDefaultTools || !agent.ToolConfig.DisableRuntimeWorktree {
		t.Fatalf("unexpected scoped file config: %+v", agent.ToolConfig)
	}
	if agent.SystemPrompt == "" || agent.Name != memoryConsolidatorAgentName {
		t.Fatalf("unexpected built-in agent: %#v", agent)
	}

	tasks, err := repos.tasks.ListByProject(ctx, repos.projectID, string(models.CategoryScheduled))
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one scheduled task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.AgentDefinitionID == nil || *task.AgentDefinitionID != agent.ID {
		t.Fatalf("scheduled task agent = %v, want %s", task.AgentDefinitionID, agent.ID)
	}
	if task.CreatedVia != models.TaskOriginWeb {
		t.Fatalf("scheduled task created_via = %q, want normal web origin", task.CreatedVia)
	}
	schedules, err := repos.schedules.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(schedules) != 1 || schedules[0].RepeatType != models.RepeatDaily || schedules[0].RepeatInterval != 1 {
		t.Fatalf("unexpected schedule: %#v", schedules)
	}
}

func TestMemoryServiceEnsureProjectDoesNotRewriteUserScheduledTaskAssignedToMemoryAgent(t *testing.T) {
	ctx := context.Background()
	svc, repos := newMemoryScheduleTestService(t)

	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project first: %v", err)
	}
	agent, err := repos.agents.GetBySystemKind(ctx, models.AgentSystemKindMemoryConsolidator)
	if err != nil || agent == nil {
		t.Fatalf("get memory agent: %v", err)
	}
	agentID := agent.ID
	userTask := &models.Task{
		ProjectID:         repos.projectID,
		Title:             "User scheduled memory review",
		Category:          models.CategoryScheduled,
		Status:            models.StatusPending,
		Prompt:            "Use the memory agent for a manual review.",
		AgentDefinitionID: &agentID,
		Tag:               models.TagNone,
		ChainConfig:       "{}",
		CreatedVia:        models.TaskOriginWeb,
	}
	if err := repos.tasks.Create(ctx, userTask); err != nil {
		t.Fatalf("create user scheduled task: %v", err)
	}

	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project second: %v", err)
	}
	got, err := repos.tasks.GetByID(ctx, userTask.ID)
	if err != nil {
		t.Fatalf("get user task: %v", err)
	}
	if got.Title != userTask.Title || got.Prompt != userTask.Prompt {
		t.Fatalf("user task was rewritten: %#v", got)
	}
	systemTask, err := repos.tasks.GetByProjectAndTitle(ctx, repos.projectID, memoryConsolidationTaskTitle)
	if err != nil || systemTask == nil {
		t.Fatalf("get system task: task=%#v err=%v", systemTask, err)
	}
	if systemTask.ID == userTask.ID {
		t.Fatal("system task reused user task id")
	}
}

func TestMemoryServiceEnsureProjectIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, repos := newMemoryScheduleTestService(t)

	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project first: %v", err)
	}
	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project second: %v", err)
	}

	agents, err := repos.agents.List(ctx)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	memoryAgents := 0
	for _, agent := range agents {
		if agent.SystemKind == models.AgentSystemKindMemoryConsolidator {
			memoryAgents++
		}
	}
	if memoryAgents != 1 {
		t.Fatalf("expected one memory consolidator agent, got %d", memoryAgents)
	}
	tasks, err := repos.tasks.ListByProject(ctx, repos.projectID, string(models.CategoryScheduled))
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one scheduled task, got %d", len(tasks))
	}
	schedules, err := repos.schedules.ListByTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected one schedule, got %d", len(schedules))
	}
}
