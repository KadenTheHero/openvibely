package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/openvibely/openvibely/internal/memory"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type chatExclusionRepos struct {
	db        *sql.DB
	tasks     *repository.TaskRepo
	execs     *repository.ExecutionRepo
	memory    *repository.MemoryRepo
	llm       *repository.LLMConfigRepo
	projectID string
}

// newChatExclusionService is a memory service wired with real execRepo so we
// can verify that the consolidation source excludes Chat-category executions
// and that runExtraction silently no-ops for Chat surfaces.
func newChatExclusionService(t *testing.T) (*MemoryService, chatExclusionRepos) {
	t.Helper()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "Chat exclusion", RepoPath: t.TempDir()}
	if err := projectRepo.Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	agentRepo := repository.NewAgentRepo(db)
	memoryRepo := repository.NewMemoryRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	llmRepo := repository.NewLLMConfigRepo(db)
	resolver, err := memory.NewPathResolver(t.TempDir(), "")
	if err != nil {
		t.Fatalf("path resolver: %v", err)
	}
	store := memory.NewFileStore(resolver)
	svc := NewMemoryService(memoryRepo, taskRepo, scheduleRepo, agentRepo, nil, projectRepo, execRepo, nil, store, resolver)
	return svc, chatExclusionRepos{db: db, tasks: taskRepo, execs: execRepo, memory: memoryRepo, llm: llmRepo, projectID: project.ID}
}

// TestRecentProjectExecutions_ExcludesChat asserts that the consolidation
// prompt source skips Chat-category executions so Chat page prompts and mode
// scaffolding (Orchestrate/Plan, "Switch to Orchestrate", <proposed_plan>)
// never reach the consolidator.
func TestRecentProjectExecutions_ExcludesChat(t *testing.T) {
	svc, repos := newChatExclusionService(t)
	ctx := context.Background()
	agent, err := repos.llm.GetDefault(ctx)
	if err != nil || agent == nil {
		t.Fatalf("default agent: %v (agent=%v)", err, agent)
	}
	chatTask := &models.Task{ProjectID: repos.projectID, Title: "Chat 00:00:00.000: hello", Category: models.CategoryChat, Status: models.StatusPending, Prompt: "switch to orchestrate"}
	if err := repos.tasks.Create(ctx, chatTask); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	taskA := &models.Task{ProjectID: repos.projectID, Title: "Real task", Category: models.CategoryActive, Status: models.StatusPending, Prompt: "do work"}
	if err := repos.tasks.Create(ctx, taskA); err != nil {
		t.Fatalf("create task: %v", err)
	}
	for _, e := range []*models.Execution{
		{TaskID: chatTask.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "Plan: refactor"},
		{TaskID: taskA.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "implement feature"},
	} {
		if err := repos.execs.Create(ctx, e); err != nil {
			t.Fatalf("create exec: %v", err)
		}
		if err := repos.execs.Complete(ctx, e.ID, models.ExecCompleted, "out", "", 1, 1); err != nil {
			t.Fatalf("complete exec: %v", err)
		}
	}

	execs := svc.recentProjectExecutions(ctx, repos.projectID, 50)
	if len(execs) != 1 {
		t.Fatalf("expected 1 non-chat execution, got %d (%v)", len(execs), execs)
	}
	if execs[0].TaskID != taskA.ID {
		t.Fatalf("expected only task execution, got task=%s prompt=%q", execs[0].TaskID, execs[0].PromptSent)
	}
}

// TestRunExtraction_ChatSurfaceRecordsNothing verifies that when a chat
// interaction is enqueued for extraction the run is recorded as "nothing"
// with the chat-surface skip reason, and no model-backed extraction is
// attempted. This guards against Chat prompts ever producing memory writes
// even if a caller forgets to short-circuit at the handler layer.
func TestRunExtraction_ChatSurfaceRecordsNothing(t *testing.T) {
	svc, repos := newChatExclusionService(t)
	ctx := context.Background()
	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project: %v", err)
	}
	in := memory.Interaction{
		ProjectID:    repos.projectID,
		SourceKind:   memory.SourceChat,
		SourceID:     "exec-chat-1",
		UserText:     "Switch to Orchestrate and dispatch the plan as tasks.",
		AssistantOut: "<proposed_plan>step 1</proposed_plan>",
	}
	if err := svc.runExtraction(ctx, in); err != nil {
		t.Fatalf("runExtraction: %v", err)
	}
	runs, err := repos.memory.ListRecentExtractionRuns(ctx, repos.projectID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly 1 recorded run, got %d", len(runs))
	}
	if runs[0].Status != "nothing" {
		t.Fatalf("expected status=nothing, got %q", runs[0].Status)
	}
	if runs[0].Reason != string(memory.SkipChatSurface) {
		t.Fatalf("expected reason=%q, got %q", memory.SkipChatSurface, runs[0].Reason)
	}
}

// TestRunExtraction_APIChatSurfaceRecordsNothing covers the /api/chat/message
// surface symmetrically.
func TestRunExtraction_APIChatSurfaceRecordsNothing(t *testing.T) {
	svc, repos := newChatExclusionService(t)
	ctx := context.Background()
	if err := svc.EnsureProject(ctx, repos.projectID); err != nil {
		t.Fatalf("ensure project: %v", err)
	}
	in := memory.Interaction{
		ProjectID:  repos.projectID,
		SourceKind: memory.SourceAPIChat,
		SourceID:   "exec-api-chat-1",
		UserText:   "Plan how to migrate auth, then I'll switch to orchestrate.",
	}
	if err := svc.runExtraction(ctx, in); err != nil {
		t.Fatalf("runExtraction: %v", err)
	}
	runs, err := repos.memory.ListRecentExtractionRuns(ctx, repos.projectID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "nothing" || runs[0].Reason != string(memory.SkipChatSurface) {
		t.Fatalf("expected single nothing/chat-surface run, got %+v", runs)
	}
}
