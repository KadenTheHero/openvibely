package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/events"
	"github.com/openvibely/openvibely/internal/lifecycle"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

// TestWorkerService_TaskClaimedBeforeLifecycleHooks verifies the fix for the
// stale-kanban bug: when the worker picks up a pending task, it must claim
// (set status -> running) BEFORE invoking lifecycle hooks (route_task /
// before_run). Otherwise the Tasks page shows the task as "queued" while
// expensive lifecycle hooks run (which may invoke an LLM), and the dropzone
// only updates after the user manually refreshes.
//
// The test:
//  1. Sets up a worker with a lifecycle runner whose route_task invoker
//     blocks until released, then reads the task's status from the DB.
//  2. Submits a pending task.
//  3. Asserts that, by the time the route_task invoker is called, the task
//     status is already "running" — proving the claim happened first.
//  4. Asserts that a TaskStatusChanged(pending -> running) event was
//     published before the lifecycle hook fired.
func TestWorkerService_TaskClaimedBeforeLifecycleHooks(t *testing.T) {
	db := testutil.NewTestDB(t)
	broadcaster := events.NewBroadcaster()
	taskRepo := repository.NewTaskRepo(db, broadcaster)
	projectRepo := repository.NewProjectRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	ctx := context.Background()

	// Subscribe to events BEFORE dispatching.
	sub, err := broadcaster.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer broadcaster.Unsubscribe(sub)

	// Project + agent fixtures.
	project := &models.Project{Name: "claim-before-hooks"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("Create project: %v", err)
	}
	agent := &models.LLMConfig{
		Name:       "Test Agent",
		Provider:   models.ProviderTest,
		Model:      "test-model",
		MaxTokens:  4096,
		AuthMethod: models.AuthMethodCLI,
		IsDefault:  true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	// Pending active task.
	task := &models.Task{
		ProjectID: project.ID,
		Title:     "Claim Before Hooks",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "test prompt",
		AgentID:   &agent.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("Create task: %v", err)
	}

	// LLM service with a mock caller that returns instantly after the
	// lifecycle hook releases the worker.
	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, nil, attachmentRepo)
	llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())

	// Build a lifecycle runner whose route_task invoker captures the task
	// status visible at hook invocation time. The invoker blocks briefly
	// to simulate a slow LLM-backed hook so the bug (status still pending
	// during hook execution) would be visible without the fix.
	hookEntered := make(chan struct{})
	releaseHook := make(chan struct{})
	var (
		mu                sync.Mutex
		statusAtHookEntry models.TaskStatus
	)
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{
		ID:             "route-hook",
		When:           models.LifecycleRouteTask,
		SkillKey:       "route_task",
		OutputContract: models.OutputContractSelectedSkills,
		Blocking:       true,
		Enabled:        true,
	}}}
	invoker := routeHookInvokerFunc(func(invCtx context.Context, _ models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		// Snapshot status visible to the hook.
		dbTask, _ := taskRepo.GetByID(invCtx, in.TaskID)
		mu.Lock()
		if dbTask != nil {
			statusAtHookEntry = dbTask.Status
		}
		mu.Unlock()

		// Signal the test that we entered the hook, then block until released.
		select {
		case hookEntered <- struct{}{}:
		default:
		}
		<-releaseHook

		// Return an empty selected_skills payload — route_task is optional.
		return json.RawMessage(`{"skills":[],"confidence":0.0}`), nil
	})
	runner := lifecycle.NewRunner(store, invoker, nil)

	ws := NewWorkerService(llmSvc, 10, projectRepo)
	ws.SetLLMConfigRepo(llmConfigRepo)
	ws.SetTaskRepo(taskRepo)
	ws.SetLifecycleRunner(runner)
	ws.SetLifecycleRepo(repository.NewLifecycleRepo(db))
	ws.SetExecutionRepo(execRepo)
	ws.Start(ctx)
	defer ws.Stop()

	ws.Submit(*task)

	// Wait until the route_task hook is invoked.
	select {
	case <-hookEntered:
	case <-time.After(5 * time.Second):
		close(releaseHook)
		t.Fatal("timed out waiting for route_task hook to be invoked")
	}

	// At hook entry, the task MUST already be in running status. Before
	// the fix it would still be 'pending', causing the kanban board to
	// show it stuck in the queued sub-zone.
	mu.Lock()
	got := statusAtHookEntry
	mu.Unlock()
	if got != models.StatusRunning {
		close(releaseHook)
		t.Fatalf("expected task to be running when lifecycle hook fires, got %q", got)
	}

	// A TaskStatusChanged(pending -> running) event must have already been
	// published by the pre-claim, BEFORE the lifecycle hook returned. This
	// is what drives the live kanban update.
	var sawRunningEvent bool
drain:
	for {
		select {
		case ev := <-sub:
			if ev.Type == events.TaskStatusChanged && ev.TaskID == task.ID &&
				ev.OldStatus == string(models.StatusPending) &&
				ev.Status == string(models.StatusRunning) {
				sawRunningEvent = true
				break drain
			}
		case <-time.After(500 * time.Millisecond):
			break drain
		}
	}
	if !sawRunningEvent {
		close(releaseHook)
		t.Fatal("expected TaskStatusChanged(pending->running) event to be published before lifecycle hook completed")
	}

	// Release the hook so the worker can finish the task and shut down cleanly.
	close(releaseHook)
}
