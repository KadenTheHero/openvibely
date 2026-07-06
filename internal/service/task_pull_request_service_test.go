package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type fakeTaskPullRequestGitHubProvider struct {
	resolveRepoFn func(context.Context, string, string) (*GitHubRepoRef, error)
	pushBranchFn  func(context.Context, string, string, string, *GitHubRepoRef) error
	findPRFn      func(context.Context, *GitHubRepoRef, string) (*GitHubPullRequest, error)
	createPRFn    func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error)
}

func (f *fakeTaskPullRequestGitHubProvider) ResolveRepo(ctx context.Context, repoURL, repoPath string) (*GitHubRepoRef, error) {
	if f.resolveRepoFn != nil {
		return f.resolveRepoFn(ctx, repoURL, repoPath)
	}
	return &GitHubRepoRef{Owner: "openvibely", Name: "openvibely", FullName: "openvibely/openvibely"}, nil
}

func (f *fakeTaskPullRequestGitHubProvider) PushBranch(ctx context.Context, repoPath, worktreePath, branch string, repo *GitHubRepoRef) error {
	if f.pushBranchFn != nil {
		return f.pushBranchFn(ctx, repoPath, worktreePath, branch, repo)
	}
	return nil
}

func (f *fakeTaskPullRequestGitHubProvider) FindPullRequestByBranch(ctx context.Context, repo *GitHubRepoRef, branch string) (*GitHubPullRequest, error) {
	if f.findPRFn != nil {
		return f.findPRFn(ctx, repo, branch)
	}
	return nil, nil
}

func (f *fakeTaskPullRequestGitHubProvider) CreatePullRequest(ctx context.Context, repo *GitHubRepoRef, req GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
	if f.createPRFn != nil {
		return f.createPRFn(ctx, repo, req)
	}
	return &GitHubPullRequest{Number: 42, URL: "https://github.com/openvibely/openvibely/pull/42", State: "open"}, nil
}

func TestTaskPullRequestServiceOpenForTaskCreatesAndPersistsPR(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	var pushedBranch string
	var createdReq GitHubCreatePullRequestRequest
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		pushBranchFn: func(_ context.Context, repoPath, worktreePath, branch string, repo *GitHubRepoRef) error {
			pushedBranch = branch
			return nil
		},
		createPRFn: func(_ context.Context, _ *GitHubRepoRef, req GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			createdReq = req
			return &GitHubPullRequest{Number: 77, URL: "https://github.com/openvibely/openvibely/pull/77", State: "open"}, nil
		},
	}, prRepo)
	project := &models.Project{Name: "PR Service Project", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ProjectID: project.ID, Title: "Implement runtime PR", Category: models.CategoryActive, Status: models.StatusCompleted, WorktreeBranch: "task/runtime-pr", MergeTargetBranch: "develop"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	issueNumber := 99

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{
		Title:       "Custom PR",
		Body:        "Custom body",
		Draft:       true,
		IssueNumber: &issueNumber,
		IssueURL:    "https://github.com/openvibely/openvibely/issues/99",
	})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if !result.Created || result.PullRequest.Number != 77 || pushedBranch != task.WorktreeBranch {
		t.Fatalf("unexpected result=%#v pushedBranch=%q", result, pushedBranch)
	}
	if createdReq.Title != "Custom PR" || createdReq.Body != "Custom body" || createdReq.Head != task.WorktreeBranch || createdReq.Base != "develop" || !createdReq.Draft {
		t.Fatalf("unexpected create request: %#v", createdReq)
	}
	record, err := prRepo.GetByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByTaskID: %v", err)
	}
	if record == nil || record.PRNumber != 77 || record.IssueNumber == nil || *record.IssueNumber != 99 || record.IssueURL == "" {
		t.Fatalf("unexpected persisted PR record: %#v", record)
	}
}

func TestTaskPullRequestServiceOpenForTaskReusesExistingRecord(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	project := &models.Project{Name: "Existing PR Project", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ProjectID: project.ID, Title: "Existing PR", Category: models.CategoryActive, Status: models.StatusCompleted, WorktreeBranch: "task/existing"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := prRepo.Upsert(ctx, &models.TaskPullRequest{TaskID: task.ID, PRNumber: 22, PRURL: "https://github.com/openvibely/openvibely/pull/22", PRState: "open"}); err != nil {
		t.Fatalf("seed PR record: %v", err)
	}
	createCalls := 0
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		createPRFn: func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			createCalls++
			return nil, fmt.Errorf("should not create")
		},
	}, prRepo)

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if !result.ReusedExistingRecord || result.PullRequest.Number != 22 || createCalls != 0 {
		t.Fatalf("expected existing PR reuse, result=%#v createCalls=%d", result, createCalls)
	}
}

func TestTaskPullRequestServiceOpenForTaskRecoversAlreadyExistsByFindingPR(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	project := &models.Project{Name: "Recovered PR Project", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ProjectID: project.ID, Title: "Recovered PR", Category: models.CategoryActive, Status: models.StatusCompleted, WorktreeBranch: "task/reused"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	findCalls := 0
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		findPRFn: func(_ context.Context, _ *GitHubRepoRef, _ string) (*GitHubPullRequest, error) {
			findCalls++
			if findCalls == 1 {
				return nil, nil
			}
			return &GitHubPullRequest{Number: 88, URL: "https://github.com/openvibely/openvibely/pull/88", State: "open"}, nil
		},
		createPRFn: func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			return nil, fmt.Errorf("Validation Failed: pull request already exists for openvibely:task/reused")
		},
	}, prRepo)

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if result.Created || !result.ReusedRemote || result.PullRequest.Number != 88 || findCalls != 2 {
		t.Fatalf("expected remote PR recovery, result=%#v findCalls=%d", result, findCalls)
	}
	record, err := prRepo.GetByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByTaskID: %v", err)
	}
	if record == nil || record.PRNumber != 88 {
		t.Fatalf("expected recovered PR record, got %#v", record)
	}
}

func TestTaskPullRequestServiceOpenForTaskRequiresWorktreeBranch(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{}, repository.NewTaskPullRequestRepo(db))
	project := &models.Project{ID: "project-1", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	task := &models.Task{ID: "task-1", ProjectID: project.ID, Title: "No Branch"}

	_, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{})
	if err == nil || !strings.Contains(err.Error(), "no worktree branch") {
		t.Fatalf("expected missing branch error, got %v", err)
	}
}
