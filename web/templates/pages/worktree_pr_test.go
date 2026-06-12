package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestTaskChangesWorktreeContent_RendersCreatePRInGitHubSection(t *testing.T) {
	task := &models.Task{ID: "task-1", WorktreeBranch: "task/feature", MergeStatus: models.MergeStatusPending}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Create PR") {
		t.Fatal("expected Create PR item in dropdown")
	}
	if !strings.Contains(out, "GitHub") {
		t.Fatal("expected GitHub section header in dropdown")
	}
	if !strings.Contains(out, `hx-indicator="#create-pr-indicator"`) {
		t.Fatal("expected Create PR action to include htmx indicator slot for consistent indentation")
	}
	if !strings.Contains(out, `id="create-pr-indicator"`) {
		t.Fatal("expected Create PR indicator element in dropdown action")
	}
}

func TestTaskChangesWorktreeContent_RendersViewPRInGitHubSection(t *testing.T) {
	task := &models.Task{ID: "task-1", WorktreeBranch: "task/feature", MergeStatus: models.MergeStatusPending}
	pr := &models.TaskPullRequest{TaskID: task.ID, PRNumber: 42, PRURL: "https://github.com/openvibely/openvibely/pull/42", PRState: "open"}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, pr, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "View PR #42") {
		t.Fatal("expected View PR link in dropdown")
	}
	// The View PR action must point at the absolute GitHub PR URL and use
	// target="_blank" so the desktop bridge (in base layout) routes the click
	// to the system browser instead of relying on Wails WebView new-window
	// navigation, which silently drops the click.
	if !strings.Contains(out, `href="https://github.com/openvibely/openvibely/pull/42"`) {
		t.Fatal("expected View PR anchor to use absolute GitHub PR URL")
	}
	if !strings.Contains(out, `target="_blank"`) {
		t.Fatal("expected View PR anchor to use target=\"_blank\" so the desktop bridge can intercept it")
	}
	if strings.Contains(out, "Create PR") {
		t.Fatal("did not expect Create PR when PR exists")
	}
	if !strings.Contains(out, "GitHub") {
		t.Fatal("expected GitHub section header in dropdown")
	}
	if !strings.Contains(out, `<span class="htmx-indicator"><span class="loading loading-spinner loading-xs"></span></span>`) {
		t.Fatal("expected View PR action to include indicator-width slot for consistent indentation")
	}
}

func TestTaskChangesWorktreeContent_LocalAndGitHubSections(t *testing.T) {
	task := &models.Task{ID: "task-1", WorktreeBranch: "task/feature", MergeStatus: models.MergeStatusPending}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Local") {
		t.Fatal("expected Local section header in dropdown")
	}
	if !strings.Contains(out, "GitHub") {
		t.Fatal("expected GitHub section header in dropdown")
	}
	if !strings.Contains(out, "Merge commit") {
		t.Fatal("expected Merge commit option in Local section")
	}
	if !strings.Contains(out, "Fast-forward only") {
		t.Fatal("expected Fast-forward only option in Local section")
	}
	if !strings.Contains(out, "Squash merge") {
		t.Fatal("expected Squash merge option in Local section")
	}
	if !strings.Contains(out, `hx-target="#changes-content"`) {
		t.Fatal("expected Changes-tab local merge actions to target the visible changes content")
	}
	if !strings.Contains(out, `type="button"`) {
		t.Fatal("expected Changes-tab local merge actions to be explicit buttons so clicks cannot fall back to form submission")
	}
	if !strings.Contains(out, "Create PR") {
		t.Fatal("expected Create PR in GitHub section")
	}
}

func TestTaskChangesWorktreeContent_RebaseOnlyWhenAvailable(t *testing.T) {
	task := &models.Task{ID: "task-1", WorktreeBranch: "task/feature", MergeStatus: models.MergeStatusPending}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, nil, false, true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "/worktree/rebase") {
		t.Fatal("expected Rebase action when task branch is behind target")
	}
	if !strings.Contains(out, "Rebase onto main") {
		t.Fatal("expected Rebase action label to include target branch")
	}
	if !strings.Contains(out, `hx-disabled-elt="this"`) {
		t.Fatal("expected Rebase action to disable while request is in flight")
	}
}

func TestTaskChangesWorktreeContent_RebaseHiddenWhenUnavailable(t *testing.T) {
	task := &models.Task{ID: "task-1", WorktreeBranch: "task/feature", MergeStatus: models.MergeStatusPending}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if strings.Contains(buf.String(), "/worktree/rebase") {
		t.Fatal("did not expect Rebase action when task branch is not behind target")
	}
}

