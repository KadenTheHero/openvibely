package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

// TestHandler_GetTaskChanges_MergedTaskShowsPreservedDiff verifies that after a task is merged,
// the changes tab still shows the preserved diff from the execution, not an empty live diff.
func TestHandler_GetTaskChanges_MergedTaskShowsPreservedDiff(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	defer db.Close()

	ctx := context.Background()
	_ = e // Use the echo instance variable

	// Get default project from test setup
	projectRepo := repository.NewProjectRepo(db)
	projects, err := projectRepo.List(ctx)
	if err != nil || len(projects) == 0 {
		// Create a project if none exists
		project := &models.Project{
			Name:     "Test Project",
			RepoPath: "/tmp/test-repo",
		}
		if err := projectRepo.Create(ctx, project); err != nil {
			t.Fatalf("failed to create project: %v", err)
		}
		projects = []models.Project{*project}
	}
	project := &projects[0]

	// Create task using the handler's task repo
	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)

	// Create a task with worktree that has been merged
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Merged Task",
		Category:          models.CategoryCompleted,
		Status:            models.StatusCompleted,
		WorktreePath:      "/tmp/.worktrees/task_123",
		WorktreeBranch:    "task/123-merged-task",
		MergeTargetBranch: "main",
		MergeStatus:       models.MergeStatusMerged, // Key: task is merged
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Get a default agent config for the execution
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	agents, _ := llmConfigRepo.List(ctx)
	var agentID string
	if len(agents) > 0 {
		agentID = agents[0].ID
	}

	// Create an execution with preserved diff output
	preservedDiff := `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 line 1
-old content
+new content
 line 3`

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agentID,
		Status:        models.ExecCompleted,
		PromptSent:    "Test prompt",
	}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	// Set the diff output separately (this is how it's done in llm_service.go)
	if err := execRepo.UpdateDiffOutput(ctx, exec.ID, preservedDiff); err != nil {
		t.Fatalf("failed to update diff output: %v", err)
	}
	exec.DiffOutput = preservedDiff // Update local copy for consistency

	// Verify task was created with worktree info
	t.Logf("Created task ID: %s, WorktreeBranch: %s, MergeStatus: %s", task.ID, task.WorktreeBranch, task.MergeStatus)

	// Fetch task again to verify it was saved correctly
	fetchedTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to fetch task: %v", err)
	}
	t.Logf("Fetched task ID: %s, WorktreeBranch: %s, MergeStatus: %s", fetchedTask.ID, fetchedTask.WorktreeBranch, fetchedTask.MergeStatus)

	// Verify execution was created with diff
	fetchedExecs, err := execRepo.ListByTaskChronological(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to fetch executions: %v", err)
	}
	t.Logf("Fetched %d executions", len(fetchedExecs))
	for i, e := range fetchedExecs {
		t.Logf("  Execution %d: ID=%s, DiffOutput length=%d", i, e.ID, len(e.DiffOutput))
		if len(e.DiffOutput) > 100 {
			t.Logf("  DiffOutput (first 100 chars): %s", e.DiffOutput[:100])
		} else if e.DiffOutput != "" {
			t.Logf("  DiffOutput: %s", e.DiffOutput)
		}
	}

	// Create a request to get the changes tab
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/changes", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("taskId")
	c.SetParamValues(task.ID)

	// Execute the handler
	if err := h.GetTaskChanges(c); err != nil {
		t.Fatalf("GetTaskChanges failed: %v", err)
	}

	// Verify the response contains the preserved diff
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", rec.Code, body)
	}

	// The response should contain the preserved diff content
	if body == "" {
		t.Error("expected non-empty response body")
	}

	// Debug: print the first 500 chars of response
	if len(body) > 500 {
		t.Logf("Response body (first 500 chars): %s", body[:500])
	} else {
		t.Logf("Response body: %s", body)
	}

	// Verify the diff content is present (check for a unique string from the diff)
	if !containsString(body, "new content") {
		t.Errorf("expected preserved diff to contain 'new content', but it was missing")
	}

	// Verify it's showing the worktree changes view (not the regular execution view)
	// The worktree view should mention the branch name
	if !containsString(body, task.WorktreeBranch) {
		t.Errorf("expected response to show worktree branch %s", task.WorktreeBranch)
	}

	if containsString(body, "Merge to") {
		t.Error("did not expect merge options when task changes merge flag is disabled by default")
	}
}

