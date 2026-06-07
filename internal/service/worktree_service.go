package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

// WorktreeService manages git worktrees for task isolation.
type WorktreeService struct {
	taskRepo     *repository.TaskRepo
	projectRepo  *repository.ProjectRepo
	settingsRepo *repository.SettingsRepo
	llmSvc       *LLMService
	githubSvc    *GitHubService
}

func NewWorktreeService(taskRepo *repository.TaskRepo, projectRepo *repository.ProjectRepo, settingsRepo *repository.SettingsRepo) *WorktreeService {
	return &WorktreeService{
		taskRepo:     taskRepo,
		projectRepo:  projectRepo,
		settingsRepo: settingsRepo,
	}
}

// SetLLMService sets the LLM service for AI-assisted conflict resolution.
func (ws *WorktreeService) SetLLMService(llmSvc *LLMService) {
	ws.llmSvc = llmSvc
}

// SetGitHubService sets GitHub service for remote git auth when syncing worktrees.
func (ws *WorktreeService) SetGitHubService(githubSvc *GitHubService) {
	ws.githubSvc = githubSvc
}

// slugify creates a branch-name-safe slug from a string.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// IsGitRepo checks if the given directory is inside a git repository.
func IsGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// GetDefaultBranch returns the name of the default branch (main or master).
func GetDefaultBranch(repoDir string) string {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		// Strip "origin/" prefix
		parts := strings.SplitN(branch, "/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return branch
	}
	// Fallback: check if main or master branch exists
	for _, name := range []string{"main", "master"} {
		checkCmd := exec.Command("git", "rev-parse", "--verify", name)
		checkCmd.Dir = repoDir
		if checkCmd.Run() == nil {
			return name
		}
	}
	return "main"
}

// GetCurrentBranch returns the current branch name.
func GetCurrentBranch(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func GitStatusPorcelain(repoDir string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SetupWorktree creates a git worktree for a task.
// For chained tasks with lineage metadata (BaseCommitSHA/BaseBranch), the worktree
// is created from the parent's commit SHA so child tasks inherit parent code changes.
// Returns the worktree path and branch name, or error.
func (ws *WorktreeService) SetupWorktree(ctx context.Context, task *models.Task, repoDir string) (worktreePath string, branchName string, err error) {
	return ws.setupWorktree(ctx, task, repoDir, false)
}

// SetupFollowupWorktree resolves the worktree for a task-thread follow-up. Terminal
// merged/stale tasks continue from the current merge target on a fresh follow-up
// branch instead of trying to merge current target back into the historical branch.
// The returned skipStartupSync flag is true when the new branch was just created
// from the current target and therefore does not need startup auto-sync.
func (ws *WorktreeService) SetupFollowupWorktree(ctx context.Context, task *models.Task, repoDir string) (worktreePath string, branchName string, skipStartupSync bool, err error) {
	if repoDir == "" || !IsGitRepo(repoDir) {
		return "", "", false, fmt.Errorf("not a git repository: %s", repoDir)
	}

	if ws.shouldReuseStoredFollowupWorktree(task, repoDir) {
		applog.Infof("[worktree] reusing stored follow-up worktree task=%s path=%s branch=%s", task.ID, task.WorktreePath, task.WorktreeBranch)
		if updateErr := ws.taskRepo.UpdateWorktreeInfo(ctx, task.ID, task.WorktreePath, task.WorktreeBranch); updateErr != nil {
			applog.Infof("[worktree] error updating stored follow-up worktree info: %v", updateErr)
		}
		return task.WorktreePath, task.WorktreeBranch, false, nil
	}

	if ws.shouldContinueFollowupFromCurrentTarget(task, repoDir) {
		wtPath, wtBranch, setupErr := ws.setupWorktree(ctx, task, repoDir, true)
		return wtPath, wtBranch, true, setupErr
	}

	wtPath, wtBranch, setupErr := ws.SetupWorktree(ctx, task, repoDir)
	return wtPath, wtBranch, false, setupErr
}

func (ws *WorktreeService) setupWorktree(ctx context.Context, task *models.Task, repoDir string, continueFromCurrentTarget bool) (worktreePath string, branchName string, err error) {
	if repoDir == "" || !IsGitRepo(repoDir) {
		return "", "", fmt.Errorf("not a git repository: %s", repoDir)
	}

	baseRef := ws.resolveWorktreeBaseRef(ctx, task, repoDir, continueFromCurrentTarget)
	if baseRef == "" {
		return "", "", fmt.Errorf("could not resolve base ref for task %s", task.ID)
	}

	// If this is a chained task and we couldn't resolve lineage, log a clear error
	if !continueFromCurrentTarget && task.ParentTaskID != nil && task.BaseCommitSHA != "" && baseRef != task.BaseCommitSHA {
		applog.Infof("[worktree] WARNING: chained task %s could not use parent lineage SHA %s, using fallback base %s", task.ID, task.BaseCommitSHA, baseRef)
	}

	// Create branch name from task
	slug := slugify(task.Title)
	if slug == "" {
		slug = task.ID[:8]
	}
	branchName = fmt.Sprintf("task/%s-%s", task.ID[:8], slug)
	if continueFromCurrentTarget {
		branchName = fmt.Sprintf("task/%s-followup-%d", task.ID[:8], time.Now().UnixNano())
	}

	// Worktree directory
	worktreePath = filepath.Join(repoDir, ".worktrees", fmt.Sprintf("task_%s", task.ID))
	if continueFromCurrentTarget {
		worktreePath = filepath.Join(repoDir, ".worktrees", fmt.Sprintf("task_%s_followup_%d", task.ID, time.Now().UnixNano()))
	}

	// Check if worktree already exists
	if !continueFromCurrentTarget {
		if storedPath, storedBranch, ok := ws.existingStoredWorktree(task); ok {
			ws.clearStaleConflictStatusIfClean(ctx, task)
			applog.Infof("[worktree] stored worktree already exists at %s, reusing", storedPath)
			if updateErr := ws.taskRepo.UpdateWorktreeInfo(ctx, task.ID, storedPath, storedBranch); updateErr != nil {
				applog.Infof("[worktree] error updating worktree info: %v", updateErr)
			}
			return storedPath, storedBranch, nil
		}
		if _, err := os.Stat(worktreePath); err == nil {
			applog.Infof("[worktree] worktree already exists at %s, reusing", worktreePath)
			if updateErr := ws.taskRepo.UpdateWorktreeInfo(ctx, task.ID, worktreePath, branchName); updateErr != nil {
				applog.Infof("[worktree] error updating worktree info: %v", updateErr)
			}
			return worktreePath, branchName, nil
		}
	}

	// Check if branch already exists
	checkBranch := exec.Command("git", "rev-parse", "--verify", branchName)
	checkBranch.Dir = repoDir
	branchExists := checkBranch.Run() == nil

	if branchExists {
		// Branch exists, create worktree pointing to it
		cmd := exec.Command("git", "worktree", "add", worktreePath, branchName)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("creating worktree for existing branch: %w: %s", err, string(out))
		}
	} else {
		// Create new branch from the resolved base ref
		cmd := exec.Command("git", "worktree", "add", "-b", branchName, worktreePath, baseRef)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("creating worktree from base %s: %w: %s", baseRef, err, string(out))
		}
	}

	applog.Infof("[worktree] created worktree at %s on branch %s (base: %s) for task %s (lineage_depth=%d)", worktreePath, branchName, baseRef, task.ID, task.LineageDepth)

	// Update task record with worktree info
	if updateErr := ws.taskRepo.UpdateWorktreeInfo(ctx, task.ID, worktreePath, branchName); updateErr != nil {
		applog.Infof("[worktree] error updating worktree info: %v", updateErr)
	}
	if task.MergeTargetBranch == "" {
		mergeTarget := baseRef
		if !continueFromCurrentTarget && task.BaseBranch != "" {
			// Use parent's branch as merge target so changes merge back correctly.
			mergeTarget = task.BaseBranch
		}
		task.MergeTargetBranch = mergeTarget
		if updateErr := ws.taskRepo.UpdateAutoMerge(ctx, task.ID, task.AutoMerge, mergeTarget); updateErr != nil {
			applog.Infof("[worktree] error setting merge target branch: %v", updateErr)
		}
	}

	return worktreePath, branchName, nil
}

