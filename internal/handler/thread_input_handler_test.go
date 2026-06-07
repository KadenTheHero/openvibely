package handler

import (
	"net/http"
	"testing"
)

// threadInputRepo is wired in NewTestContext (derived from execRepo.DB()).

func TestCancelThreadInput_NonExistentID(t *testing.T) {
	tc := NewTestContext(t)
	// CancelPending on a missing ID returns ErrInputNotPending → 409 Conflict.
	rec := tc.HTTP().Post("/thread-inputs/nonexistent-input/cancel").Execute()
	tc.Assert(rec).StatusCode(http.StatusConflict)
}

func TestChatQueuedInputSteer_NonExistentID(t *testing.T) {
	tc := NewTestContext(t)
	// GetByID returns (nil, nil) for missing ID → 409 "queued input is no longer pending"
	rec := tc.HTTP().Post("/chat/queued/nonexistent-input/steer").Execute()
	tc.Assert(rec).StatusCode(http.StatusConflict)
}

func TestTaskThreadQueuedInputSteer_NonExistentInput(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	task := tc.CreateTask(p.ID).Build()
	// Input doesn't exist → GetByID returns nil → 409
	rec := tc.HTTP().Post("/tasks/" + task.ID + "/thread/queued/nonexistent-input/steer").Execute()
	tc.Assert(rec).StatusCode(http.StatusConflict)
}

func TestPublishThreadInputCancelledEvent_NilInput(t *testing.T) {
	tc := NewTestContext(t)
	// publishThreadInputCancelledEvent with nil input is a no-op; call it to exercise the nil guard.
	tc.handler.publishThreadInputCancelledEvent(nil)
}