// TestHandler_GetTaskChanges_UnmergedTaskShowsLiveDiff verifies that for unmerged tasks,
// we still attempt to show live diff if the worktree exists.
func TestHandler_GetTaskChanges_UnmergedTaskShowsLiveDiff(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	defer db.Close()

	ctx := context.Background()
	_ = e // Use the echo instance variable

	// Get default project from test setup
	projectRepo := repository.NewProjectRepo(db)
	projects, err := projectRepo.List(ctx)
	if err != nil || len(projects) == 0 {
		// Create a project if none exists
		project := &models.Project{
			Name:     "Test Project",
			RepoPath: "/tmp/test-repo",
		}
		if err := projectRepo.Create(ctx, project); err != nil {
			t.Fatalf("failed to create project: %v", err)
		}
		projects = []models.Project{*project}
	}
	project := &projects[0]

	// Create task using repos
	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)

	// Create a task with worktree that is NOT merged (status = pending)
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Unmerged Task",
		Category:          models.CategoryActive,
		Status:            models.StatusCompleted,
		WorktreePath:      "/tmp/.worktrees/task_456",
		WorktreeBranch:    "task/456-unmerged-task",
		MergeTargetBranch: "main",
		MergeStatus:       models.MergeStatusPending, // Key: task is NOT merged yet
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Get a default agent config for the execution
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	agents, _ := llmConfigRepo.List(ctx)
	var agentID string
	if len(agents) > 0 {
		agentID = agents[0].ID
	}

	// Create an execution with preserved diff
	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agentID,
		Status:        models.ExecCompleted,
		PromptSent:    "Test prompt",
	}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	// Set the diff output separately
	preservedDiff := "diff --git a/file.txt b/file.txt\n+some changes"
	if err := execRepo.UpdateDiffOutput(ctx, exec.ID, preservedDiff); err != nil {
		t.Fatalf("failed to update diff output: %v", err)
	}
	exec.DiffOutput = preservedDiff

	// Create a request to get the changes tab
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/changes", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("taskId")
	c.SetParamValues(task.ID)

	// Execute the handler
	if err := h.GetTaskChanges(c); err != nil {
		t.Fatalf("GetTaskChanges failed: %v", err)
	}

	// For unmerged tasks, if the worktree doesn't exist (which is likely in this test),
	// it should fall back to showing the preserved diff
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if body == "" {
		t.Error("expected non-empty response body")
	}

	// Since the worktree path doesn't exist in the test environment,
	// it should show the preserved diff as a fallback
	if !containsString(body, "some changes") {
		t.Error("expected fallback to preserved diff when worktree doesn't exist")
	}
}

