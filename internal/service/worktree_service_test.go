package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

// createTestGitRepo creates a temporary git repository with an initial commit.
func createTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	// Create initial file and commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit failed: %v\n%s", err, out)
	}

	return dir
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitRevParseTest(t *testing.T, dir string, rev string) string {
	t.Helper()
	return runGitTest(t, dir, "rev-parse", rev)
}

func TestIsGitRepo(t *testing.T) {
	// Non-git directory
	tmpDir := t.TempDir()
	if IsGitRepo(tmpDir) {
		t.Error("expected non-git dir to return false")
	}

	// Git directory
	gitDir := createTestGitRepo(t)
	if !IsGitRepo(gitDir) {
		t.Error("expected git dir to return true")
	}

	// Empty string
	if IsGitRepo("") {
		t.Error("expected empty string to return false")
	}
}

func TestGetDefaultBranch(t *testing.T) {
	repoDir := createTestGitRepo(t)
	branch := GetDefaultBranch(repoDir)
	// Should be either "main" or "master" depending on git config
	if branch != "main" && branch != "master" {
		t.Errorf("expected main or master, got %q", branch)
	}
}

func TestGetCurrentBranch(t *testing.T) {
	repoDir := createTestGitRepo(t)
	branch := GetCurrentBranch(repoDir)
	if branch == "" {
		t.Error("expected non-empty branch name")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello-world"},
		{"Fix Bug #123", "fix-bug-123"},
		{"Multiple   Spaces", "multiple-spaces"},
		{"Special!@#Characters", "special-characters"},
		{"", ""},
		{"A very long title that should be truncated to fifty characters maximum length", "a-very-long-title-that-should-be-truncated-to-fift"},
	}

	for _, tc := range tests {
		got := slugify(tc.input)
		if got != tc.expected {
			t.Errorf("slugify(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestSetupWorktree(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID: "default",
		Title:     "Test Worktree Task",
		Category:  models.CategoryActive,
		Priority:  1,
		Prompt:    "Do something",
		Status:    models.StatusPending,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	// Setup worktree
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if wtPath == "" {
		t.Error("expected non-empty worktree path")
	}
	if branchName == "" {
		t.Error("expected non-empty branch name")
	}
	if !strings.HasPrefix(branchName, "task/") {
		t.Errorf("expected branch to start with 'task/', got %q", branchName)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory should exist")
	}

	// Verify task was updated in DB
	dbTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbTask.WorktreePath == "" {
		t.Error("expected worktree_path to be set in DB")
	}
	if dbTask.WorktreeBranch == "" {
		t.Error("expected worktree_branch to be set in DB")
	}
}

func TestSetupWorktree_NotGitRepo(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID: "default",
		Title:     "Test Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	_, _, err := ws.SetupWorktree(ctx, task, tmpDir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestSyncWorktreeFromMainAtStart_CleanWorktreeAutoMergeSuccess(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Startup Sync Success",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, wtBranch, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch

	// Create new commit on main/default branch after worktree was created.
	newMainFile := filepath.Join(repoDir, "main_update.txt")
	if err := os.WriteFile(newMainFile, []byte("from main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "main_update.txt")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add on main: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "main update")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit on main: %v\n%s", err, out)
	}

	if err := ws.SyncWorktreeFromMainAtStart(ctx, task, repoDir); err != nil {
		t.Fatalf("SyncWorktreeFromMainAtStart: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "main_update.txt")); err != nil {
		t.Fatalf("expected main_update.txt in worktree after startup sync: %v", err)
	}

	checkStatus := exec.Command("git", "status", "--porcelain")
	checkStatus.Dir = wtPath
	statusOut, err := checkStatus.Output()
	if err != nil {
		t.Fatalf("git status in worktree: %v", err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		t.Fatalf("expected clean worktree after startup sync, got status: %s", string(statusOut))
	}
}

func TestSyncWorktreeFromMainAtStart_DirtyWorktreeSkipsAutoMerge(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Startup Sync Dirty Skip",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, wtBranch, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch

	// Make worktree dirty (untracked file).
	if err := os.WriteFile(filepath.Join(wtPath, "local_task_change.txt"), []byte("wip\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create new commit on main/default branch.
	if err := os.WriteFile(filepath.Join(repoDir, "main_dirty_skip_update.txt"), []byte("from main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "main_dirty_skip_update.txt")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add on main: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "main update for dirty skip")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit on main: %v\n%s", err, out)
	}

	if err := ws.SyncWorktreeFromMainAtStart(ctx, task, repoDir); err != nil {
		t.Fatalf("expected dirty worktree skip without error, got: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "main_dirty_skip_update.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected main update to not be merged into dirty worktree")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "local_task_change.txt")); err != nil {
		t.Fatalf("expected local dirty file to remain: %v", err)
	}
}

func TestSyncWorktreeFromMainAtStart_ConflictFailsGracefully(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Startup Sync Conflict",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, wtBranch, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch

	// Commit change in worktree branch.
	if err := os.WriteFile(filepath.Join(wtPath, "README.md"), []byte("task-branch-change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "task branch update readme"); err != nil {
		t.Fatalf("CommitWorktreeChanges in worktree: %v", err)
	}

	// Commit conflicting change on main/default branch.
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("main-branch-change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "README.md")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add on main: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "main branch update readme")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit on main: %v\n%s", err, out)
	}

	err = ws.SyncWorktreeFromMainAtStart(ctx, task, repoDir)
	if err == nil {
		t.Fatal("expected startup auto-merge conflict error")
	}
	if !strings.Contains(err.Error(), "startup auto-merge conflict") {
		t.Fatalf("expected actionable startup auto-merge conflict error, got: %v", err)
	}

	dbTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if dbTask.MergeStatus != models.MergeStatusConflict {
		t.Fatalf("expected merge_status=conflict, got %q", dbTask.MergeStatus)
	}

	// Confirm repository is recoverable: no in-progress merge remains.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = wtPath
	statusOut, err := statusCmd.Output()
	if err != nil {
		t.Fatalf("git status in worktree: %v", err)
	}
	if strings.Contains(string(statusOut), "UU ") {
		t.Fatalf("expected no unresolved conflict entries after abort, got status: %s", string(statusOut))
	}
	headCmd := exec.Command("git", "rev-parse", "-q", "--verify", "MERGE_HEAD")
	headCmd.Dir = wtPath
	if out, err := headCmd.CombinedOutput(); err == nil {
		t.Fatalf("expected MERGE_HEAD to be absent after aborted startup merge, got: %s", strings.TrimSpace(string(out)))
	}
}

func TestSyncWorktreeFromMainAtStart_FetchFailureFallsBackToLocalBranch(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	// Add a broken origin so fetch fails immediately.
	cmd := exec.Command("git", "remote", "add", "origin", "/tmp/nonexistent-openvibely-origin")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin failed: %v\n%s", err, out)
	}

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)
	task := &models.Task{
		ProjectID:         "default",
		Title:             "Startup Sync Fetch Fallback",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, wtBranch, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch

	// Add commit on main/default branch after worktree creation.
	if err := os.WriteFile(filepath.Join(repoDir, "fallback_sync_file.txt"), []byte("main update\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "fallback_sync_file.txt")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add on main failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "main update for fetch fallback")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit on main failed: %v\n%s", err, out)
	}

	if err := ws.SyncWorktreeFromMainAtStart(ctx, task, repoDir); err != nil {
		t.Fatalf("expected fallback local merge without error, got: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "fallback_sync_file.txt")); err != nil {
		t.Fatalf("expected local main branch update to be merged into worktree: %v", err)
	}
}

func TestCommitWorktreeChanges(t *testing.T) {
	repoDir := createTestGitRepo(t)

	// Empty commit message should fail
	if err := CommitWorktreeChanges(repoDir, "   "); err == nil {
		t.Fatal("expected error for empty commit message")
	}

	// No changes to commit
	if err := CommitWorktreeChanges(repoDir, "no changes"); err != nil {
		t.Errorf("expected nil for no changes, got: %v", err)
	}

	// Make a change and commit
	if err := os.WriteFile(filepath.Join(repoDir, "new_file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(repoDir, "test commit"); err != nil {
		t.Errorf("CommitWorktreeChanges: %v", err)
	}

	// Verify the commit was made
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "test commit") {
		t.Errorf("expected commit message in log, got: %s", string(out))
	}
}

func TestCommitWorktreeChanges_AutoConfigSetup(t *testing.T) {
	// Create repo WITHOUT git config (simulating VPS scenario)
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Create initial file and commit using explicit config
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit failed: %v\n%s", err, out)
	}

	// Verify no local user.email is configured in repo
	cmd = exec.Command("git", "config", "--local", "user.email")
	cmd.Dir = dir
	out, _ := cmd.Output()
	hasLocalConfig := len(strings.TrimSpace(string(out))) > 0

	// Make a change and commit - should succeed even without local config
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(dir, "auto config test"); err != nil {
		t.Fatalf("CommitWorktreeChanges failed (should auto-set config if needed): %v", err)
	}

	// If there was no local config before, verify bot config was auto-set
	if !hasLocalConfig {
		cmd = exec.Command("git", "config", "--local", "user.email")
		cmd.Dir = dir
		out, _ := cmd.Output()
		email := strings.TrimSpace(string(out))
		// If local config was set, it should be the bot email
		// If it's empty, that's ok too (means global config was used)
		if email != "" && email != "bot@openvibely.ai" {
			t.Errorf("expected bot@openvibely.ai or empty (using global) in local config, got: %s", email)
		}

		// Verify the effective config (local or global) allowed the commit
		cmd = exec.Command("git", "config", "user.email")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		effectiveEmail := strings.TrimSpace(string(out))
		if effectiveEmail == "" {
			t.Error("expected some user.email configured (local or global)")
		}
	}

	// Verify commit was made
	cmd = exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "auto config test") {
		t.Errorf("expected commit in log, got: %s", string(out))
	}
}

func TestCommitWorktreeChanges_AutoConfigSetupMissingName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit failed: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "config", "--local", "user.email", "partial@example.com")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set local email failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "config", "--local", "--unset", "user.name")
	cmd.Dir = dir
	_ = cmd.Run()

	if err := os.WriteFile(filepath.Join(dir, "partial-config.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(dir, "partial config commit"); err != nil {
		t.Fatalf("CommitWorktreeChanges should set missing user.name independently: %v", err)
	}

	cmd = exec.Command("git", "config", "--local", "user.name")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if name := strings.TrimSpace(string(out)); name != "OpenVibely Bot" {
		t.Fatalf("expected missing user.name to be filled, got %q", name)
	}
}

func TestWorktreeDiff(t *testing.T) {
	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	// Create a new branch and make changes
	cmd := exec.Command("git", "checkout", "-b", "test-branch")
	cmd.Dir = repoDir
	cmd.Run()

	if err := os.WriteFile(filepath.Join(repoDir, "new_file.txt"), []byte("hello from branch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "branch commit")
	cmd.Dir = repoDir
	cmd.Run()

	// Switch back to default branch
	cmd = exec.Command("git", "checkout", defaultBranch)
	cmd.Dir = repoDir
	cmd.Run()

	// Get diff
	diff := GetWorktreeDiff(repoDir, "test-branch", defaultBranch)
	if diff == "" {
		t.Error("expected non-empty diff")
	}
	if !strings.Contains(diff, "new_file.txt") {
		t.Error("expected diff to contain new_file.txt")
	}
}

func TestWorktreeFileStats(t *testing.T) {
	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	// Create a new branch with changes
	cmd := exec.Command("git", "checkout", "-b", "stats-branch")
	cmd.Dir = repoDir
	cmd.Run()

	os.WriteFile(filepath.Join(repoDir, "added.txt"), []byte("new file\n"), 0644)
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified\n"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "changes")
	cmd.Dir = repoDir
	cmd.Run()

	// Go back
	cmd = exec.Command("git", "checkout", defaultBranch)
	cmd.Dir = repoDir
	cmd.Run()

	stats := GetWorktreeFileStats(repoDir, "stats-branch", defaultBranch)
	if len(stats) == 0 {
		t.Error("expected file stats")
	}

	// Check for added file
	foundAdded := false
	foundModified := false
	for _, s := range stats {
		if s.Path == "added.txt" && s.Status == "added" {
			foundAdded = true
		}
		if s.Path == "README.md" && s.Status == "modified" {
			foundModified = true
		}
	}
	if !foundAdded {
		t.Error("expected added.txt in stats")
	}
	if !foundModified {
		t.Error("expected README.md modified in stats")
	}
}

func TestGetWorktreeFileStatsWithUncommittedIncludesLiveWorktreeChanges(t *testing.T) {
	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	worktreePath := filepath.Join(t.TempDir(), "live-worktree")

	cmd := exec.Command("git", "branch", "live-stats", defaultBranch)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create branch: %v output=%s", err, string(out))
	}
	cmd = exec.Command("git", "worktree", "add", worktreePath, "live-stats")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add worktree: %v output=%s", err, string(out))
	}

	if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("# Live\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "untracked.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := GetWorktreeFileStatsWithUncommitted(repoDir, "live-stats", defaultBranch, worktreePath)
	foundModified := false
	foundAdded := false
	for _, stat := range stats {
		if stat.Path == "README.md" && stat.Status == "modified" {
			foundModified = true
		}
		if stat.Path == "untracked.txt" && stat.Status == "added" {
			foundAdded = true
		}
	}
	if !foundModified {
		t.Fatalf("expected live README.md modification in stats, got %+v", stats)
	}
	if !foundAdded {
		t.Fatalf("expected live untracked.txt addition in stats, got %+v", stats)
	}
}

func TestWorktreeRepo_UpdateAndClear(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	task := &models.Task{
		ProjectID: "default",
		Title:     "Worktree Update Test",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	// Update worktree info
	if err := repo.UpdateWorktreeInfo(ctx, task.ID, "/path/to/worktree", "task/test-branch"); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.GetByID(ctx, task.ID)
	if got.WorktreePath != "/path/to/worktree" {
		t.Errorf("expected worktree_path=/path/to/worktree, got %q", got.WorktreePath)
	}
	if got.WorktreeBranch != "task/test-branch" {
		t.Errorf("expected worktree_branch=task/test-branch, got %q", got.WorktreeBranch)
	}

	// Update merge status
	if err := repo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusPending); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetByID(ctx, task.ID)
	if got.MergeStatus != models.MergeStatusPending {
		t.Errorf("expected merge_status=pending, got %q", got.MergeStatus)
	}

	// Update auto-merge
	if err := repo.UpdateAutoMerge(ctx, task.ID, true, "main"); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetByID(ctx, task.ID)
	if !got.AutoMerge {
		t.Error("expected auto_merge=true")
	}
	if got.MergeTargetBranch != "main" {
		t.Errorf("expected merge_target_branch=main, got %q", got.MergeTargetBranch)
	}

	// Clear worktree info
	if err := repo.ClearWorktreeInfo(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetByID(ctx, task.ID)
	if got.WorktreePath != "" {
		t.Errorf("expected empty worktree_path after clear, got %q", got.WorktreePath)
	}
	if got.WorktreeBranch != "" {
		t.Errorf("expected empty worktree_branch after clear, got %q", got.WorktreeBranch)
	}
}

func TestMergeBranch(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Merge Test Task",
		Category:          models.CategoryActive,
		Priority:          1,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	// Setup worktree
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	// Make changes in worktree
	os.WriteFile(filepath.Join(wtPath, "feature.txt"), []byte("new feature\n"), 0644)
	CommitWorktreeChanges(wtPath, "add feature")

	// Merge
	result, err := ws.MergeBranch(ctx, task, repoDir, "merge")
	if err != nil {
		t.Fatalf("MergeBranch: %v", err)
	}
	if !result.Success {
		t.Errorf("expected merge success, got error: %s", result.ErrorMessage)
	}
	if result.MergeCommit == "" {
		t.Error("expected merge commit hash")
	}

	// Verify merge status in DB
	dbTask, _ := taskRepo.GetByID(ctx, task.ID)
	if dbTask.MergeStatus != models.MergeStatusMerged {
		t.Errorf("expected merge_status=merged, got %q", dbTask.MergeStatus)
	}

	// Verify the file exists on the target branch
	if _, err := os.Stat(filepath.Join(repoDir, "feature.txt")); os.IsNotExist(err) {
		t.Error("expected feature.txt to exist on target branch after merge")
	}
}

func TestMergeBranch_ReturnsErrorWhenAutoCommitFails(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Auto Commit Failure",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
		WorktreePath:      t.TempDir(),
		WorktreeBranch:    "task/auto-commit-fail",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(task.WorktreePath, "dirty.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ws.MergeBranch(ctx, task, repoDir, "merge")
	if err == nil {
		t.Fatal("expected merge error when auto-commit fails")
	}
	if result == nil || result.ErrorMessage == "" {
		t.Fatalf("expected merge result with error message, got %#v", result)
	}
	if !strings.Contains(result.ErrorMessage, "checking git status") && !strings.Contains(result.ErrorMessage, "staging changes") && !strings.Contains(result.ErrorMessage, "committing changes") {
		t.Fatalf("expected commit failure details, got %q", result.ErrorMessage)
	}

	dbTask, dbErr := taskRepo.GetByID(ctx, task.ID)
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	if dbTask.MergeStatus != models.MergeStatusFailed {
		t.Fatalf("expected merge status failed, got %q", dbTask.MergeStatus)
	}
}

func TestMergeBranch_UnrelatedDirtyTargetFileCanMerge(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Dirty Target",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	if err := os.WriteFile(filepath.Join(wtPath, "task_change.txt"), []byte("task change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "task change"); err != nil {
		t.Fatal(err)
	}

	dirtyPath := filepath.Join(repoDir, "local_dirty.txt")
	if err := os.WriteFile(dirtyPath, []byte("do not touch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ws.MergeBranch(ctx, task, repoDir, "merge")
	if err != nil {
		t.Fatalf("expected non-overlapping dirty target file to be allowed, got %v result=%#v", err, result)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful merge, got %#v", result)
	}
	if got, readErr := os.ReadFile(dirtyPath); readErr != nil || string(got) != "do not touch\n" {
		t.Fatalf("expected dirty target file to remain untouched, got %q err=%v", got, readErr)
	}

	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MergeStatus != models.MergeStatusMerged {
		t.Fatalf("expected merge_status=merged, got %q", updated.MergeStatus)
	}
}

func TestMergeBranch_OverlappingDirtyTargetFileFails(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Overlapping Dirty Target",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	sharedPath := filepath.Join(wtPath, "README.md")
	if err := os.WriteFile(sharedPath, []byte("# Task branch change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "task changes README"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Local dirty change\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ws.MergeBranch(ctx, task, repoDir, "merge")
	if err == nil {
		t.Fatal("expected Git to reject merge that would overwrite local dirty changes")
	}
	if result == nil || !strings.Contains(strings.ToLower(result.ErrorMessage), "would be overwritten") {
		t.Fatalf("expected overwrite warning from git, got result=%#v err=%v", result, err)
	}
	if got, readErr := os.ReadFile(filepath.Join(repoDir, "README.md")); readErr != nil || string(got) != "# Local dirty change\n" {
		t.Fatalf("expected local dirty file to remain untouched, got %q err=%v", got, readErr)
	}

	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MergeStatus != models.MergeStatusFailed {
		t.Fatalf("expected merge_status=failed, got %q", updated.MergeStatus)
	}
}

func TestMergeBranch_FastForward(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "FF Merge Test",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	os.WriteFile(filepath.Join(wtPath, "ff_feature.txt"), []byte("fast forward\n"), 0644)
	CommitWorktreeChanges(wtPath, "add ff feature")

	result, err := ws.MergeBranch(ctx, task, repoDir, "ff")
	if err != nil {
		t.Fatalf("MergeBranch ff: %v", err)
	}
	if !result.Success {
		t.Errorf("expected ff merge success: %s", result.ErrorMessage)
	}
	if !IsBranchTipMergedInto(repoDir, branchName, defaultBranch) {
		t.Fatalf("expected task branch tip to be merged into %s after app fast-forward merge", defaultBranch)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "ff_feature.txt")); err != nil {
		t.Fatalf("expected checked-out target worktree files to be refreshed after ff merge: %v", err)
	}
	if staged := runGitTest(t, repoDir, "diff", "--name-only", "--cached"); staged != "" {
		t.Fatalf("expected no staged changes left in checked-out target worktree after ff merge, got %q", staged)
	}
	dbTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task after ff merge: %v", err)
	}
	if dbTask.MergeStatus != models.MergeStatusMerged {
		t.Fatalf("expected merge status merged after app fast-forward merge, got %q", dbTask.MergeStatus)
	}
}

func TestMergeBranch_FastForward_SequentialMergesAutoRebaseSecondTask(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	taskA := &models.Task{
		ProjectID:         "default",
		Title:             "Sequential Merge Task A",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, taskA); err != nil {
		t.Fatal(err)
	}
	wtPathA, branchA, err := ws.SetupWorktree(ctx, taskA, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	taskA.WorktreePath = wtPathA
	taskA.WorktreeBranch = branchA
	if err := os.WriteFile(filepath.Join(wtPathA, "task_a.txt"), []byte("task a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPathA, "task a commit"); err != nil {
		t.Fatal(err)
	}

	taskB := &models.Task{
		ProjectID:         "default",
		Title:             "Sequential Merge Task B",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, taskB); err != nil {
		t.Fatal(err)
	}
	wtPathB, branchB, err := ws.SetupWorktree(ctx, taskB, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	taskB.WorktreePath = wtPathB
	taskB.WorktreeBranch = branchB
	if err := os.WriteFile(filepath.Join(wtPathB, "task_b.txt"), []byte("task b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPathB, "task b commit"); err != nil {
		t.Fatal(err)
	}

	resultA, err := ws.MergeBranch(ctx, taskA, repoDir, "ff")
	if err != nil {
		t.Fatalf("MergeBranch task A ff: %v", err)
	}
	if !resultA.Success {
		t.Fatalf("expected task A ff merge success: %s", resultA.ErrorMessage)
	}

	resultB, err := ws.MergeBranch(ctx, taskB, repoDir, "ff")
	if err != nil {
		t.Fatalf("MergeBranch task B ff: %v", err)
	}
	if !resultB.Success {
		t.Fatalf("expected task B ff merge success after auto-rebase, got: %s", resultB.ErrorMessage)
	}

	if _, err := os.Stat(filepath.Join(repoDir, "task_a.txt")); os.IsNotExist(err) {
		t.Fatal("expected task_a.txt to exist after sequential merge")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "task_b.txt")); os.IsNotExist(err) {
		t.Fatal("expected task_b.txt to exist after sequential merge")
	}
	mainHead := gitRevParseTest(t, repoDir, "refs/heads/"+defaultBranch)
	taskHead := gitRevParseTest(t, wtPathB, "HEAD")
	if mainHead != taskHead {
		t.Fatalf("expected target branch to advance to rebased task worktree HEAD, got target=%s task=%s", mainHead, taskHead)
	}
}

func TestMergeBranch_FastForward_SequentialMergesRebaseConflictPreserved(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	sharedFile := filepath.Join(repoDir, "shared.txt")
	if err := os.WriteFile(sharedFile, []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "shared.txt")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add shared file: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "add shared file")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit shared file: %v\n%s", err, out)
	}

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	taskA := &models.Task{
		ProjectID:         "default",
		Title:             "Sequential Conflict Task A",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, taskA); err != nil {
		t.Fatal(err)
	}
	wtPathA, branchA, err := ws.SetupWorktree(ctx, taskA, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	taskA.WorktreePath = wtPathA
	taskA.WorktreeBranch = branchA
	if err := os.WriteFile(filepath.Join(wtPathA, "shared.txt"), []byte("task-a-change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPathA, "task a shared update"); err != nil {
		t.Fatal(err)
	}

	taskB := &models.Task{
		ProjectID:         "default",
		Title:             "Sequential Conflict Task B",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, taskB); err != nil {
		t.Fatal(err)
	}
	wtPathB, branchB, err := ws.SetupWorktree(ctx, taskB, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	taskB.WorktreePath = wtPathB
	taskB.WorktreeBranch = branchB
	if err := os.WriteFile(filepath.Join(wtPathB, "shared.txt"), []byte("task-b-change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPathB, "task b shared update"); err != nil {
		t.Fatal(err)
	}

	resultA, err := ws.MergeBranch(ctx, taskA, repoDir, "ff")
	if err != nil {
		t.Fatalf("MergeBranch task A ff: %v", err)
	}
	if !resultA.Success {
		t.Fatalf("expected task A ff merge success: %s", resultA.ErrorMessage)
	}

	resultB, err := ws.MergeBranch(ctx, taskB, repoDir, "ff")
	if err != nil {
		t.Fatalf("expected conflict result without hard error, got: %v", err)
	}
	if resultB.Success {
		t.Fatalf("expected task B ff merge conflict after auto-rebase attempt")
	}
	if len(resultB.ConflictFiles) == 0 {
		t.Fatalf("expected conflict files for task B rebase conflict")
	}
	if !strings.Contains(strings.ToLower(resultB.ErrorMessage), "auto-rebase") {
		t.Fatalf("expected conflict message to mention auto-rebase, got %q", resultB.ErrorMessage)
	}

	updatedTaskB, err := taskRepo.GetByID(ctx, taskB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTaskB.MergeStatus != models.MergeStatusConflict {
		t.Fatalf("expected merge status conflict for task B, got %q", updatedTaskB.MergeStatus)
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = wtPathB
	statusOut, err := statusCmd.Output()
	if err != nil {
		t.Fatalf("git status in task B worktree: %v", err)
	}
	if strings.Contains(string(statusOut), "UU ") {
		t.Fatalf("expected task B worktree conflicts to be aborted after failed auto-rebase, got: %s", string(statusOut))
	}
}

func TestMergeBranch_FastForward_DirtyWorktreeRejected(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{ProjectID: "default", Title: "Dirty FF", Category: models.CategoryActive, Status: models.StatusPending, MergeTargetBranch: defaultBranch}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ws.MergeBranch(ctx, task, repoDir, "ff")
	if err == nil {
		t.Fatal("expected dirty task worktree to reject fast-forward merge")
	}
	if result == nil || !strings.Contains(result.ErrorMessage, "uncommitted changes") {
		t.Fatalf("expected dirty worktree message, got result=%#v err=%v", result, err)
	}
	if gitRevParseTest(t, repoDir, "refs/heads/"+defaultBranch) != gitRevParseTest(t, repoDir, "HEAD") {
		t.Fatal("expected target branch to remain unchanged after dirty worktree rejection")
	}
}

func TestMergeBranch_FastForward_WrongTaskWorktreeBranchRejected(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{ProjectID: "default", Title: "Wrong Branch FF", Category: models.CategoryActive, Status: models.StatusPending, MergeTargetBranch: defaultBranch}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName
	runGitTest(t, wtPath, "checkout", "-b", "task/wrong-branch")

	result, err := ws.MergeBranch(ctx, task, repoDir, "ff")
	if err == nil {
		t.Fatal("expected wrong task worktree branch to reject fast-forward merge")
	}
	if result == nil || !strings.Contains(result.ErrorMessage, "expected") {
		t.Fatalf("expected wrong branch message, got result=%#v err=%v", result, err)
	}
}

func TestMergeBranch_FastForward_CheckedOutTargetVerifiesMergedHead(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{ProjectID: "default", Title: "Wrong Merge Head FF", Category: models.CategoryActive, Status: models.StatusPending, MergeTargetBranch: defaultBranch}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName
	if err := os.WriteFile(filepath.Join(wtPath, "expected_task.txt"), []byte("expected\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "expected task change"); err != nil {
		t.Fatal(err)
	}
	expectedTaskHead := gitRevParseTest(t, wtPath, "HEAD")

	runGitTest(t, repoDir, "checkout", "-b", "wrong-merge-head", defaultBranch)
	if err := os.WriteFile(filepath.Join(repoDir, "wrong.txt"), []byte("wrong\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoDir, "add", "wrong.txt")
	runGitTest(t, repoDir, "commit", "-m", "wrong merge head")
	wrongHead := gitRevParseTest(t, repoDir, "HEAD")
	runGitTest(t, repoDir, "checkout", defaultBranch)

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapperDir := t.TempDir()
	wrapperPath := filepath.Join(wrapperDir, "git")
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = \"merge\" ] && [ \"$2\" = \"--ff-only\" ] && [ \"$3\" = \"refs/heads/" + task.WorktreeBranch + "\" ]; then\n" +
		"  exec \"" + realGit + "\" reset --hard " + wrongHead + "\n" +
		"fi\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := ws.MergeBranch(ctx, task, repoDir, "ff")
	if err == nil {
		t.Fatal("expected checked-out target merge with wrong HEAD to fail")
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed wrong-head result, got %#v", result)
	}
	if !strings.Contains(result.ErrorMessage, "expected rebased task HEAD") {
		t.Fatalf("expected wrong-head verification message, got %q", result.ErrorMessage)
	}
	if got := gitRevParseTest(t, repoDir, "HEAD"); got != wrongHead {
		t.Fatalf("expected wrapper to leave target worktree at wrong head %s, got %s", wrongHead, got)
	}
	if expectedTaskHead == wrongHead {
		t.Fatal("test setup invalid: expected task head and wrong head should differ")
	}
}

func TestMergeBranch_FastForward_UncheckedOutTargetUsesUpdateRef(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{ProjectID: "default", Title: "Update Ref FF", Category: models.CategoryActive, Status: models.StatusPending, MergeTargetBranch: defaultBranch}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName
	if err := os.WriteFile(filepath.Join(wtPath, "update_ref.txt"), []byte("update-ref\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "add update-ref feature"); err != nil {
		t.Fatal(err)
	}

	runGitTest(t, repoDir, "checkout", "-b", "parking")
	result, err := ws.MergeBranch(ctx, task, repoDir, "ff")
	if err != nil {
		t.Fatalf("MergeBranch ff update-ref fallback: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected update-ref fallback success: %s", result.ErrorMessage)
	}
	if gitRevParseTest(t, repoDir, "refs/heads/"+defaultBranch) != gitRevParseTest(t, wtPath, "HEAD") {
		t.Fatal("expected unchecked-out target branch ref to advance to task worktree HEAD")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "update_ref.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected checked-out parking worktree files not to be refreshed by update-ref fallback, err=%v", err)
	}
}

func TestMergeBranch_FastForward_StaleMainUpdateRefRejected(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{ProjectID: "default", Title: "Stale Ref FF", Category: models.CategoryActive, Status: models.StatusPending, MergeTargetBranch: defaultBranch}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName
	if err := os.WriteFile(filepath.Join(wtPath, "stale_task.txt"), []byte("task\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "task stale ref change"); err != nil {
		t.Fatal(err)
	}

	externalCommitBranch := "external-main-advance"
	runGitTest(t, repoDir, "checkout", "-b", externalCommitBranch, defaultBranch)
	if err := os.WriteFile(filepath.Join(repoDir, "external.txt"), []byte("external\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoDir, "add", "external.txt")
	runGitTest(t, repoDir, "commit", "-m", "external main advance")
	externalCommit := gitRevParseTest(t, repoDir, "HEAD")
	runGitTest(t, repoDir, "checkout", "-b", "parking")

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapperDir := t.TempDir()
	wrapperPath := filepath.Join(wrapperDir, "git")
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = \"update-ref\" ] && [ \"$2\" = \"refs/heads/" + defaultBranch + "\" ]; then\n" +
		"  \"" + realGit + "\" -C \"" + repoDir + "\" update-ref refs/heads/" + defaultBranch + " " + externalCommit + "\n" +
		"fi\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := ws.MergeBranch(ctx, task, repoDir, "ff")
	if err == nil {
		t.Fatal("expected stale target update-ref to fail")
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed stale-ref result, got %#v", result)
	}
	if got := gitRevParseTest(t, repoDir, "refs/heads/"+defaultBranch); got != externalCommit {
		t.Fatalf("expected stale update to preserve externally advanced target ref %s, got %s", externalCommit, got)
	}
}

func TestMergeBranch_FastForward_DetachedTargetCommitUsesUpdateRefFallback(t *testing.T) {
	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	head := gitRevParseTest(t, repoDir, "HEAD")
	runGitTest(t, repoDir, "checkout", "--detach", head)

	worktreePath, err := findWorktreeForBranch(repoDir, defaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	if worktreePath != "" {
		t.Fatalf("expected detached worktree at target commit not to count as checked-out %s, got %q", defaultBranch, worktreePath)
	}
}

func TestMergeBranch_SquashAllowsUnrelatedStagedUserChanges(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Squash With User Staged File",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	if err := os.WriteFile(filepath.Join(wtPath, "task_squash.txt"), []byte("task squash\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "task squash change"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "user_staged.txt"), []byte("user staged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stageCmd := exec.Command("git", "add", "user_staged.txt")
	stageCmd.Dir = repoDir
	if out, err := stageCmd.CombinedOutput(); err != nil {
		t.Fatalf("stage user file: %v\n%s", err, out)
	}

	result, err := ws.MergeBranch(ctx, task, repoDir, "squash")
	if err != nil {
		t.Fatalf("expected squash merge to allow unrelated staged user file, got %v result=%#v", err, result)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful squash merge, got %#v", result)
	}

	showCmd := exec.Command("git", "show", "--name-only", "--pretty=format:", "HEAD")
	showCmd.Dir = repoDir
	showOut, err := showCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	changedInCommit := string(showOut)
	if !strings.Contains(changedInCommit, "task_squash.txt") {
		t.Fatalf("expected squash commit to contain task file, got %q", changedInCommit)
	}
	if strings.Contains(changedInCommit, "user_staged.txt") {
		t.Fatalf("expected squash commit not to include unrelated staged user file, got %q", changedInCommit)
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(statusOut), "A  user_staged.txt") {
		t.Fatalf("expected unrelated user file to remain staged, status=%q", statusOut)
	}
}

func TestMergeBranch_SquashCommitFailureMarksMergeFailedAndDoesNotUseHardReset(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Squash Commit Failure",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	if err := os.WriteFile(filepath.Join(wtPath, "squash_hook_failure.txt"), []byte("squash commit should fail\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "commit before failing squash hook"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "user_staged_survives_failure.txt"), []byte("user staged survives failure\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stageCmd := exec.Command("git", "add", "user_staged_survives_failure.txt")
	stageCmd.Dir = repoDir
	if out, err := stageCmd.CombinedOutput(); err != nil {
		t.Fatalf("stage user file: %v\n%s", err, out)
	}

	hookPath := filepath.Join(repoDir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho blocked squash commit >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	result, err := ws.MergeBranch(ctx, task, repoDir, "squash")
	if err == nil {
		t.Fatal("expected squash commit failure")
	}
	if result == nil || !strings.Contains(result.ErrorMessage, "blocked squash commit") {
		t.Fatalf("expected squash commit failure details, got %#v", result)
	}

	dbTask, dbErr := taskRepo.GetByID(ctx, task.ID)
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	if dbTask.MergeStatus != models.MergeStatusFailed {
		t.Fatalf("expected merge_status=failed after squash commit failure, got %q", dbTask.MergeStatus)
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, statusErr := statusCmd.Output()
	if statusErr != nil {
		t.Fatalf("git status after failed squash commit: %v", statusErr)
	}
	status := string(statusOut)
	if !strings.Contains(status, "A  user_staged_survives_failure.txt") {
		t.Fatalf("expected pre-existing staged user file to survive failed squash cleanup, status=%q", statusOut)
	}
	if strings.Contains(status, "squash_hook_failure.txt") {
		t.Fatalf("expected failed squash cleanup to remove squash-produced changes, status=%q", statusOut)
	}

	serviceSource, readErr := os.ReadFile("worktree_service.go")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(serviceSource), "reset\", \"--hard") {
		t.Fatal("merge cleanup must not use git reset --hard")
	}
}

func TestResolveConflictsWithAI_NoActiveConflictsClearsStaleConflictStatus(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)
	ws.SetLLMService(&LLMService{})

	task := &models.Task{
		ProjectID:   "default",
		Title:       "Stale Conflict",
		Category:    models.CategoryActive,
		Status:      models.StatusPending,
		MergeStatus: models.MergeStatusConflict,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusConflict); err != nil {
		t.Fatal(err)
	}

	result, err := ws.ResolveConflictsWithAI(ctx, task, repoDir)
	if err == nil {
		t.Fatal("expected stale conflict resolution to fail when no active conflicts exist")
	}
	if result == nil || !strings.Contains(result.ErrorMessage, "no active merge conflicts") {
		t.Fatalf("expected no-conflicts error message, got %#v", result)
	}

	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MergeStatus != models.MergeStatusPending {
		t.Fatalf("expected stale conflict status to reset to pending, got %q", updated.MergeStatus)
	}
}

func TestMergeBranch_Squash(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	defaultBranch := GetCurrentBranch(repoDir)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Squash Merge Test",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: defaultBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	// Multiple commits
	os.WriteFile(filepath.Join(wtPath, "file1.txt"), []byte("file1\n"), 0644)
	CommitWorktreeChanges(wtPath, "commit 1")
	os.WriteFile(filepath.Join(wtPath, "file2.txt"), []byte("file2\n"), 0644)
	CommitWorktreeChanges(wtPath, "commit 2")

	result, err := ws.MergeBranch(ctx, task, repoDir, "squash")
	if err != nil {
		t.Fatalf("MergeBranch squash: %v", err)
	}
	if !result.Success {
		t.Errorf("expected squash merge success: %s", result.ErrorMessage)
	}
}

func TestCleanupWorktree(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	task := &models.Task{
		ProjectID: "default",
		Title:     "Cleanup Test Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	wtPath, branchName, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = branchName

	// Cleanup
	if err := ws.CleanupWorktree(ctx, task, repoDir, true); err != nil {
		t.Fatalf("CleanupWorktree: %v", err)
	}

	// Verify directory removed
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("expected worktree directory to be removed")
	}

	// Verify branch deleted
	checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = repoDir
	if checkCmd.Run() == nil {
		t.Error("expected branch to be deleted")
	}

	// Verify task DB cleared
	dbTask, _ := taskRepo.GetByID(ctx, task.ID)
	if dbTask.WorktreePath != "" {
		t.Error("expected worktree_path to be cleared in DB")
	}
	if dbTask.WorktreeBranch != "" {
		t.Error("expected worktree_branch to be cleared in DB")
	}
}

func TestCreateTaskWithAutoMerge(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Auto Merge Task",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		AutoMerge:         true,
		MergeTargetBranch: "develop",
	}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.GetByID(ctx, task.ID)
	if !got.AutoMerge {
		t.Error("expected auto_merge=true")
	}
	if got.MergeTargetBranch != "develop" {
		t.Errorf("expected merge_target_branch=develop, got %q", got.MergeTargetBranch)
	}
}

func TestIsBranchMerged(t *testing.T) {
	repoDir := createTestGitRepo(t)
	ctx := context.Background()
	_ = ctx

	// Create a test branch
	branchName := "feature/test"
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating branch failed: %v", err)
	}

	// Make a change and commit
	testFile := filepath.Join(repoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "test commit")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Branch should NOT be merged yet
	mainBranch := GetDefaultBranch(repoDir)
	if IsBranchMerged(repoDir, branchName, mainBranch) {
		t.Error("branch should not be merged before merging")
	}

	// Checkout main and merge
	cmd = exec.Command("git", "checkout", mainBranch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("checkout main failed: %v", err)
	}
	cmd = exec.Command("git", "merge", "--no-ff", "-m", "merge test", branchName)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Branch should be merged now
	if !IsBranchMerged(repoDir, branchName, mainBranch) {
		t.Error("branch should be merged after merging")
	}

	// Non-existent branch should be considered merged (cleanup edge case)
	if !IsBranchMerged(repoDir, "non-existent-branch", mainBranch) {
		t.Error("non-existent branch should be considered merged")
	}
}

func TestCleanupMergedWorktrees(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	// Create a test git repo
	repoDir := createTestGitRepo(t)
	mainBranch := GetDefaultBranch(repoDir)

	// Create project with repo path
	project := &models.Project{
		Name:     "Test Project",
		RepoPath: repoDir,
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	// Set cleanup policy to "after_merge"
	if err := settingsRepo.Set(ctx, "worktree_cleanup", "after_merge"); err != nil {
		t.Fatal(err)
	}

	// Create worktree service
	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	// Create a task with worktree
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Test Task",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		MergeTargetBranch: mainBranch,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	// Setup worktree
	wtPath, wtBranch, err := ws.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("SetupWorktree failed: %v", err)
	}

	// Make a change in the worktree
	testFile := filepath.Join(wtPath, "worktree_test.txt")
	if err := os.WriteFile(testFile, []byte("test from worktree\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit the change
	if err := CommitWorktreeChanges(wtPath, "test commit from worktree"); err != nil {
		t.Fatalf("CommitWorktreeChanges failed: %v", err)
	}

	// Merge the worktree branch to main manually (simulating manual merge)
	cmd := exec.Command("git", "checkout", mainBranch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("checkout main failed: %v", err)
	}
	cmd = exec.Command("git", "merge", "--no-ff", "-m", "manual merge", wtBranch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("manual merge failed: %v", err)
	}

	// Verify the branch is merged
	if !IsBranchMerged(repoDir, wtBranch, mainBranch) {
		t.Error("branch should be merged")
	}

	// Mark task as completed (cleanup skips running/pending tasks)
	if err := taskRepo.UpdateStatus(ctx, task.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	// Run the cleanup scan
	if err := ws.CleanupMergedWorktrees(ctx); err != nil {
		t.Errorf("CleanupMergedWorktrees failed: %v", err)
	}

	// Verify the worktree was cleaned up
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed")
	}

	// Verify the branch was deleted
	cmd = exec.Command("git", "rev-parse", "--verify", wtBranch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err == nil {
		t.Error("branch should be deleted")
	}

	// Verify task DB was updated
	dbTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbTask.WorktreePath != "" {
		t.Error("expected worktree_path to be cleared in DB")
	}
	if dbTask.WorktreeBranch != "" {
		t.Error("expected worktree_branch to be cleared in DB")
	}
	if dbTask.MergeStatus != models.MergeStatusMerged {
		t.Errorf("expected merge_status=merged, got %q", dbTask.MergeStatus)
	}
}

func TestCleanupMergedWorktrees_KeepPolicy(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	// Set cleanup policy to "keep"
	if err := settingsRepo.Set(ctx, "worktree_cleanup", "keep"); err != nil {
		t.Fatal(err)
	}

	ws := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	// Should return immediately with "keep" policy
	if err := ws.CleanupMergedWorktrees(ctx); err != nil {
		t.Errorf("CleanupMergedWorktrees should not error with keep policy: %v", err)
	}
}

func TestCleanupOrphanedWorktrees(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	settingsRepo := repository.NewSettingsRepo(db)
	worktreeSvc := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	// Create a test project with a git repo
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "test-repo")
	exec.Command("git", "init", repoDir).Run()
	exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoDir, "config", "user.name", "Test User").Run()

	// Create initial commit
	testFile := filepath.Join(repoDir, "README.md")
	os.WriteFile(testFile, []byte("# Test"), 0644)
	exec.Command("git", "-C", repoDir, "add", ".").Run()
	exec.Command("git", "-C", repoDir, "commit", "-m", "initial").Run()

	// Set cleanup policy to after_merge
	settingsRepo.Set(ctx, "worktree_cleanup", "after_merge")

	project := &models.Project{
		ID:       "test-project",
		Name:     "Test Project",
		RepoPath: repoDir,
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create a task and its worktree
	task := &models.Task{
		ID:        "test-task",
		ProjectID: project.ID,
		Title:     "Test Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create worktree for the task
	worktreeDir, branch, err := worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Verify worktree exists
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		t.Fatal("worktree directory was not created")
	}

	// Verify branch exists
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal("worktree branch was not created")
	}

	// Now delete the task from the database (simulating orphaned worktree)
	if err := taskRepo.Delete(ctx, task.ID); err != nil {
		t.Fatalf("failed to delete task: %v", err)
	}

	// Verify worktree still exists on disk
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		t.Fatal("worktree directory should still exist after task deletion")
	}

	// Run orphaned worktree cleanup
	cleanedCount, err := worktreeSvc.CleanupOrphanedWorktrees(ctx)
	if err != nil {
		t.Fatalf("CleanupOrphanedWorktrees failed: %v", err)
	}

	if cleanedCount != 1 {
		t.Errorf("expected 1 orphaned worktree to be cleaned, got %d", cleanedCount)
	}

	// Verify worktree directory was removed
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Error("expected orphaned worktree directory to be removed")
	}

	// Verify branch was deleted
	cmd = exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err == nil {
		t.Error("expected orphaned branch to be deleted")
	}
}

func TestCleanupOrphanedWorktrees_SkipsWhenTaskStillExists(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	settingsRepo := repository.NewSettingsRepo(db)
	worktreeSvc := NewWorktreeService(taskRepo, projectRepo, settingsRepo)

	repoDir := createTestGitRepo(t)
	if err := settingsRepo.Set(ctx, "worktree_cleanup", "after_merge"); err != nil {
		t.Fatalf("failed to set cleanup policy: %v", err)
	}

	project := &models.Project{
		ID:       "project-skip-existing-task",
		Name:     "Skip Existing Task",
		RepoPath: repoDir,
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	task := &models.Task{
		ID:        "existing-task-id",
		ProjectID: project.ID,
		Title:     "Existing Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	worktreeDir, branch, err := worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Simulate stale metadata: task exists but worktree fields are empty in DB.
	if err := taskRepo.ClearWorktreeInfo(ctx, task.ID); err != nil {
		t.Fatalf("failed to clear worktree info: %v", err)
	}

	cleanedCount, err := worktreeSvc.CleanupOrphanedWorktrees(ctx)
	if err != nil {
		t.Fatalf("CleanupOrphanedWorktrees failed: %v", err)
	}
	if cleanedCount != 0 {
		t.Fatalf("expected 0 orphaned worktrees cleaned, got %d", cleanedCount)
	}

	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		t.Fatalf("worktree should not be cleaned while task %s still exists", task.ID)
	}

	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("worktree branch should still exist: %v", err)
	}
}

func TestTaskIDFromWorktreePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		id   string
		ok   bool
	}{
		{name: "valid", path: "/tmp/repo/.worktrees/task_abc123", id: "abc123", ok: true},
		{name: "missing prefix", path: "/tmp/repo/.worktrees/abc123", id: "", ok: false},
		{name: "empty id", path: "/tmp/repo/.worktrees/task_", id: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := taskIDFromWorktreePath(tt.path)
			if id != tt.id || ok != tt.ok {
				t.Fatalf("taskIDFromWorktreePath(%q) = (%q, %v), want (%q, %v)", tt.path, id, ok, tt.id, tt.ok)
			}
		})
	}
}

func TestListGitWorktrees(t *testing.T) {
	// Create a test git repo
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "test-repo")
	exec.Command("git", "init", repoDir).Run()
	exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoDir, "config", "user.name", "Test User").Run()

	// Create initial commit
	testFile := filepath.Join(repoDir, "README.md")
	os.WriteFile(testFile, []byte("# Test"), 0644)
	exec.Command("git", "-C", repoDir, "add", ".").Run()
	exec.Command("git", "-C", repoDir, "commit", "-m", "initial").Run()

	// Create a worktree
	worktreeDir := filepath.Join(tempDir, "test-worktree")
	exec.Command("git", "-C", repoDir, "worktree", "add", worktreeDir, "-b", "test-branch").Run()

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	resolvedWorktreeDir, _ := filepath.EvalSymlinks(worktreeDir)
	if resolvedWorktreeDir == "" {
		resolvedWorktreeDir = worktreeDir
	}

	// List worktrees
	worktrees, err := ListGitWorktrees(repoDir)
	if err != nil {
		t.Fatalf("ListGitWorktrees failed: %v", err)
	}

	// Should have at least 2 worktrees: main repo + test worktree
	if len(worktrees) < 2 {
		t.Fatalf("expected at least 2 worktrees, got %d", len(worktrees))
	}

	// Find the test worktree
	var foundTestWorktree bool
	for _, wt := range worktrees {
		// Resolve symlinks for comparison
		resolvedWtPath, _ := filepath.EvalSymlinks(wt.Path)
		if resolvedWtPath == "" {
			resolvedWtPath = wt.Path
		}

		if resolvedWtPath == resolvedWorktreeDir {
			foundTestWorktree = true
			if wt.Branch != "test-branch" {
				t.Errorf("expected branch 'test-branch', got '%s'", wt.Branch)
			}
			if wt.IsMain {
				t.Error("test worktree should not be marked as main")
			}
		}
	}

	if !foundTestWorktree {
		t.Errorf("test worktree not found in list")
	}
}

func TestGetWorktreeDiffWithUncommitted(t *testing.T) {
	repoDir := createTestGitRepo(t)

	// Create a feature branch and worktree
	branchName := "task/test-uncommitted"
	mainBranch := GetDefaultBranch(repoDir)
	wtPath := filepath.Join(repoDir, ".worktrees", "task_test")

	cmd := exec.Command("git", "worktree", "add", "-b", branchName, wtPath, mainBranch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create worktree: %v\n%s", err, out)
	}

	// Set git config in worktree
	for _, args := range [][]string{
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtPath
		cmd.Run()
	}

	// Test 1: No changes - should return empty
	diff := GetWorktreeDiffWithUncommitted(repoDir, branchName, mainBranch, wtPath)
	if diff != "" {
		t.Errorf("expected empty diff with no changes, got: %q", diff)
	}

	// Test 2: Committed changes only
	if err := os.WriteFile(filepath.Join(wtPath, "committed.txt"), []byte("committed content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CommitWorktreeChanges(wtPath, "add committed file"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	diff = GetWorktreeDiffWithUncommitted(repoDir, branchName, mainBranch, wtPath)
	if !strings.Contains(diff, "committed.txt") {
		t.Error("expected committed changes to appear in diff")
	}

	// Test 3: Uncommitted changes should also appear
	if err := os.WriteFile(filepath.Join(wtPath, "uncommitted.txt"), []byte("uncommitted content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	diff = GetWorktreeDiffWithUncommitted(repoDir, branchName, mainBranch, wtPath)
	if !strings.Contains(diff, "committed.txt") {
		t.Error("expected committed changes to still appear")
	}
	if !strings.Contains(diff, "uncommitted.txt") {
		t.Error("expected uncommitted (untracked) file to appear in diff")
	}

	// Test 4: Modified tracked file (uncommitted) should appear
	if err := os.WriteFile(filepath.Join(wtPath, "committed.txt"), []byte("modified content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	diff = GetWorktreeDiffWithUncommitted(repoDir, branchName, mainBranch, wtPath)
	if !strings.Contains(diff, "modified content") {
		t.Error("expected uncommitted modification to appear in diff")
	}

	// Test 5: Without worktree path, only committed changes shown
	diff = GetWorktreeDiffWithUncommitted(repoDir, branchName, mainBranch, "")
	if !strings.Contains(diff, "committed.txt") {
		t.Error("expected committed changes with empty worktree path")
	}
	if strings.Contains(diff, "uncommitted.txt") {
		t.Error("should not show untracked files when worktree path is empty")
	}
}

func TestGetWorktreeDiffUsesCurrentTargetTreeNotStaleMergeBase(t *testing.T) {
	repoDir := createTestGitRepo(t)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	defaultBranch := GetCurrentBranch(repoDir)
	if defaultBranch == "" {
		defaultBranch = GetDefaultBranch(repoDir)
	}

	// Simulate an older task branch with a large historical change.
	runGit("checkout", "-b", "task/stale-base")
	if err := os.WriteFile(filepath.Join(repoDir, "large-feature.txt"), []byte("large feature\n"), 0644); err != nil {
		t.Fatalf("write large feature: %v", err)
	}
	runGit("add", "large-feature.txt")
	runGit("commit", "-m", "large task change")

	// Target advances with an equivalent squashed version of that old change.
	runGit("checkout", defaultBranch)
	if err := os.WriteFile(filepath.Join(repoDir, "large-feature.txt"), []byte("large feature\n"), 0644); err != nil {
		t.Fatalf("write squashed large feature: %v", err)
	}
	runGit("add", "large-feature.txt")
	runGit("commit", "-m", "squash large task change")

	// The task branch adds only a small follow-up after the target already has
	// the old large change. A merge-base diff would still show large-feature.txt;
	// the Changes UI should show only the current net difference.
	runGit("checkout", "task/stale-base")
	if err := os.WriteFile(filepath.Join(repoDir, "followup.txt"), []byte("small followup\n"), 0644); err != nil {
		t.Fatalf("write followup: %v", err)
	}
	runGit("add", "followup.txt")
	runGit("commit", "-m", "small followup")

	diff := GetWorktreeDiff(repoDir, "task/stale-base", defaultBranch)
	if strings.Contains(diff, "large-feature.txt") {
		t.Fatalf("expected stale historical change to be omitted from current target-tree diff, got:\n%s", diff)
	}
	if !strings.Contains(diff, "followup.txt") || !strings.Contains(diff, "small followup") {
		t.Fatalf("expected follow-up change in diff, got:\n%s", diff)
	}

	stats := GetWorktreeFileStats(repoDir, "task/stale-base", defaultBranch)
	if len(stats) != 1 {
		t.Fatalf("expected one current file stat, got %#v", stats)
	}
	if stats[0].Path != "followup.txt" {
		t.Fatalf("expected file stats to omit stale historical change, got %#v", stats)
	}
}