func (ws *WorktreeService) resolveWorktreeBaseRef(ctx context.Context, task *models.Task, repoDir string, continueFromCurrentTarget bool) string {
	if continueFromCurrentTarget {
		baseRef := task.MergeTargetBranch
		if baseRef == "" {
			baseRef = ws.getGlobalMergeTarget(ctx)
		}
		if baseRef == "" {
			baseRef = GetDefaultBranch(repoDir)
		}
		return baseRef
	}

	// Determine the base ref to branch from.
	// Priority for chained tasks: BaseCommitSHA > BaseBranch > MergeTargetBranch > global > default
	baseRef := ""
	if task.BaseCommitSHA != "" {
		// Verify the SHA exists in the repo
		checkSHA := exec.Command("git", "cat-file", "-t", task.BaseCommitSHA)
		checkSHA.Dir = repoDir
		if checkSHA.Run() == nil {
			baseRef = task.BaseCommitSHA
			applog.Infof("[worktree] using lineage commit SHA %s as base for task %s (depth=%d)", baseRef, task.ID, task.LineageDepth)
		} else {
			applog.Infof("[worktree] lineage commit SHA %s not found in repo for task %s, falling back", task.BaseCommitSHA, task.ID)
		}
	}
	if baseRef == "" && task.BaseBranch != "" {
		// Verify the branch exists
		checkBr := exec.Command("git", "rev-parse", "--verify", task.BaseBranch)
		checkBr.Dir = repoDir
		if checkBr.Run() == nil {
			baseRef = task.BaseBranch
			applog.Infof("[worktree] using lineage branch %s as base for task %s (depth=%d)", baseRef, task.ID, task.LineageDepth)
		} else {
			applog.Infof("[worktree] lineage branch %s not found in repo for task %s, falling back", task.BaseBranch, task.ID)
		}
	}

	// Standard fallback chain for non-chained tasks or if lineage refs not found
	if baseRef == "" {
		baseRef = task.MergeTargetBranch
		if baseRef == "" {
			baseRef = ws.getGlobalMergeTarget(ctx)
		}
		if baseRef == "" {
			baseRef = GetDefaultBranch(repoDir)
		}
	}
	return baseRef
}

func (ws *WorktreeService) existingStoredWorktree(task *models.Task) (string, string, bool) {
	if task.WorktreePath == "" || task.WorktreeBranch == "" {
		return "", "", false
	}
	if _, err := os.Stat(task.WorktreePath); err != nil {
		return "", "", false
	}
	return task.WorktreePath, task.WorktreeBranch, true
}

func (ws *WorktreeService) shouldContinueFollowupFromCurrentTarget(task *models.Task, repoDir string) bool {
	if task == nil || !models.IsTerminalStatus(task.Status) {
		return false
	}
	if task.MergeStatus == models.MergeStatusMerged || task.MergeStatus == models.MergeStatusConflict {
		return true
	}
	return task.WorktreeBranch != "" && ws.isBranchTipMergedIntoTarget(repoDir, task.WorktreeBranch, task.MergeTargetBranch)
}

func (ws *WorktreeService) shouldReuseStoredFollowupWorktree(task *models.Task, repoDir string) bool {
	if task == nil || task.WorktreePath == "" || task.WorktreeBranch == "" || !strings.Contains(task.WorktreeBranch, "-followup-") {
		return false
	}
	if _, err := os.Stat(task.WorktreePath); err != nil {
		return false
	}
	status, err := GitStatusPorcelain(task.WorktreePath)
	if err != nil {
		return false
	}
	if strings.TrimSpace(status) != "" {
		return true
	}
	if task.MergeStatus == models.MergeStatusMerged || task.MergeStatus == models.MergeStatusConflict {
		return !ws.branchHasCommitsBeyondTarget(repoDir, task.WorktreeBranch, task.MergeTargetBranch)
	}
	return true
}

