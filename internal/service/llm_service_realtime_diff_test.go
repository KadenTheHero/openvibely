package service

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/events"
)

type recordingDiffRepo struct {
	updates []string
	err     error
}

func (r *recordingDiffRepo) UpdateDiffOutput(ctx context.Context, id string, diffOutput string) error {
	r.updates = append(r.updates, diffOutput)
	return r.err
}

func TestPublishDiffSnapshotIfChangedSkipsIdenticalPeriodicDiffs(t *testing.T) {
	ctx := context.Background()
	repo := &recordingDiffRepo{}
	broadcaster := events.NewFileChangeBroadcaster()
	sub, err := broadcaster.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer broadcaster.Unsubscribe(sub)
	state := &DiffSnapshotState{}

	if !PublishDiffSnapshotIfChanged(ctx, repo, broadcaster, state, "task-1", "exec-1", "diff one", false) {
		t.Fatal("expected first periodic diff to publish")
	}
	assertDiffSnapshotEvent(t, sub, "task-1", "exec-1")
	if got := len(repo.updates); got != 1 {
		t.Fatalf("expected first diff to write once, got %d writes", got)
	}

	if PublishDiffSnapshotIfChanged(ctx, repo, broadcaster, state, "task-1", "exec-1", "diff one", false) {
		t.Fatal("expected identical periodic diff to be skipped")
	}
	assertNoDiffSnapshotEvent(t, sub)
	if got := len(repo.updates); got != 1 {
		t.Fatalf("expected identical diff to skip DB write, got %d writes", got)
	}
}

func TestPublishDiffSnapshotIfChangedWritesAndPublishesChangedDiffs(t *testing.T) {
	ctx := context.Background()
	repo := &recordingDiffRepo{}
	broadcaster := events.NewFileChangeBroadcaster()
	sub, err := broadcaster.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer broadcaster.Unsubscribe(sub)
	state := &DiffSnapshotState{}

	if !PublishDiffSnapshotIfChanged(ctx, repo, broadcaster, state, "task-1", "exec-1", "diff one", false) {
		t.Fatal("expected first periodic diff to publish")
	}
	assertDiffSnapshotEvent(t, sub, "task-1", "exec-1")

	if !PublishDiffSnapshotIfChanged(ctx, repo, broadcaster, state, "task-1", "exec-1", "diff two", false) {
		t.Fatal("expected changed periodic diff to publish")
	}
	assertDiffSnapshotEvent(t, sub, "task-1", "exec-1")
	if got := len(repo.updates); got != 2 {
		t.Fatalf("expected changed diff to write again, got %d writes", got)
	}
	if got := repo.updates[1]; got != "diff two" {
		t.Fatalf("expected changed diff to be persisted, got %q", got)
	}
}

func TestPublishDiffSnapshotIfChangedFinalSnapshotStillWritesAndPublishes(t *testing.T) {
	ctx := context.Background()
	repo := &recordingDiffRepo{}
	broadcaster := events.NewFileChangeBroadcaster()
	sub, err := broadcaster.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer broadcaster.Unsubscribe(sub)
	state := &DiffSnapshotState{}

	if !PublishDiffSnapshotIfChanged(ctx, repo, broadcaster, state, "task-1", "exec-1", "diff one", false) {
		t.Fatal("expected first periodic diff to publish")
	}
	assertDiffSnapshotEvent(t, sub, "task-1", "exec-1")

	if !PublishDiffSnapshotIfChanged(ctx, repo, broadcaster, state, "task-1", "exec-1", "diff one", true) {
		t.Fatal("expected final diff snapshot to publish even when unchanged")
	}
	assertDiffSnapshotEvent(t, sub, "task-1", "exec-1")
	if got := len(repo.updates); got != 2 {
		t.Fatalf("expected final diff to write even when unchanged, got %d writes", got)
	}
}

func assertDiffSnapshotEvent(t *testing.T, sub events.FileChangeSubscriber, taskID, execID string) {
	t.Helper()
	select {
	case evt := <-sub:
		if evt.Type != events.DiffSnapshot {
			t.Fatalf("expected DiffSnapshot event, got %s", evt.Type)
		}
		if evt.TaskID != taskID || evt.ExecID != execID {
			t.Fatalf("expected task=%s exec=%s, got task=%s exec=%s", taskID, execID, evt.TaskID, evt.ExecID)
		}
	default:
		t.Fatal("expected diff_snapshot event")
	}
}

func assertNoDiffSnapshotEvent(t *testing.T, sub events.FileChangeSubscriber) {
	t.Helper()
	select {
	case evt := <-sub:
		t.Fatalf("expected no diff_snapshot event, got %#v", evt)
	default:
	}
}