// TestHandler_GetTaskChanges_PendingMergeStatusButMergedBranchShowsPreservedDiff verifies
// stale merge_status records do not hide changes after a successful manual merge.
func TestHandler_GetTaskChanges_PendingMergeStatusButMergedBranchShowsPreservedDiff(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	defer db.Close()

	ctx := context.Background()

	repoDir := createHandlerTestGitRepo(t)
	mainBranch := gitCurrentBranch(t, repoDir)
	taskBranch := "task/stale-merge-status"

	runGit(t, repoDir, "checkout", "-b", taskBranch)
	testFile := filepath.Join(repoDir, "merged_file.txt")
	if err := os.WriteFile(testFile, []byte("new content from merged branch\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGit(t, repoDir, "add", "merged_file.txt")
	runGit(t, repoDir, "commit", "-m", "task branch change")
	runGit(t, repoDir, "checkout", mainBranch)
	runGit(t, repoDir, "merge", "--no-ff", "-m", "merge task branch", taskBranch)

	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{
		Name:     "Merged Branch Project",
		RepoPath: repoDir,
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	agents, _ := llmConfigRepo.List(ctx)
	var agentID string
	if len(agents) > 0 {
		agentID = agents[0].ID
	}

	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Stale merge status task",
		Category:          models.CategoryCompleted,
		Status:            models.StatusCompleted,
		WorktreePath:      t.TempDir(),
		WorktreeBranch:    taskBranch,
		MergeTargetBranch: mainBranch,
		MergeStatus:       models.MergeStatusPending, // stale status despite merged branch
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agentID,
		Status:        models.ExecCompleted,
		PromptSent:    "stale merge status regression",
	}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	preservedDiff := "diff --git a/merged_file.txt b/merged_file.txt\n+new content from merged branch"
	if err := execRepo.UpdateDiffOutput(ctx, exec.ID, preservedDiff); err != nil {
		t.Fatalf("update diff output: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/changes", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("taskId")
	c.SetParamValues(task.ID)

	if err := h.GetTaskChanges(c); err != nil {
		t.Fatalf("GetTaskChanges failed: %v", err)
	}

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, body)
	}
	if !containsString(body, "new content from merged branch") {
		t.Fatalf("expected preserved diff content in response body, got: %s", body)
	}
}

// TestHandler_GetTaskChanges_FastForwardMergedBranchHidesMergeOptions
// reproduces the stale-state bug where a task branch that is already reachable
// from the target branch keeps surfacing local merge options on the Changes tab.
// After the fix, the changes-tab response must NOT include local /worktree/merge
// endpoint actions, and stale merge_status is back-filled to "merged" when found.
func TestHandler_GetTaskChanges_FastForwardMergedBranchHidesMergeOptions(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	defer db.Close()
	h.SetTaskChangesMergeOptionsEnabled(true) // ensure merge options would render if not suppressed

	ctx := context.Background()

	repoDir := createHandlerTestGitRepo(t)
	mainBranch := gitCurrentBranch(t, repoDir)
	taskBranch := "task/0f7ee252-smart-scrolling"

	// Create a task branch with a commit, then fast-forward main onto it.
	runGit(t, repoDir, "checkout", "-b", taskBranch)
	testFile := filepath.Join(repoDir, "scrolling.txt")
	if err := os.WriteFile(testFile, []byte("smart scrolling change\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGit(t, repoDir, "add", "scrolling.txt")
	runGit(t, repoDir, "commit", "-m", "implement smart scrolling")
	runGit(t, repoDir, "checkout", mainBranch)
	runGit(t, repoDir, "merge", "--ff-only", taskBranch)

	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "FF-Merged Project", RepoPath: repoDir}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	agents, _ := llmConfigRepo.List(ctx)
	var agentID string
	if len(agents) > 0 {
		agentID = agents[0].ID
	}

	// Worktree path doesn't need to exist for this test path; the handler
	// reconciles via IsBranchMerged on the project repo.
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Implement smart scrolling for active task thread streaming",
		Category:          models.CategoryCompleted,
		Status:            models.StatusCompleted,
		WorktreePath:      t.TempDir(),
		WorktreeBranch:    taskBranch,
		MergeTargetBranch: mainBranch,
		MergeStatus:       models.MergeStatusPending, // stale stored state despite branch ancestry
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agentID,
		Status:        models.ExecCompleted,
		PromptSent:    "smart scrolling regression",
	}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	preservedDiff := "diff --git a/scrolling.txt b/scrolling.txt\n+smart scrolling change"
	if err := execRepo.UpdateDiffOutput(ctx, exec.ID, preservedDiff); err != nil {
		t.Fatalf("update diff output: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/changes", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("taskId")
	c.SetParamValues(task.ID)

	if err := h.GetTaskChanges(c); err != nil {
		t.Fatalf("GetTaskChanges failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Local merge actions must be hidden because the branch is already merged.
	if strings.Contains(body, "/worktree/merge") {
		t.Fatalf("expected local /worktree/merge actions to be hidden after fast-forward merge, body=%s", body)
	}
	// Preserved diff content should still render so the user can review what was merged.
	if !strings.Contains(body, "smart scrolling change") {
		t.Fatalf("expected preserved diff content in changes-tab response, body=%s", body)
	}

	// Stale merge_status should have been back-filled.
	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("re-fetch task: %v", err)
	}
	if updated.MergeStatus != models.MergeStatusMerged {
		t.Fatalf("expected merge_status to be back-filled to merged, got %s", updated.MergeStatus)
	}
}

// TestHandler_MergeTaskBranch_RejectsAlreadyMergedBranch ensures that an
// in-flight merge POST (e.g. from a stale changes-tab UI) is rejected with 409
// when the branch is already merged into its target.
func TestHandler_MergeTaskBranch_RejectsAlreadyMergedBranch(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	defer db.Close()
	h.SetTaskChangesMergeOptionsEnabled(true)

	ctx := context.Background()
	repoDir := createHandlerTestGitRepo(t)
	mainBranch := gitCurrentBranch(t, repoDir)
	taskBranch := "task/already-merged"

	runGit(t, repoDir, "checkout", "-b", taskBranch)
	if err := os.WriteFile(filepath.Join(repoDir, "x.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatalf("write x: %v", err)
	}
	runGit(t, repoDir, "add", "x.txt")
	runGit(t, repoDir, "commit", "-m", "x change")
	runGit(t, repoDir, "checkout", mainBranch)
	runGit(t, repoDir, "merge", "--ff-only", taskBranch)

	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "Already Merged Project", RepoPath: repoDir}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskRepo := repository.NewTaskRepo(db, nil)
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Already merged",
		Category:          models.CategoryCompleted,
		Status:            models.StatusCompleted,
		WorktreePath:      t.TempDir(),
		WorktreeBranch:    taskBranch,
		MergeTargetBranch: mainBranch,
		MergeStatus:       models.MergeStatusPending,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	form := "merge_type=merge&merge_source=changes_tab"
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+task.ID+"/worktree/merge", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("taskId")
	c.SetParamValues(task.ID)

	if err := h.MergeTaskBranch(c); err != nil {
		t.Fatalf("MergeTaskBranch returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for already-merged branch, got %d body=%s", rec.Code, rec.Body.String())
	}

	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("re-fetch task: %v", err)
	}
	if updated.MergeStatus != models.MergeStatusMerged {
		t.Fatalf("expected merge_status back-filled to merged after rejection, got %s", updated.MergeStatus)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	branch := runGit(t, dir, "branch", "--show-current")
	if branch == "" {
		t.Fatal("expected non-empty current branch")
	}
	return branch
}

// Helper to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) &&
		(s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