func (ws *WorktreeService) isBranchTipMergedIntoTarget(repoDir, branchName, targetBranch string) bool {
	if branchName == "" {
		return false
	}
	if targetBranch == "" {
		targetBranch = GetDefaultBranch(repoDir)
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", branchName, targetBranch)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

func (ws *WorktreeService) branchHasCommitsBeyondTarget(repoDir, branchName, targetBranch string) bool {
	if branchName == "" {
		return false
	}
	if targetBranch == "" {
		targetBranch = GetDefaultBranch(repoDir)
	}
	cmd := exec.Command("git", "rev-list", "--count", fmt.Sprintf("%s..%s", targetBranch, branchName))
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0"
}

func worktreeHasActiveMerge(worktreePath string) bool {
	if worktreePath == "" {
		return false
	}
	cmd := exec.Command("git", "rev-parse", "--git-path", "MERGE_HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	mergeHeadPath := strings.TrimSpace(string(out))
	if !filepath.IsAbs(mergeHeadPath) {
		mergeHeadPath = filepath.Join(worktreePath, mergeHeadPath)
	}
	_, statErr := os.Stat(mergeHeadPath)
	return statErr == nil
}

func worktreeHasConflictFiles(worktreePath string) bool {
	return len(detectConflicts(worktreePath)) > 0
}

func (ws *WorktreeService) clearStaleConflictStatusIfClean(ctx context.Context, task *models.Task) {
	if task == nil || task.MergeStatus != models.MergeStatusConflict || task.WorktreePath == "" || ws.taskRepo == nil {
		return
	}
	status, err := GitStatusPorcelain(task.WorktreePath)
	if err != nil || strings.TrimSpace(status) != "" || worktreeHasActiveMerge(task.WorktreePath) || worktreeHasConflictFiles(task.WorktreePath) {
		return
	}
	if err := ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusPending); err != nil {
		applog.Infof("[worktree] error clearing stale conflict status for task %s: %v", task.ID, err)
		return
	}
	task.MergeStatus = models.MergeStatusPending
	applog.Infof("[worktree] cleared stale conflict status for task %s after clean aborted merge state", task.ID)
}

// SyncWorktreeFromMainAtStart updates a task branch with the latest main/default branch
// before task execution begins. It only runs when the worktree is clean.
func (ws *WorktreeService) SyncWorktreeFromMainAtStart(ctx context.Context, task *models.Task, repoDir string) error {
	if task == nil || task.WorktreePath == "" {
		return nil
	}
	authEnv := []string(nil)
	if ws.githubSvc != nil {
		authEnv = ws.githubSvc.GitAuthEnvForRepo(ctx, repoDir)
	}

	runGit := func(dir string, args ...string) ([]byte, error) {
		cmdCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cmdCtx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ASKPASS=true",
			"SSH_ASKPASS=true",
		)
		if len(authEnv) > 0 {
			cmd.Env = append(cmd.Env, authEnv...)
		}
		return cmd.CombinedOutput()
	}

	currentBranch := GetCurrentBranch(task.WorktreePath)
	if currentBranch == "" {
		currentBranch = task.WorktreeBranch
	}
	applog.Infof("[worktree] startup auto-merge check task=%s worktree=%s branch=%s", task.ID, task.WorktreePath, currentBranch)

	statusOut, statusErr := runGit(task.WorktreePath, "status", "--porcelain")
	if statusErr != nil {
		applog.Infof("[worktree] startup auto-merge failed task=%s unable to read git status: %v", task.ID, statusErr)
		if ws.taskRepo != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		}
		return fmt.Errorf("could not check worktree status in %s: %w", task.WorktreePath, statusErr)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		applog.Infof("[worktree] startup auto-merge skipped task=%s branch=%s reason=dirty_worktree", task.ID, currentBranch)
		return nil
	}
	ws.clearStaleConflictStatusIfClean(ctx, task)

	syncBranch := task.MergeTargetBranch
	if syncBranch == "" {
		syncBranch = "main"
		hasMain := false
		if _, err := runGit(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/main"); err == nil {
			hasMain = true
		} else {
			_, err = runGit(repoDir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/main")
			hasMain = err == nil
		}
		if !hasMain {
			syncBranch = GetDefaultBranch(repoDir)
		}
	}

	mergeSource := syncBranch
	if _, originErr := runGit(repoDir, "remote", "get-url", "origin"); originErr == nil {
		fetchOut, fetchErr := runGit(task.WorktreePath, "fetch", "origin", syncBranch)
		if fetchErr != nil {
			applog.Infof("[worktree] startup auto-merge task=%s fetch origin/%s skipped (non-fatal): %s", task.ID, syncBranch, strings.TrimSpace(string(fetchOut)))
			mergeSource = syncBranch
		} else {
			mergeSource = "origin/" + syncBranch
		}
	} else {
		applog.Infof("[worktree] startup auto-merge task=%s no origin remote, using local %s", task.ID, syncBranch)
	}

	mergeOut, mergeErr := runGit(task.WorktreePath, "merge", "--no-edit", mergeSource)
	mergeMsg := strings.TrimSpace(string(mergeOut))
	if mergeErr != nil {
		conflictFiles := detectConflicts(task.WorktreePath)
		if len(conflictFiles) > 0 {
			abortErr := AbortMerge(task.WorktreePath)
			if ws.taskRepo != nil {
				_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusConflict)
			}
			action := fmt.Sprintf("startup auto-merge conflict while merging %s into %s (conflicts: %s); merge was aborted. Resolve conflicts in %s and rerun the task", mergeSource, currentBranch, strings.Join(conflictFiles, ", "), task.WorktreePath)
			if abortErr != nil {
				action = fmt.Sprintf("%s; additionally, git merge --abort failed: %v", action, abortErr)
			}
			applog.Infof("[worktree] startup auto-merge failed task=%s reason=conflict details=%s", task.ID, action)
			return fmt.Errorf("%s", action)
		}

		if ws.taskRepo != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		}
		if mergeMsg == "" {
			mergeMsg = mergeErr.Error()
		}
		applog.Infof("[worktree] startup auto-merge failed task=%s branch=%s source=%s error=%s", task.ID, currentBranch, mergeSource, mergeMsg)
		return fmt.Errorf("startup auto-merge failed while merging %s into %s: %s", mergeSource, currentBranch, mergeMsg)
	}

	if mergeMsg == "" {
		mergeMsg = "already up to date"
	}
	applog.Infof("[worktree] startup auto-merge ran task=%s branch=%s source=%s result=%s", task.ID, currentBranch, mergeSource, mergeMsg)
	return nil
}

// CommitWorktreeChanges stages and commits all changes in the worktree.
func CommitWorktreeChanges(worktreePath string, message string) error {
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("empty commit message")
	}

	// Check for changes
	out, err := GitStatusPorcelain(worktreePath)
	if err != nil {
		return fmt.Errorf("checking git status: %w", err)
	}
	if len(strings.TrimSpace(out)) == 0 {
		return nil // no changes
	}

	// Ensure git identity is set (required for commits). Check email and name
	// independently because a repo/environment may configure only one of them.
	checkEmailCmd := exec.Command("git", "config", "user.email")
	checkEmailCmd.Dir = worktreePath
	if out, _ := checkEmailCmd.Output(); len(strings.TrimSpace(string(out))) == 0 {
		exec.Command("git", "-C", worktreePath, "config", "user.email", "bot@openvibely.ai").Run()
	}
	checkNameCmd := exec.Command("git", "config", "user.name")
	checkNameCmd.Dir = worktreePath
	if out, _ := checkNameCmd.Output(); len(strings.TrimSpace(string(out))) == 0 {
		exec.Command("git", "-C", worktreePath, "config", "user.name", "OpenVibely Bot").Run()
	}

	// Stage all changes
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = worktreePath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("staging changes: %w: %s", err, string(out))
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = worktreePath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("committing changes: %w: %s", err, string(out))
	}

	return nil
}

// MergeResult holds the result of a merge operation.
type MergeResult struct {
	Success       bool
	MergeCommit   string
	ConflictFiles []string
	ErrorMessage  string
}

