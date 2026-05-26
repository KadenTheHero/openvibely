package repository

import (
	"context"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
)

func createLifecycleTestAgent(t *testing.T, agentRepo *AgentRepo) *models.Agent {
	t.Helper()
	a := &models.Agent{
		Name:         "Lifecycle Test Agent",
		Description:  "fixture",
		SystemPrompt: "You help with tests.",
		Model:        "inherit",
		Tools:        []string{"Read"},
	}
	if err := agentRepo.Create(context.Background(), a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return a
}

func TestLifecycleRepo_HookCRUDAndQueryByWhen(t *testing.T) {
	db := testutil.NewTestDB(t)
	agentRepo := NewAgentRepo(db)
	repo := NewLifecycleRepo(db)
	ctx := context.Background()

	agent := createLifecycleTestAgent(t, agentRepo)

	h := &models.AgentLifecycleHook{
		AgentID:        agent.ID,
		When:           models.LifecycleAfterComplete,
		SkillKey:       "memory/observe_task_for_learning",
		OutputContract: models.OutputContractLearningSummary,
		Blocking:       true,
		Enabled:        true,
	}
	if err := repo.CreateHook(ctx, h); err != nil {
		t.Fatalf("create hook: %v", err)
	}
	if h.ID == "" {
		t.Fatalf("expected hook ID set")
	}

	byAgent, err := repo.HooksByAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("hooks by agent: %v", err)
	}
	if len(byAgent) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(byAgent))
	}
	if byAgent[0].When != models.LifecycleAfterComplete {
		t.Fatalf("expected after_complete, got %s", byAgent[0].When)
	}
	if !byAgent[0].Blocking || !byAgent[0].Enabled {
		t.Fatalf("expected blocking+enabled to round-trip true, got %+v", byAgent[0])
	}

	// Seeded system-agent hooks share the after_complete slot, so assert
	// the test hook is present rather than expecting a single-row result.
	byWhen, err := repo.HooksForWhen(ctx, models.LifecycleAfterComplete)
	if err != nil {
		t.Fatalf("hooks for when: %v", err)
	}
	if !containsHookID(byWhen, h.ID) {
		t.Fatalf("expected hook %s in HooksForWhen result, got %+v", h.ID, byWhen)
	}

	// Disabling should remove the hook from HooksForWhen results.
	h.Enabled = false
	if err := repo.UpdateHook(ctx, h); err != nil {
		t.Fatalf("update hook: %v", err)
	}
	byWhen, err = repo.HooksForWhen(ctx, models.LifecycleAfterComplete)
	if err != nil {
		t.Fatalf("hooks for when after disable: %v", err)
	}
	if containsHookID(byWhen, h.ID) {
		t.Fatalf("expected hook %s to be filtered after disable, got %+v", h.ID, byWhen)
	}

	if err := repo.DeleteHook(ctx, h.ID); err != nil {
		t.Fatalf("delete hook: %v", err)
	}
	byAgent, _ = repo.HooksByAgent(ctx, agent.ID)
	if len(byAgent) != 0 {
		t.Fatalf("expected 0 hooks after delete, got %d", len(byAgent))
	}
}

func containsHookID(hooks []models.AgentLifecycleHook, id string) bool {
	for _, h := range hooks {
		if h.ID == id {
			return true
		}
	}
	return false
}

func TestLifecycleRepo_ExecutionLifecycle(t *testing.T) {
	db := testutil.NewTestDB(t)
	agentRepo := NewAgentRepo(db)
	taskRepo := NewTaskRepo(db, nil)
	repo := NewLifecycleRepo(db)
	ctx := context.Background()

	agent := createLifecycleTestAgent(t, agentRepo)

	task := &models.Task{
		ProjectID: "default",
		Title:     "Lifecycle Exec Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "test prompt",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	hook := &models.AgentLifecycleHook{
		AgentID:        agent.ID,
		When:           models.LifecycleBeforeRun,
		SkillKey:       "project_context/load",
		OutputContract: models.OutputContractContextBlock,
		Enabled:        true,
	}
	if err := repo.CreateHook(ctx, hook); err != nil {
		t.Fatalf("create hook: %v", err)
	}

	exec := &models.LifecycleExecution{
		TaskID:          task.ID,
		TaskRunID:       "run-1",
		AgentID:         agent.ID,
		When:            models.LifecycleBeforeRun,
		LifecycleHookID: &hook.ID,
		SkillKey:        hook.SkillKey,
		OutputContract:  hook.OutputContract,
		Status:          models.LifecycleExecRunning,
		AttemptCount:    1,
		InputJSON:       `{"task_id":"t"}`,
	}
	if err := repo.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	if exec.ID == "" {
		t.Fatalf("expected execution ID set")
	}

	completed := time.Now().UTC()
	exec.Status = models.LifecycleExecCompleted
	exec.OutputJSON = `{"content":"context"}`
	exec.CompletedAt = &completed
	if err := repo.UpdateExecution(ctx, exec); err != nil {
		t.Fatalf("update execution: %v", err)
	}

	list, err := repo.ListExecutionsForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(list))
	}
	got := list[0]
	if got.Status != models.LifecycleExecCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.OutputJSON != `{"content":"context"}` {
		t.Fatalf("expected stored output, got %s", got.OutputJSON)
	}
	if got.LifecycleHookID == nil || *got.LifecycleHookID != hook.ID {
		t.Fatalf("expected hook id link, got %+v", got.LifecycleHookID)
	}
	if got.TaskRunID != "run-1" {
		t.Fatalf("expected task_run_id round-trip, got %q", got.TaskRunID)
	}
}

