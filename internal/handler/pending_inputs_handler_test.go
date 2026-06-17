package handler

// Tests for the ChatPendingInputs and TaskThreadPendingInputs endpoints.
//
// These endpoints are called on SSE reconnect (e.g. Chrome tab blur/refocus) to
// reconcile the #pending-thread-inputs container. The server-side query must exclude
// prepared/in-flight steering rows (expected_turn_id=NULL, input_mode=steering,
// input_status=pending) so that stale "Steering pending" rows are removed from the
// UI without requiring a manual page refresh.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

// ---------------------------------------------------------------------------
// Chat surface
// ---------------------------------------------------------------------------

func TestChatPendingInputs_NoPendingInputs(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()

	rec := tc.HTTP().Get("/chat/pending-inputs?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	// Container should be present even when empty so HTMX outerHTML swap works.
	if !strings.Contains(body, `id="pending-thread-inputs"`) {
		t.Errorf("expected #pending-thread-inputs in response, got: %q", body)
	}
}

func TestChatPendingInputs_ExcludesPreparedInFlightSteering(t *testing.T) {
	// Regression: prepared/in-flight steering (expected_turn_id=NULL) must NOT appear
	// in the pending-inputs fragment returned on reconnect. The steering row was
	// already consumed by the provider; showing it again would be a stale ghost.
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()
	// Chat steering requires a chat-category task (exec must join a chat task).
	task := tc.CreateTask(p.ID).WithCategory(models.CategoryChat).Build()

	agent, _ := tc.llmConfigRepo.GetDefault(ctx)
	if agent == nil {
		t.Skip("no default agent configured")
	}

	// Create a synthetic active execution backed by the chat task.
	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        models.ExecRunning,
		PromptSent:    "active",
	}
	if err := tc.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	// Create a valid pending steering row.
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeChat,
		ProjectID:      p.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		RunExecutionID: exec.ID,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "valid-steer",
	}
	if err := tc.handler.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID); err != nil {
		t.Fatalf("create steering: %v", err)
	}

	// Simulate prepare (two-phase consumption): clears expected_turn_id → NULL.
	if _, err := tc.handler.threadInputRepo.PreparePendingSteering(ctx, exec.ID, exec.ID); err != nil {
		t.Fatalf("prepare steering: %v", err)
	}

	rec := tc.HTTP().Get("/chat/pending-inputs?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if strings.Contains(body, "valid-steer") {
		t.Errorf("prepared/in-flight steering row must be excluded from pending-inputs fragment, got: %q", body)
	}
}

func TestChatPendingInputs_IncludesValidPendingSteering(t *testing.T) {
	// A steering row that has NOT been prepared yet (expected_turn_id still set)
	// should appear in the pending-inputs fragment.
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).WithCategory(models.CategoryChat).Build()

	agent, _ := tc.llmConfigRepo.GetDefault(ctx)
	if agent == nil {
		t.Skip("no default agent configured")
	}

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        models.ExecRunning,
		PromptSent:    "active",
	}
	if err := tc.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeChat,
		ProjectID:      p.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		RunExecutionID: exec.ID,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "unprepared-steer",
	}
	if err := tc.handler.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID); err != nil {
		t.Fatalf("create steering: %v", err)
	}

	rec := tc.HTTP().Get("/chat/pending-inputs?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "unprepared-steer") {
		t.Errorf("valid (unprepared) pending steering must appear in pending-inputs fragment, got: %q", body)
	}
}

func TestChatPendingInputs_IncludesQueuedInput(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()

	queued := &models.ThreadInput{
		Scope:       models.ThreadInputScopeChat,
		ProjectID:   p.ID,
		InputMode:   models.ThreadInputModeQueued,
		InputStatus: models.ThreadInputPending,
		Content:     "queued-chat-msg",
	}
	if err := tc.handler.threadInputRepo.CreateQueued(ctx, queued); err != nil {
		t.Fatalf("create queued: %v", err)
	}

	rec := tc.HTTP().Get("/chat/pending-inputs?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "queued-chat-msg") {
		t.Errorf("queued input must appear in pending-inputs fragment, got: %q", body)
	}
}

// ---------------------------------------------------------------------------
// Task thread surface
// ---------------------------------------------------------------------------