// MergeBranch merges the task branch into the target branch.
// mergeType: "merge" (merge commit), "ff" (fast-forward only), "squash"
func (ws *WorktreeService) MergeBranch(ctx context.Context, task *models.Task, repoDir string, mergeType string) (*MergeResult, error) {
	if task.WorktreeBranch == "" {
		return nil, fmt.Errorf("task has no worktree branch")
	}

	targetBranch := task.MergeTargetBranch
	if targetBranch == "" {
		targetBranch = GetDefaultBranch(repoDir)
	}

	// First, commit any uncommitted changes in the worktree for merge modes
	// that create a merge/squash commit. Fast-forward task-worktree merges must
	// reject dirty worktrees so the rebase and ref update operate on committed
	// task branch state only.
	if task.WorktreePath != "" && mergeType != "ff" {
		if err := CommitWorktreeChanges(task.WorktreePath, fmt.Sprintf("Auto-commit changes for task: %s", task.Title)); err != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			return &MergeResult{ErrorMessage: err.Error()}, fmt.Errorf("auto-commit before merge failed: %w", err)
		}
	}

	// Update merge status to pending
	_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusPending)

	if mergeType == "ff" && task.WorktreePath != "" {
		return ws.fastForwardTaskWorktreeToTarget(ctx, task, repoDir, targetBranch)
	}

	var stagedBeforeSquash map[string]bool
	if mergeType == "squash" {
		var err error
		stagedBeforeSquash, err = StagedPaths(repoDir)
		if err != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			return &MergeResult{ErrorMessage: fmt.Sprintf("checking staged files before squash: %s", err.Error())}, fmt.Errorf("checking staged files before squash: %w", err)
		}
	}

	// Checkout target branch in the main repo
	checkoutCmd := exec.Command("git", "checkout", targetBranch)
	checkoutCmd.Dir = repoDir
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: fmt.Sprintf("checkout target: %s", string(out))}, fmt.Errorf("checkout target branch: %w", err)
	}

	// Build merge command based on type
	var mergeArgs []string
	switch mergeType {
	case "ff":
		mergeArgs = []string{"merge", "--ff-only", task.WorktreeBranch}
	case "squash":
		mergeArgs = []string{"merge", "--squash", task.WorktreeBranch}
	default:
		mergeArgs = []string{"merge", "--no-ff", "-m", fmt.Sprintf("Merge task: %s", task.Title), task.WorktreeBranch}
	}

	mergeCmd := exec.Command("git", mergeArgs...)
	mergeCmd.Dir = repoDir
	mergeOut, mergeErr := mergeCmd.CombinedOutput()

	if mergeErr != nil {
		// Check if it's a conflict
		conflictFiles := detectConflicts(repoDir)
		if len(conflictFiles) > 0 {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusConflict)
			return &MergeResult{
				ConflictFiles: conflictFiles,
				ErrorMessage:  string(mergeOut),
			}, nil
		}
		mergeErrMsg := strings.TrimSpace(string(mergeOut))
		if mergeType == "ff" && strings.Contains(strings.ToLower(mergeErrMsg), "not possible to fast-forward") {
			mergeErrMsg = fmt.Sprintf("fast-forward merge requires branch update from %s. The app attempted to auto-rebase when possible. If this task has no worktree path or still failed, open the task worktree and rebase onto %s, then retry", targetBranch, targetBranch)
		}
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: mergeErrMsg}, fmt.Errorf("merge failed: %w", mergeErr)
	}

	// For squash merge, commit only the paths introduced by the squash result.
	// This allows unrelated user-staged changes to remain staged without being
	// accidentally included in the app-created squash commit.
	if mergeType == "squash" {
		squashPaths, pathErr := SquashMergePaths(repoDir, stagedBeforeSquash)
		if pathErr != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			return &MergeResult{ErrorMessage: fmt.Sprintf("checking squash merge paths: %s", pathErr.Error())}, fmt.Errorf("checking squash merge paths: %w", pathErr)
		}
		commitArgs := append([]string{"commit", "-m", fmt.Sprintf("Squash merge task: %s", task.Title), "--only", "--"}, squashPaths...)
		commitCmd := exec.Command("git", commitArgs...)
		commitCmd.Dir = repoDir
		if out, err := commitCmd.CombinedOutput(); err != nil {
			commitErrMsg := strings.TrimSpace(string(out))
			if commitErrMsg == "" {
				commitErrMsg = err.Error()
			}
			if resetErr := ResetSquashMergeChanges(repoDir, squashPaths); resetErr != nil {
				commitErrMsg = fmt.Sprintf("%s; additionally failed to restore squash merge changes: %v", commitErrMsg, resetErr)
			}
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			return &MergeResult{ErrorMessage: fmt.Sprintf("squash commit failed: %s", commitErrMsg)}, fmt.Errorf("squash commit failed: %w", err)
		}
	}

	// Get merge commit hash
	hashCmd := exec.Command("git", "rev-parse", "HEAD")
	hashCmd.Dir = repoDir
	hashOut, _ := hashCmd.Output()

	_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusMerged)

	return &MergeResult{
		Success:     true,
		MergeCommit: strings.TrimSpace(string(hashOut)),
	}, nil
}

func (ws *WorktreeService) fastForwardTaskWorktreeToTarget(ctx context.Context, task *models.Task, repoDir string, targetBranch string) (*MergeResult, error) {
	currentBranchOut, err := gitOutput(task.WorktreePath, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		msg := "task worktree must be on the expected task branch before fast-forward merge"
		return &MergeResult{ErrorMessage: msg}, fmt.Errorf("%s", msg)
	}
	currentBranch := strings.TrimSpace(string(currentBranchOut))
	if currentBranch != task.WorktreeBranch {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		msg := fmt.Sprintf("task worktree is on branch %q, expected %q", currentBranch, task.WorktreeBranch)
		return &MergeResult{ErrorMessage: msg}, fmt.Errorf("%s", msg)
	}

	statusOut, err := GitStatusPorcelain(task.WorktreePath)
	if err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: fmt.Sprintf("checking task worktree status: %s", err.Error())}, fmt.Errorf("checking task worktree status: %w", err)
	}
	if strings.TrimSpace(statusOut) != "" {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		msg := "task worktree has uncommitted changes; commit or discard them before fast-forward merge"
		return &MergeResult{ErrorMessage: msg}, fmt.Errorf("%s", msg)
	}

	rebaseOut, rebaseErr := gitOutput(task.WorktreePath, "rebase", targetBranch)
	if rebaseErr != nil {
		conflictFiles := detectConflicts(task.WorktreePath)
		if len(conflictFiles) > 0 {
			_ = AbortRebase(task.WorktreePath)
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusConflict)
			return &MergeResult{
				Success:       false,
				ConflictFiles: conflictFiles,
				ErrorMessage:  fmt.Sprintf("Local fast-forward merge requires updating branch from %s. Auto-rebase encountered conflicts; rebase was aborted. Resolve conflicts in worktree and retry merge.", targetBranch),
			}, nil
		}
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: strings.TrimSpace(string(rebaseOut))}, fmt.Errorf("auto-rebase task branch onto %s failed: %w", targetBranch, rebaseErr)
	}

	oldTargetOut, err := gitOutput(task.WorktreePath, "rev-parse", "refs/heads/"+targetBranch)
	if err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: fmt.Sprintf("resolving target branch: %s", strings.TrimSpace(string(oldTargetOut)))}, fmt.Errorf("resolving target branch %s: %w", targetBranch, err)
	}
	oldTarget := strings.TrimSpace(string(oldTargetOut))

	newTaskOut, err := gitOutput(task.WorktreePath, "rev-parse", "HEAD")
	if err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: fmt.Sprintf("resolving task HEAD: %s", strings.TrimSpace(string(newTaskOut)))}, fmt.Errorf("resolving task HEAD: %w", err)
	}
	newTask := strings.TrimSpace(string(newTaskOut))

	if out, err := gitOutput(task.WorktreePath, "merge-base", "--is-ancestor", oldTarget, newTask); err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		msg := fmt.Sprintf("fast-forward merge requires %s to be an ancestor of task HEAD", targetBranch)
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			msg = fmt.Sprintf("%s: %s", msg, trimmed)
		}
		return &MergeResult{ErrorMessage: msg}, fmt.Errorf("%s", msg)
	}

	if targetWorktree, err := findWorktreeForBranch(repoDir, targetBranch); err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: err.Error()}, fmt.Errorf("finding target worktree: %w", err)
	} else if targetWorktree != "" {
		mergeOut, mergeErr := gitOutput(targetWorktree, "merge", "--ff-only", "refs/heads/"+task.WorktreeBranch)
		if mergeErr != nil {
			mergeErrMsg := strings.TrimSpace(string(mergeOut))
			if mergeErrMsg == "" {
				mergeErrMsg = mergeErr.Error()
			}
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			return &MergeResult{ErrorMessage: mergeErrMsg}, fmt.Errorf("fast-forward merge in target worktree failed: %w", mergeErr)
		}

		mergedHeadOut, err := gitOutput(targetWorktree, "rev-parse", "HEAD")
		if err != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			return &MergeResult{ErrorMessage: fmt.Sprintf("resolving merged target HEAD: %s", strings.TrimSpace(string(mergedHeadOut)))}, fmt.Errorf("resolving merged target HEAD: %w", err)
		}
		mergedHead := strings.TrimSpace(string(mergedHeadOut))
		if mergedHead != newTask {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
			msg := fmt.Sprintf("fast-forward merge ended at %s, expected rebased task HEAD %s", mergedHead, newTask)
			return &MergeResult{ErrorMessage: msg}, fmt.Errorf("%s", msg)
		}
	} else if out, err := gitOutput(task.WorktreePath, "update-ref", "refs/heads/"+targetBranch, newTask, oldTarget); err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return &MergeResult{ErrorMessage: msg}, fmt.Errorf("updating target branch ref: %w", err)
	}

	_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusMerged)
	return &MergeResult{Success: true, MergeCommit: newTask}, nil
}