func TestLifecycleRepo_IdempotencyKeyLookup(t *testing.T) {
	db := testutil.NewTestDB(t)
	agentRepo := NewAgentRepo(db)
	taskRepo := NewTaskRepo(db, nil)
	repo := NewLifecycleRepo(db)
	ctx := context.Background()

	agent := createLifecycleTestAgent(t, agentRepo)
	task := &models.Task{ProjectID: "default", Title: "Idemp", Category: models.CategoryActive, Status: models.StatusPending, Prompt: "p"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	exec := &models.LifecycleExecution{
		TaskID:         task.ID,
		TaskRunID:      "run-1",
		AgentID:        agent.ID,
		When:           models.LifecycleAfterComplete,
		SkillKey:       "activity/summarize",
		OutputContract: models.OutputContractActivitySummary,
		Status:         models.LifecycleExecCompleted,
		IdempotencyKey: "run-1:hook-123:deadbeef",
	}
	if err := repo.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("create exec: %v", err)
	}
	got, err := repo.FindExecutionByIdempotencyKey(ctx, exec.IdempotencyKey)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID != exec.ID {
		t.Fatalf("expected same execution, got %+v", got)
	}
	// Empty key returns sql.ErrNoRows so unkeyed rows never collide.
	if _, err := repo.FindExecutionByIdempotencyKey(ctx, ""); err == nil {
		t.Fatalf("expected error for empty key")
	}
}

func TestLifecycleRepo_ExecutionEvents(t *testing.T) {
	db := testutil.NewTestDB(t)
	agentRepo := NewAgentRepo(db)
	taskRepo := NewTaskRepo(db, nil)
	repo := NewLifecycleRepo(db)
	ctx := context.Background()

	agent := createLifecycleTestAgent(t, agentRepo)
	task := &models.Task{ProjectID: "default", Title: "Trace", Category: models.CategoryActive, Status: models.StatusPending, Prompt: "p"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	exec := &models.LifecycleExecution{TaskID: task.ID, AgentID: agent.ID, When: models.LifecycleAfterComplete, Status: models.LifecycleExecRunning}
	if err := repo.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("create exec: %v", err)
	}

	first := &models.LifecycleExecutionEvent{LifecycleExecutionID: exec.ID, EventType: "tool_call", PayloadJSON: `{"name":"skills_list"}`}
	if err := repo.AppendExecutionEvent(ctx, first); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	second := &models.LifecycleExecutionEvent{LifecycleExecutionID: exec.ID, EventType: "tool_result", PayloadJSON: `{"ok":true}`}
	if err := repo.AppendExecutionEvent(ctx, second); err != nil {
		t.Fatalf("append second event: %v", err)
	}

	events, err := repo.ListExecutionEvents(ctx, exec.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Seq != 1 || events[0].EventType != "tool_call" || events[1].Seq != 2 || events[1].EventType != "tool_result" {
		t.Fatalf("unexpected ordered events: %+v", events)
	}
}

func TestLifecycleRepo_IdempotencyAllowsRetryAfterFailure(t *testing.T) {
	db := testutil.NewTestDB(t)
	agentRepo := NewAgentRepo(db)
	taskRepo := NewTaskRepo(db, nil)
	repo := NewLifecycleRepo(db)
	ctx := context.Background()

	agent := createLifecycleTestAgent(t, agentRepo)
	task := &models.Task{ProjectID: "default", Title: "Retry", Category: models.CategoryActive, Status: models.StatusPending, Prompt: "p"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	key := "run-1:hook-x:abc"
	// First attempt fails.
	first := &models.LifecycleExecution{
		TaskID: task.ID, TaskRunID: "run-1", AgentID: agent.ID,
		When: models.LifecycleAfterComplete, SkillKey: "x/y",
		Status: models.LifecycleExecFailed, IdempotencyKey: key,
	}
	if err := repo.CreateExecution(ctx, first); err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	// Retry with same key should be allowed because partial unique index
	// only enforces uniqueness for completed rows.
	second := &models.LifecycleExecution{
		TaskID: task.ID, TaskRunID: "run-1", AgentID: agent.ID,
		When: models.LifecycleAfterComplete, SkillKey: "x/y",
		Status: models.LifecycleExecRunning, IdempotencyKey: key,
	}
	if err := repo.CreateExecution(ctx, second); err != nil {
		t.Fatalf("retry should be allowed, got %v", err)
	}
	// Completing second is OK because no other completed row holds the key.
	completed := time.Now().UTC()
	second.Status = models.LifecycleExecCompleted
	second.CompletedAt = &completed
	if err := repo.UpdateExecution(ctx, second); err != nil {
		t.Fatalf("update second: %v", err)
	}
	// A third completed row with same key MUST be rejected.
	third := &models.LifecycleExecution{
		TaskID: task.ID, TaskRunID: "run-1", AgentID: agent.ID,
		When: models.LifecycleAfterComplete, SkillKey: "x/y",
		Status: models.LifecycleExecCompleted, IdempotencyKey: key,
	}
	if err := repo.CreateExecution(ctx, third); err == nil {
		t.Fatalf("expected unique-index violation for second completed row with same key")
	}
}