func TestTaskThreadPendingInputs_NoPendingInputs(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	rec := tc.HTTP().Get("/tasks/" + task.ID + "/thread/pending-inputs").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, `id="pending-thread-inputs"`) {
		t.Errorf("expected #pending-thread-inputs in response, got: %q", body)
	}
}

func TestTaskThreadPendingInputs_ExcludesPreparedInFlightSteering(t *testing.T) {
	// Regression: prepared/in-flight steering must not appear in the fragment.
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	agent, _ := tc.llmConfigRepo.GetDefault(ctx)
	if agent == nil {
		t.Skip("no default agent configured")
	}

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        models.ExecRunning,
		PromptSent:    "active",
	}
	if err := tc.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      p.ID,
		TaskID:         task.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		RunExecutionID: exec.ID,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "task-valid-steer",
	}
	if err := tc.handler.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID); err != nil {
		t.Fatalf("create steering: %v", err)
	}

	if _, err := tc.handler.threadInputRepo.PreparePendingSteering(ctx, exec.ID, exec.ID); err != nil {
		t.Fatalf("prepare steering: %v", err)
	}

	rec := tc.HTTP().Get("/tasks/" + task.ID + "/thread/pending-inputs").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if strings.Contains(body, "task-valid-steer") {
		t.Errorf("prepared/in-flight steering row must be excluded from task pending-inputs fragment, got: %q", body)
	}
}

func TestTaskThreadPendingInputs_IncludesValidPendingSteering(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	agent, _ := tc.llmConfigRepo.GetDefault(ctx)
	if agent == nil {
		t.Skip("no default agent configured")
	}

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        models.ExecRunning,
		PromptSent:    "active",
	}
	if err := tc.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      p.ID,
		TaskID:         task.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		RunExecutionID: exec.ID,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "task-unprepared-steer",
	}
	if err := tc.handler.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID); err != nil {
		t.Fatalf("create steering: %v", err)
	}

	rec := tc.HTTP().Get("/tasks/" + task.ID + "/thread/pending-inputs").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "task-unprepared-steer") {
		t.Errorf("valid (unprepared) pending steering must appear in task pending-inputs fragment, got: %q", body)
	}
}

func TestTaskThreadPendingInputs_IncludesQueuedInput(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	queued := &models.ThreadInput{
		Scope:       models.ThreadInputScopeTask,
		ProjectID:   p.ID,
		TaskID:      task.ID,
		InputMode:   models.ThreadInputModeQueued,
		InputStatus: models.ThreadInputPending,
		Content:     "queued-task-msg",
	}
	if err := tc.handler.threadInputRepo.CreateQueued(ctx, queued); err != nil {
		t.Fatalf("create queued: %v", err)
	}

	rec := tc.HTTP().Get("/tasks/" + task.ID + "/thread/pending-inputs").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "queued-task-msg") {
		t.Errorf("queued input must appear in task pending-inputs fragment, got: %q", body)
	}
}

func TestTaskThreadPendingInputs_SteerEndpointUsesTaskID(t *testing.T) {
	// Ensures the generated Steer button href uses the task ID so converted-queued
	// rows can be promoted through the correct /tasks/:taskId/thread/queued/:id/steer path.
	tc := NewTestContext(t)
	ctx := context.Background()
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()

	queued := &models.ThreadInput{
		Scope:       models.ThreadInputScopeTask,
		ProjectID:   p.ID,
		TaskID:      task.ID,
		InputMode:   models.ThreadInputModeQueued,
		InputStatus: models.ThreadInputPending,
		Content:     "steer-endpoint-check",
	}
	if err := tc.handler.threadInputRepo.CreateQueued(ctx, queued); err != nil {
		t.Fatalf("create queued: %v", err)
	}

	rec := tc.HTTP().Get("/tasks/" + task.ID + "/thread/pending-inputs").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
	body := rec.Body.String()
	expectedPath := "/tasks/" + task.ID + "/thread/queued/" + queued.ID + "/steer"
	if !strings.Contains(body, expectedPath) {
		t.Errorf("task pending-inputs fragment must contain steer endpoint %q, got: %q", expectedPath, body)
	}
}