func findWorktreeForBranch(repoDir string, branch string) (string, error) {
	worktrees, err := ListGitWorktrees(repoDir)
	if err != nil {
		return "", err
	}
	expectedRef := "refs/heads/" + branch
	for _, worktree := range worktrees {
		if worktree.Branch != branch {
			continue
		}
		out, err := gitOutput(worktree.Path, "symbolic-ref", "--quiet", "HEAD")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(out)) == expectedRef {
			return worktree.Path, nil
		}
	}
	return "", nil
}

func AbortRebase(repoDir string) error {
	cmd := exec.Command("git", "rebase", "--abort")
	cmd.Dir = repoDir
	_, err := cmd.CombinedOutput()
	return err
}

func gitOutput(repoDir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	return cmd.CombinedOutput()
}

func StagedPaths(repoDir string) (map[string]bool, error) {
	out, err := gitOutput(repoDir, "diff", "--name-only", "--cached")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	paths := make(map[string]bool)
	for _, path := range strings.Fields(string(out)) {
		paths[path] = true
	}
	return paths, nil
}

func SquashMergePaths(repoDir string, stagedBefore map[string]bool) ([]string, error) {
	stagedAfter, err := StagedPaths(repoDir)
	if err != nil {
		return nil, err
	}
	var changed []string
	for path := range stagedAfter {
		if stagedBefore[path] {
			continue
		}
		changed = append(changed, path)
	}
	if len(changed) == 0 {
		return nil, fmt.Errorf("squash merge produced no staged changes")
	}
	return changed, nil
}

// ResetSquashMergeChanges restores only files changed by a failed squash merge
// attempt. Unlike `git reset --hard`, this does not reset the whole target
// checkout or touch staged user changes that existed before the squash attempt.
func ResetSquashMergeChanges(repoDir string, squashPaths []string) error {
	if len(squashPaths) == 0 {
		return nil
	}

	args := append([]string{"restore", "--staged", "--worktree", "--source=HEAD", "--"}, squashPaths...)
	if out, err := gitOutput(repoDir, args...); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ActiveConflictFiles returns files with active merge conflicts in the given repository.
func ActiveConflictFiles(repoDir string) []string {
	return detectConflicts(repoDir)
}

// detectConflicts returns a list of files with merge conflicts.
func detectConflicts(repoDir string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// AbortMerge aborts an in-progress merge.
func AbortMerge(repoDir string) error {
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = repoDir
	_, err := cmd.CombinedOutput()
	return err
}

// ResolveConflictsWithAI uses the LLM service to resolve merge conflicts.
func (ws *WorktreeService) ResolveConflictsWithAI(ctx context.Context, task *models.Task, repoDir string) (*MergeResult, error) {
	if ws.llmSvc == nil {
		return nil, fmt.Errorf("LLM service not available for conflict resolution")
	}

	conflictFiles := detectConflicts(repoDir)
	if len(conflictFiles) == 0 {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusPending)
		return &MergeResult{ErrorMessage: "no active merge conflicts found"}, fmt.Errorf("no active merge conflicts found")
	}

	// Build a prompt describing the conflicts
	var conflictDetails strings.Builder
	conflictDetails.WriteString("Please resolve the following merge conflicts. For each file, output the resolved content.\n\n")

	for _, file := range conflictFiles {
		content, err := os.ReadFile(filepath.Join(repoDir, file))
		if err != nil {
			continue
		}
		conflictDetails.WriteString(fmt.Sprintf("=== File: %s ===\n%s\n\n", file, string(content)))
	}

	conflictDetails.WriteString("\nResolve each conflict by choosing the appropriate changes or combining them intelligently. ")
	conflictDetails.WriteString("After resolving, stage the files with `git add` and commit with a descriptive message.")

	// Execute resolution via the agent in the repo directory
	agent, err := ws.llmSvc.getDefaultAgentForTask(ctx, task.ProjectID)
	if err != nil || agent == nil {
		return nil, fmt.Errorf("no agent available for conflict resolution")
	}

	_, _, _, err = ws.llmSvc.callLLM(ctx, conflictDetails.String(), nil, *agent, "", repoDir, "")
	if err != nil {
		return nil, fmt.Errorf("AI conflict resolution failed: %w", err)
	}

	// Check if conflicts are resolved
	remainingConflicts := detectConflicts(repoDir)
	if len(remainingConflicts) > 0 {
		return &MergeResult{
			ConflictFiles: remainingConflicts,
			ErrorMessage:  "AI could not resolve all conflicts",
		}, nil
	}

	// Commit the resolution. Both staging and committing must succeed before the
	// task can be considered merged.
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: fmt.Sprintf("staging resolved conflicts failed: %s", strings.TrimSpace(string(out)))}, fmt.Errorf("staging resolved conflicts: %w", err)
	}

	commitCmd := exec.Command("git", "commit", "--no-edit")
	commitCmd.Dir = repoDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		commitErrMsg := strings.TrimSpace(string(out))
		if commitErrMsg == "" {
			commitErrMsg = err.Error()
		}
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		return &MergeResult{ErrorMessage: fmt.Sprintf("committing resolved merge failed: %s", commitErrMsg)}, fmt.Errorf("committing resolved merge: %w", err)
	}

	_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusMerged)

	hashCmd := exec.Command("git", "rev-parse", "HEAD")
	hashCmd.Dir = repoDir
	hashOut, _ := hashCmd.Output()

	return &MergeResult{
		Success:     true,
		MergeCommit: strings.TrimSpace(string(hashOut)),
	}, nil
}

