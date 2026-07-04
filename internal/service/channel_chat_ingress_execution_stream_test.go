package service

import (
	"context"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/events"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func TestCompleteChannelExecutionDoesNotPublishTerminalWhenExecutionCompleteFails(t *testing.T) {
	db := testutil.NewTestDB(t)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	hub := events.NewExecutionStreamHub()
	sub, unsubscribe, err := hub.Subscribe("exec-channel-failed-complete")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsubscribe()

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	completeChannelExecution(context.Background(), channelExecutionCompletionOptions{
		Platform:           "test",
		ExecRepo:           execRepo,
		TaskRepo:           taskRepo,
		ExecutionStreamHub: hub,
		ExecID:             "exec-channel-failed-complete",
		TaskID:             "task-channel-failed-complete",
		Output:             "should not complete",
		DurationMs:         1,
	})

	select {
	case event := <-sub:
		t.Fatalf("unexpected terminal event after failed durable complete: %#v", event)
	case <-time.After(25 * time.Millisecond):
	}
}