func TestTaskChangesWorktreeContent_MergedStatusWithoutDiffHidesLocalSection(t *testing.T) {
	task := &models.Task{
		ID:             "task-1",
		WorktreeBranch: "task/feature",
		MergeStatus:    models.MergeStatusMerged,
		Status:         models.StatusCompleted,
	}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	// When merged and no diff remains, local merge options should not appear.
	if strings.Contains(out, "/worktree/merge") {
		t.Fatal("did not expect merge endpoint actions when already merged")
	}
	// GitHub section should still render
	if !strings.Contains(out, "GitHub") {
		t.Fatal("expected GitHub section header when already merged")
	}
}

func TestTaskChangesWorktreeContent_ConflictStatusHidesLocalSection(t *testing.T) {
	task := &models.Task{
		ID:             "task-1",
		WorktreeBranch: "task/feature",
		MergeStatus:    models.MergeStatusConflict,
		Status:         models.StatusCompleted,
	}

	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Fast-forward only") || strings.Contains(out, "Squash merge") || strings.Contains(out, "merge_source") {
		t.Fatalf("expected Local merge section hidden while conflict is active, body=%s", out)
	}
	if !strings.Contains(out, "GitHub") {
		t.Fatalf("expected GitHub section to remain available, body=%s", out)
	}
}

func TestTaskChangesWorktreeContent_FailedMergedStatusHidesLocalSection(t *testing.T) {
	task := &models.Task{
		ID:             "task-1",
		WorktreeBranch: "task/feature",
		MergeStatus:    models.MergeStatusMerged,
		Status:         models.StatusFailed,
	}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "/worktree/merge") {
		t.Fatal("did not expect merge endpoint actions when merge_status is merged, even for failed task status")
	}
}

func TestWorktreeInfoPanel_LocalSectionHeader(t *testing.T) {
	task := &models.Task{
		ID:                "task-1",
		WorktreeBranch:    "task/feature",
		MergeTargetBranch: "main",
		MergeStatus:       models.MergeStatusPending,
	}
	var buf bytes.Buffer
	if err := WorktreeInfoPanel(task, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Local") {
		t.Fatal("expected Local section header in worktree info panel merge dropdown")
	}
	if !strings.Contains(out, "Merge commit") {
		t.Fatal("expected Merge commit option in worktree info panel")
	}
	if !strings.Contains(out, `hx-target="#worktree-info-panel"`) {
		t.Fatal("expected worktree panel merge actions to refresh the worktree panel")
	}
	if !strings.Contains(out, `type="button"`) {
		t.Fatal("expected worktree panel merge actions to be explicit buttons so clicks cannot fall back to form submission")
	}
}

// TestTaskChangesWorktreeContent_BranchAlreadyMergedHidesLocalSection ensures
// that when the task branch has already been merged into its target, local
// merge actions are suppressed even if `merge_status` is still stale (`pending`)
// and a preserved diff is being shown for context.
func TestTaskChangesWorktreeContent_BranchAlreadyMergedHidesLocalSection(t *testing.T) {
	task := &models.Task{
		ID:             "task-1",
		WorktreeBranch: "task/feature",
		MergeStatus:    models.MergeStatusPending, // stale
		Status:         models.StatusCompleted,
	}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git a/file.txt b/file.txt", task, nil, nil, nil, true, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "/worktree/merge") {
		t.Fatal("did not expect merge endpoint actions when branch is already merged")
	}
	if !strings.Contains(out, "file.txt") {
		t.Fatal("expected preserved diff to remain visible when local merge actions are hidden")
	}
	// GitHub section should still render (Create PR / View PR remains valid).
	if !strings.Contains(out, "GitHub") {
		t.Fatal("expected GitHub section header even when branch is already merged")
	}
}

func TestTaskChangesWorktreeContent_MergedStatusWithDiffHidesLocalSection(t *testing.T) {
	task := &models.Task{
		ID:             "task-1",
		WorktreeBranch: "task/feature",
		MergeStatus:    models.MergeStatusMerged,
		Status:         models.StatusCompleted,
	}
	var buf bytes.Buffer
	if err := TaskChangesWorktreeContent("diff --git a/file.txt b/file.txt", task, nil, nil, nil, false, false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "/worktree/merge") {
		t.Fatal("did not expect merge endpoint actions after app fast-forward merge, even when preserved diff exists")
	}
	if strings.Contains(out, "No changes detected") {
		t.Fatal("expected preserved diff to remain visible after merge")
	}
	if !strings.Contains(out, "file.txt") {
		t.Fatal("expected preserved diff file to remain visible after merge")
	}
}