// CleanupWorktree removes the worktree and optionally deletes the branch.
func (ws *WorktreeService) CleanupWorktree(ctx context.Context, task *models.Task, repoDir string, deleteBranch bool) error {
	if task.WorktreePath == "" {
		return nil
	}

	// Check for uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = task.WorktreePath
	out, err := statusCmd.Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("worktree has uncommitted changes; commit or discard them first")
	}

	// Remove worktree
	removeCmd := exec.Command("git", "worktree", "remove", task.WorktreePath, "--force")
	removeCmd.Dir = repoDir
	if out, err := removeCmd.CombinedOutput(); err != nil {
		applog.Infof("[worktree] error removing worktree: %s", string(out))
		// Try manual removal as fallback
		os.RemoveAll(task.WorktreePath)
		// Prune worktree list
		pruneCmd := exec.Command("git", "worktree", "prune")
		pruneCmd.Dir = repoDir
		pruneCmd.Run()
	}

	// Delete branch if requested, but guard against active descendants
	if deleteBranch && task.WorktreeBranch != "" {
		// Check if any descendants depend on this branch (non-terminal children)
		hasActiveDesc := false
		if ws.taskRepo != nil {
			active, descErr := ws.taskRepo.HasNonTerminalDescendants(ctx, task.ID)
			if descErr != nil {
				applog.Infof("[worktree] error checking descendants for task %s: %v", task.ID, descErr)
			} else {
				hasActiveDesc = active
			}
		}
		if hasActiveDesc {
			applog.Infof("[worktree] skipping branch deletion for task %s branch %s: has active descendants", task.ID, task.WorktreeBranch)
		} else {
			deleteCmd := exec.Command("git", "branch", "-D", task.WorktreeBranch)
			deleteCmd.Dir = repoDir
			if out, err := deleteCmd.CombinedOutput(); err != nil {
				applog.Infof("[worktree] error deleting branch %s: %s", task.WorktreeBranch, string(out))
			}
		}
	}

	// Clear worktree info from task
	if err := ws.taskRepo.ClearWorktreeInfo(ctx, task.ID); err != nil {
		applog.Infof("[worktree] error clearing worktree info: %v", err)
	}

	applog.Infof("[worktree] cleaned up worktree for task %s", task.ID)
	return nil
}

// GetWorktreeDiff returns the current tree diff between the target branch and
// worktree branch. Use a direct two-dot/tree comparison rather than a
// merge-base comparison so the Changes UI reflects the branch's net difference
// from the current target branch after target advances, rebases, or squash
// merges.
func GetWorktreeDiff(repoDir string, branchName string, targetBranch string) string {
	if branchName == "" || targetBranch == "" {
		return ""
	}
	cmd := exec.Command("git", "diff", targetBranch, branchName)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		applog.Infof("[worktree] error getting worktree diff: %v", err)
		return ""
	}
	return string(out)
}

// GetWorktreeDiffWithUncommitted returns the combined diff of committed branch changes
// plus any uncommitted changes in the worktree working directory. This provides a
// real-time view of all changes without needing to auto-commit during execution.
func GetWorktreeDiffWithUncommitted(repoDir string, branchName string, targetBranch string, worktreePath string) string {
	// Get committed branch diff
	committedDiff := GetWorktreeDiff(repoDir, branchName, targetBranch)

	if worktreePath == "" {
		return committedDiff
	}

	// Capture uncommitted changes in the worktree (staged + unstaged + untracked)
	uncommittedDiff := captureWorktreeUncommitted(worktreePath)

	if uncommittedDiff == "" {
		return committedDiff
	}
	if committedDiff == "" {
		return uncommittedDiff
	}
	return committedDiff + "\n" + uncommittedDiff
}

// captureWorktreeUncommitted captures all uncommitted changes (staged, unstaged,
// and untracked files) in a worktree directory as a unified diff.
func captureWorktreeUncommitted(worktreePath string) string {
	if worktreePath == "" {
		return ""
	}

	// git diff HEAD captures staged + unstaged changes
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	result := string(out)

	// Also capture untracked files
	untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = worktreePath
	untrackedOut, _ := untrackedCmd.Output()
	if len(untrackedOut) > 0 {
		untracked := strings.TrimSpace(string(untrackedOut))
		if untracked != "" {
			for _, f := range strings.Split(untracked, "\n") {
				f = strings.TrimSpace(f)
				if f == "" {
					continue
				}
				fileDiff := generateNewFileDiffForWorktree(worktreePath, f)
				if fileDiff != "" {
					result += fileDiff
				}
			}
		}
	}

	return result
}

// generateNewFileDiffForWorktree creates a unified diff for a new (untracked) file.
func generateNewFileDiffForWorktree(worktreePath, relPath string) string {
	absPath := filepath.Join(worktreePath, relPath)
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return ""
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}

	// Check for binary
	checkLen := len(content)
	if checkLen > 8000 {
		checkLen = 8000
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return fmt.Sprintf("\ndiff --git a/%s b/%s\nnew file mode 100644\nBinary files /dev/null and b/%s differ\n", relPath, relPath, relPath)
		}
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return fmt.Sprintf("\ndiff --git a/%s b/%s\nnew file mode 100644\n", relPath, relPath)
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("\ndiff --git a/%s b/%s\n", relPath, relPath))
	buf.WriteString("new file mode 100644\n")
	buf.WriteString("--- /dev/null\n")
	buf.WriteString(fmt.Sprintf("+++ b/%s\n", relPath))
	buf.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
	for _, l := range lines {
		buf.WriteString("+" + l + "\n")
	}
	return buf.String()
}

// GetWorktreeFileStats returns a summary of changed files in the worktree branch.
type WorktreeFileStat struct {
	Path   string
	Status string // "added", "modified", "deleted"
}

func GetWorktreeFileStats(repoDir string, branchName string, targetBranch string) []WorktreeFileStat {
	if branchName == "" || targetBranch == "" {
		return nil
	}
	cmd := exec.Command("git", "diff", "--name-status", targetBranch, branchName)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseWorktreeFileStats(out)
}

func GetWorktreeFileStatsWithUncommitted(repoDir string, branchName string, targetBranch string, worktreePath string) []WorktreeFileStat {
	stats := GetWorktreeFileStats(repoDir, branchName, targetBranch)
	if worktreePath == "" {
		return stats
	}

	cmd := exec.Command("git", "status", "--short", "--untracked-files=all")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return stats
	}
	return mergeWorktreeFileStats(stats, parseGitStatusFileStats(out))
}

func parseWorktreeFileStats(out []byte) []WorktreeFileStat {
	var stats []WorktreeFileStat
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		stats = append(stats, WorktreeFileStat{Path: parts[1], Status: gitStatusToWorktreeFileStatus(parts[0])})
	}
	return stats
}

func parseGitStatusFileStats(out []byte) []WorktreeFileStat {
	var stats []WorktreeFileStat
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if code == "" || path == "" {
			continue
		}
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = strings.TrimSpace(path[idx+4:])
		}
		stats = append(stats, WorktreeFileStat{Path: path, Status: gitStatusToWorktreeFileStatus(code)})
	}
	return stats
}

func gitStatusToWorktreeFileStatus(code string) string {
	if strings.Contains(code, "D") {
		return "deleted"
	}
	if strings.Contains(code, "A") || strings.Contains(code, "?") {
		return "added"
	}
	return "modified"
}

func mergeWorktreeFileStats(base []WorktreeFileStat, overlay []WorktreeFileStat) []WorktreeFileStat {
	if len(overlay) == 0 {
		return base
	}
	merged := make([]WorktreeFileStat, 0, len(base)+len(overlay))
	index := make(map[string]int, len(base)+len(overlay))
	for _, stat := range base {
		index[stat.Path] = len(merged)
		merged = append(merged, stat)
	}
	for _, stat := range overlay {
		if i, ok := index[stat.Path]; ok {
			merged[i] = stat
			continue
		}
		index[stat.Path] = len(merged)
		merged = append(merged, stat)
	}
	return merged
}

// getGlobalMergeTarget returns the global default merge target branch.
func (ws *WorktreeService) getGlobalMergeTarget(ctx context.Context) string {
	if ws.settingsRepo == nil {
		return ""
	}
	val, err := ws.settingsRepo.Get(ctx, "worktree_merge_target")
	if err != nil || val == "" {
		return ""
	}
	return val
}

// GetGlobalAutoMerge returns the global auto-merge default setting.
func (ws *WorktreeService) GetGlobalAutoMerge(ctx context.Context) bool {
	if ws.settingsRepo == nil {
		return false
	}
	val, err := ws.settingsRepo.Get(ctx, "worktree_auto_merge")
	if err != nil {
		return false
	}
	return val == "true"
}

// GetCleanupPolicy returns the worktree cleanup policy.
func (ws *WorktreeService) GetCleanupPolicy(ctx context.Context) string {
	if ws.settingsRepo == nil {
		return "after_merge"
	}
	val, err := ws.settingsRepo.Get(ctx, "worktree_cleanup")
	if err != nil || val == "" {
		return "after_merge"
	}
	return val
}

// IsBranchMerged checks if a branch has been fully merged into the target branch.
// Returns true if the branch is merged (no unique commits), false otherwise.
//
// NOTE: A missing branch is treated as merged here so cleanup logic can
// reclaim worktrees whose branches were manually deleted post-merge. Callers
// that need to distinguish "branch is provably merged in git right now" from
// "branch is missing" should use IsBranchTipMergedInto instead.
func IsBranchMerged(repoDir string, branchName string, targetBranch string) bool {
	if branchName == "" || targetBranch == "" {
		return false
	}

	// Check if branch exists
	checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = repoDir
	if err := checkCmd.Run(); err != nil {
		// Branch doesn't exist (might have been manually deleted)
		return true
	}

	// Use git merge-base --is-ancestor to check if branch is merged
	// This checks if the branch tip is reachable from target branch
	cmd := exec.Command("git", "merge-base", "--is-ancestor", branchName, targetBranch)
	cmd.Dir = repoDir
	err := cmd.Run()

	// Exit code 0 means ancestor (merged), non-zero means not merged
	return err == nil
}

// IsBranchTipMergedInto reports whether `branchName` exists in the repo and
// its tip commit is an ancestor of `targetBranch` (i.e. the branch has been
// merged into the target). Unlike IsBranchMerged, a missing branch returns
// false so UI reconciliation does not falsely hide merge actions for tasks
// whose worktree branch was never actually created.
func IsBranchTipMergedInto(repoDir string, branchName string, targetBranch string) bool {
	if branchName == "" || targetBranch == "" {
		return false
	}
	if !IsGitRepo(repoDir) {
		return false
	}

	// Branch must exist locally to be considered merged-in-git.
	if err := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", branchName).Run(); err != nil {
		return false
	}
	// Target branch must also exist; otherwise we can't compare ancestry.
	if err := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", targetBranch).Run(); err != nil {
		return false
	}

	cmd := exec.Command("git", "-C", repoDir, "merge-base", "--is-ancestor", branchName, targetBranch)
	return cmd.Run() == nil
}

// HandlePostExecution handles worktree operations after task execution completes.
// Called by the LLM service after a task finishes successfully.
func (ws *WorktreeService) HandlePostExecution(ctx context.Context, task *models.Task, repoDir string) {
	if task.WorktreePath == "" || task.WorktreeBranch == "" {
		return
	}

	// Commit any changes in the worktree. If this fails, do not mark the task
	// branch as ready/pending; otherwise the Changes tab can offer a branch merge
	// for a branch that does not actually contain the provider's file edits.
	msg := fmt.Sprintf("Task completed: %s", task.Title)
	if err := CommitWorktreeChanges(task.WorktreePath, msg); err != nil {
		applog.Infof("[worktree] error committing changes for task %s: %v", task.ID, err)
		if ws.taskRepo != nil {
			_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusFailed)
		}
		return
	}

	// Auto-merge if enabled
	if task.AutoMerge {
		applog.Infof("[worktree] auto-merging task %s branch %s -> %s", task.ID, task.WorktreeBranch, task.MergeTargetBranch)
		result, err := ws.MergeBranch(ctx, task, repoDir, "merge")
		if err != nil {
			applog.Infof("[worktree] auto-merge failed for task %s: %v", task.ID, err)
			return
		}
		if !result.Success && len(result.ConflictFiles) > 0 {
			applog.Infof("[worktree] auto-merge has conflicts for task %s, attempting AI resolution", task.ID)
			aiResult, aiErr := ws.ResolveConflictsWithAI(ctx, task, repoDir)
			if aiErr != nil || (aiResult != nil && !aiResult.Success) {
				applog.Infof("[worktree] AI conflict resolution failed for task %s, aborting merge", task.ID)
				AbortMerge(repoDir)
				_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusConflict)
				return
			}
		}

		// Cleanup after successful merge if policy says so
		policy := ws.GetCleanupPolicy(ctx)
		if policy == "after_merge" {
			if cleanErr := ws.CleanupWorktree(ctx, task, repoDir, true); cleanErr != nil {
				applog.Infof("[worktree] cleanup after merge failed: %v", cleanErr)
			}
		}
	} else {
		// Set merge status to pending for manual merge
		_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusPending)
	}
}

// CleanupMergedWorktrees scans all tasks with worktrees and cleans up those
// whose branches have been merged to their target branches.
// Called periodically by the scheduler to detect manual merges.
func (ws *WorktreeService) CleanupMergedWorktrees(ctx context.Context) error {
	// Get cleanup policy
	policy := ws.GetCleanupPolicy(ctx)
	if policy != "after_merge" {
		// Don't auto-cleanup if policy is "keep" or "manual"
		return nil
	}

	// Get all tasks with worktrees
	tasks, err := ws.taskRepo.ListWithWorktrees(ctx)
	if err != nil {
		return fmt.Errorf("listing tasks with worktrees: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	applog.Infof("[worktree] cleanup scan: checking %d tasks with worktrees", len(tasks))

	cleanedCount := 0
	for _, task := range tasks {
		// Skip tasks that are currently running or pending — their worktrees are in use
		if task.Status == models.StatusRunning || task.Status == models.StatusPending || task.Status == models.StatusQueued {
			continue
		}

		// Get the project to determine the repo directory
		project, err := ws.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil || project == nil {
			applog.Infof("[worktree] cleanup: skipping task %s (project not found)", task.ID)
			continue
		}

		repoDir := project.RepoPath
		if repoDir == "" || !IsGitRepo(repoDir) {
			applog.Infof("[worktree] cleanup: skipping task %s (not a git repo)", task.ID)
			continue
		}

		targetBranch := task.MergeTargetBranch
		if targetBranch == "" {
			targetBranch = ws.getGlobalMergeTarget(ctx)
		}
		if targetBranch == "" {
			targetBranch = GetDefaultBranch(repoDir)
		}

		// Check if branch has been merged
		if IsBranchMerged(repoDir, task.WorktreeBranch, targetBranch) {
			applog.Infof("[worktree] cleanup: task %s branch %s is merged to %s, cleaning up",
				task.ID, task.WorktreeBranch, targetBranch)

			// Update merge status to merged if not already
			if task.MergeStatus != models.MergeStatusMerged {
				_ = ws.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusMerged)
			}

			// Cleanup the worktree and delete the branch
			if err := ws.CleanupWorktree(ctx, &task, repoDir, true); err != nil {
				applog.Infof("[worktree] cleanup: failed to cleanup task %s: %v", task.ID, err)
			} else {
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		applog.Infof("[worktree] cleanup scan: cleaned up %d merged worktrees", cleanedCount)
	}

	// Also cleanup orphaned worktrees (worktrees with no corresponding task)
	orphanedCount, err := ws.CleanupOrphanedWorktrees(ctx)
	if err != nil {
		applog.Infof("[worktree] cleanup: failed to cleanup orphaned worktrees: %v", err)
	} else if orphanedCount > 0 {
		applog.Infof("[worktree] cleanup scan: cleaned up %d orphaned worktrees", orphanedCount)
	}

	return nil
}

// CleanupOrphanedWorktrees removes worktrees that exist on disk but have no corresponding task in the database.
// This can happen when tasks are deleted but their worktrees weren't cleaned up.
// Returns the number of orphaned worktrees cleaned up.
func (ws *WorktreeService) CleanupOrphanedWorktrees(ctx context.Context) (int, error) {
	// Get cleanup policy
	policy := ws.GetCleanupPolicy(ctx)
	if policy == "keep" {
		// Don't auto-cleanup if policy is "keep"
		return 0, nil
	}

	// Get all projects to check their worktrees
	projects, err := ws.projectRepo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing projects: %w", err)
	}

	cleanedCount := 0
	for _, project := range projects {
		if project.RepoPath == "" || !IsGitRepo(project.RepoPath) {
			continue
		}

		// List all git worktrees for this repo
		worktrees, err := ListGitWorktrees(project.RepoPath)
		if err != nil {
			applog.Infof("[worktree] cleanup: failed to list worktrees for project %s: %v", project.ID, err)
			continue
		}

		// Get all tasks for this project. We need both:
		// 1) knownPaths (worktree path already recorded in DB)
		// 2) knownTaskIDs (task exists but may not have worktree_path persisted yet)
		allTasks, err := ws.taskRepo.ListByProject(ctx, project.ID, "")
		if err != nil {
			applog.Infof("[worktree] cleanup: failed to list tasks for project %s: %v", project.ID, err)
			continue
		}

		// Build maps of known paths and known task IDs.
		knownPaths := make(map[string]bool)
		knownTaskIDs := make(map[string]bool)
		for _, task := range allTasks {
			knownTaskIDs[task.ID] = true
			if task.WorktreePath != "" {
				knownPaths[task.WorktreePath] = true
			}
		}

		// Check each worktree to see if it's orphaned
		for _, worktree := range worktrees {
			// Skip the main worktree (the original repo)
			if worktree.IsMain {
				continue
			}

			// Known in DB, not orphaned.
			if knownPaths[worktree.Path] {
				continue
			}

			// Worktree directories follow .worktrees/task_<taskID>. If the task still
			// exists but worktree_path wasn't persisted yet, treat it as in-use.
			if taskID, ok := taskIDFromWorktreePath(worktree.Path); ok && knownTaskIDs[taskID] {
				applog.Infof("[worktree] cleanup: skipping worktree at %s because task %s still exists", worktree.Path, taskID)
				continue
			}

			applog.Infof("[worktree] cleanup: found orphaned worktree at %s (branch: %s)", worktree.Path, worktree.Branch)

			// Try to remove the worktree using git first
			cmd := exec.Command("git", "worktree", "remove", "--force", worktree.Path)
			cmd.Dir = project.RepoPath
			if output, err := cmd.CombinedOutput(); err != nil {
				outputText := string(output)

				// A locked worktree may still be actively initializing. Don't perform
				// manual filesystem deletion in this case; retry on a future cleanup cycle.
				if strings.Contains(outputText, "cannot remove a locked working tree") {
					applog.Infof("[worktree] cleanup: skipping locked orphaned worktree at %s (output: %s)", worktree.Path, outputText)
					continue
				}

				// If git worktree remove fails, try manual cleanup
				applog.Infof("[worktree] cleanup: git worktree remove failed, attempting manual cleanup: %v (output: %s)", err, outputText)

				// Remove the worktree directory manually
				if err := os.RemoveAll(worktree.Path); err != nil {
					applog.Infof("[worktree] cleanup: failed to remove orphaned worktree directory %s: %v", worktree.Path, err)
					continue
				}

				// Prune stale worktree entries
				pruneCmd := exec.Command("git", "worktree", "prune")
				pruneCmd.Dir = project.RepoPath
				_ = pruneCmd.Run() // Ignore errors
			}

			// Delete the branch if it exists
			if worktree.Branch != "" {
				cmd = exec.Command("git", "branch", "-D", worktree.Branch)
				cmd.Dir = project.RepoPath
				_ = cmd.Run() // Ignore errors - branch might already be deleted
			}

			cleanedCount++
		}
	}

	return cleanedCount, nil
}

func taskIDFromWorktreePath(worktreePath string) (string, bool) {
	base := filepath.Base(strings.TrimSpace(worktreePath))
	if !strings.HasPrefix(base, "task_") {
		return "", false
	}
	taskID := strings.TrimPrefix(base, "task_")
	if taskID == "" {
		return "", false
	}
	return taskID, true
}

// WorktreeInfo represents information about a git worktree.
type WorktreeInfo struct {
	Path   string
	Branch string
	IsMain bool
}

// ListGitWorktrees lists all worktrees for a git repository.
func ListGitWorktrees(repoDir string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w (output: %s)", err, string(output))
	}

	// Resolve repoDir symlinks for comparison
	resolvedRepoDir, _ := filepath.EvalSymlinks(repoDir)
	if resolvedRepoDir == "" {
		resolvedRepoDir = repoDir
	}

	var worktrees []WorktreeInfo
	var current WorktreeInfo
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// End of a worktree entry
			if current.Path != "" {
				// Resolve symlinks for comparison
				resolvedPath, _ := filepath.EvalSymlinks(current.Path)
				if resolvedPath == "" {
					resolvedPath = current.Path
				}
				// Mark as main if this is the original repo directory
				if resolvedPath == resolvedRepoDir {
					current.IsMain = true
				}
				worktrees = append(worktrees, current)
				current = WorktreeInfo{}
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			current.Branch = strings.TrimPrefix(line, "branch ")
			// Remove "refs/heads/" prefix
			current.Branch = strings.TrimPrefix(current.Branch, "refs/heads/")
		} else if strings.HasPrefix(line, "HEAD ") && current.Branch == "" {
			// Detached HEAD, not on a branch
			current.Branch = ""
		}
	}

	// Don't forget the last entry if file doesn't end with blank line
	if current.Path != "" {
		// Resolve symlinks for comparison
		resolvedPath, _ := filepath.EvalSymlinks(current.Path)
		if resolvedPath == "" {
			resolvedPath = current.Path
		}
		// Mark as main if this is the original repo directory
		if resolvedPath == resolvedRepoDir {
			current.IsMain = true
		}
		worktrees = append(worktrees, current)
	}

	return worktrees, nil
}
