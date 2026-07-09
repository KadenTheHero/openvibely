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
	resolveRepoFn   func(context.Context, string, string) (*GitHubRepoRef, error)
	defaultBranchFn func(context.Context, *GitHubRepoRef) (string, error)
	publishBranchFn func(context.Context, *GitHubRepoRef, GitHubPublishBranchRequest) error
	findPRFn        func(context.Context, *GitHubRepoRef, string) (*GitHubPullRequest, error)
	createPRFn      func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error)
}

func (f *fakeTaskPullRequestGitHubProvider) ResolveRepo(ctx context.Context, repoURL, repoPath string) (*GitHubRepoRef, error) {
	if f.resolveRepoFn != nil {
		return f.resolveRepoFn(ctx, repoURL, repoPath)
	}
	return &GitHubRepoRef{Owner: "openvibely", Name: "openvibely", FullName: "openvibely/openvibely"}, nil
}

func (f *fakeTaskPullRequestGitHubProvider) DefaultBranch(ctx context.Context, repo *GitHubRepoRef) (string, error) {
	if f.defaultBranchFn != nil {
		return f.defaultBranchFn(ctx, repo)
	}
	return "main", nil
}

func (f *fakeTaskPullRequestGitHubProvider) PublishBranch(ctx context.Context, repo *GitHubRepoRef, req GitHubPublishBranchRequest) error {
	if f.publishBranchFn != nil {
		return f.publishBranchFn(ctx, repo, req)
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
	var publishedReq GitHubPublishBranchRequest
	var createdReq GitHubCreatePullRequestRequest
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		publishBranchFn: func(_ context.Context, _ *GitHubRepoRef, req GitHubPublishBranchRequest) error {
			publishedReq = req
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
	if !result.Created || result.PullRequest.Number != 77 || publishedReq.Branch != task.WorktreeBranch || publishedReq.BaseBranch != "develop" {
		t.Fatalf("unexpected result=%#v publishedReq=%#v", result, publishedReq)
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
	publishCalls := 0
	var publishedReq GitHubPublishBranchRequest
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		publishBranchFn: func(_ context.Context, _ *GitHubRepoRef, req GitHubPublishBranchRequest) error {
			publishCalls++
			publishedReq = req
			return nil
		},
		createPRFn: func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			createCalls++
			return nil, fmt.Errorf("should not create")
		},
	}, prRepo)

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if !result.ReusedExistingRecord || result.PullRequest.Number != 22 || createCalls != 0 || publishCalls != 1 || publishedReq.Branch != task.WorktreeBranch {
		t.Fatalf("expected existing PR reuse with branch publish, result=%#v createCalls=%d publishCalls=%d publishedReq=%#v", result, createCalls, publishCalls, publishedReq)
	}
}

func TestTaskPullRequestServiceOpenForTaskReusesExistingRecordAndPersistsIssueMetadata(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	project := &models.Project{Name: "Existing PR Metadata Project", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ProjectID: project.ID, Title: "Existing PR Metadata", Category: models.CategoryActive, Status: models.StatusCompleted, WorktreeBranch: "task/existing-metadata"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := prRepo.Upsert(ctx, &models.TaskPullRequest{TaskID: task.ID, PRNumber: 23, PRURL: "https://github.com/openvibely/openvibely/pull/23", PRState: "open"}); err != nil {
		t.Fatalf("seed PR record: %v", err)
	}
	createCalls := 0
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		createPRFn: func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			createCalls++
			return nil, fmt.Errorf("should not create")
		},
	}, prRepo)
	issueNumber := 123

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{
		IssueNumber: &issueNumber,
		IssueURL:    "https://github.com/openvibely/openvibely/issues/123",
	})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if !result.ReusedExistingRecord || result.PullRequest.Number != 23 || createCalls != 0 {
		t.Fatalf("expected existing PR reuse, result=%#v createCalls=%d", result, createCalls)
	}
	if result.Record.IssueNumber == nil || *result.Record.IssueNumber != 123 || result.Record.IssueURL == "" {
		t.Fatalf("expected result record issue metadata, got %#v", result.Record)
	}
	persisted, err := prRepo.GetByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByTaskID: %v", err)
	}
	if persisted == nil || persisted.IssueNumber == nil || *persisted.IssueNumber != 123 || persisted.IssueURL != "https://github.com/openvibely/openvibely/issues/123" {
		t.Fatalf("expected persisted issue metadata, got %#v", persisted)
	}
}

func TestTaskPullRequestServiceOpenForTaskClearsStaleIssueURLWhenIssueNumberChanges(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	project := &models.Project{Name: "Existing PR Stale Issue URL Project", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ProjectID: project.ID, Title: "Existing PR Stale Issue URL", Category: models.CategoryActive, Status: models.StatusCompleted, WorktreeBranch: "task/existing-stale-issue-url"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	oldIssueNumber := 123
	if err := prRepo.Upsert(ctx, &models.TaskPullRequest{TaskID: task.ID, PRNumber: 24, PRURL: "https://github.com/openvibely/openvibely/pull/24", PRState: "open", IssueNumber: &oldIssueNumber, IssueURL: "https://github.com/openvibely/openvibely/issues/123"}); err != nil {
		t.Fatalf("seed PR record: %v", err)
	}
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		createPRFn: func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			return nil, fmt.Errorf("should not create")
		},
	}, prRepo)
	newIssueNumber := 456

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{IssueNumber: &newIssueNumber})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if !result.ReusedExistingRecord || result.PullRequest.Number != 24 {
		t.Fatalf("expected existing PR reuse, got %#v", result)
	}
	if result.Record.IssueNumber == nil || *result.Record.IssueNumber != 456 || result.Record.IssueURL != "" {
		t.Fatalf("expected result record with new issue number and cleared URL, got %#v", result.Record)
	}
	persisted, err := prRepo.GetByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByTaskID: %v", err)
	}
	if persisted == nil || persisted.IssueNumber == nil || *persisted.IssueNumber != 456 || persisted.IssueURL != "" {
		t.Fatalf("expected persisted issue number with cleared stale URL, got %#v", persisted)
	}
}

func TestTaskPullRequestServiceOpenForTaskClearsStaleIssueNumberWhenIssueURLChanges(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	project := &models.Project{Name: "Existing PR Stale Issue Number Project", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ProjectID: project.ID, Title: "Existing PR Stale Issue Number", Category: models.CategoryActive, Status: models.StatusCompleted, WorktreeBranch: "task/existing-stale-issue-number"}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	oldIssueNumber := 123
	if err := prRepo.Upsert(ctx, &models.TaskPullRequest{TaskID: task.ID, PRNumber: 25, PRURL: "https://github.com/openvibely/openvibely/pull/25", PRState: "open", IssueNumber: &oldIssueNumber, IssueURL: "https://github.com/openvibely/openvibely/issues/123"}); err != nil {
		t.Fatalf("seed PR record: %v", err)
	}
	svc := NewTaskPullRequestService(&fakeTaskPullRequestGitHubProvider{
		createPRFn: func(context.Context, *GitHubRepoRef, GitHubCreatePullRequestRequest) (*GitHubPullRequest, error) {
			return nil, fmt.Errorf("should not create")
		},
	}, prRepo)

	result, err := svc.OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{IssueURL: "https://github.com/openvibely/openvibely/issues/456"})
	if err != nil {
		t.Fatalf("OpenForTask: %v", err)
	}
	if !result.ReusedExistingRecord || result.PullRequest.Number != 25 {
		t.Fatalf("expected existing PR reuse, got %#v", result)
	}
	if result.Record.IssueNumber != nil || result.Record.IssueURL != "https://github.com/openvibely/openvibely/issues/456" {
		t.Fatalf("expected result record with new issue URL and cleared number, got %#v", result.Record)
	}
	persisted, err := prRepo.GetByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByTaskID: %v", err)
	}
	if persisted == nil || persisted.IssueNumber != nil || persisted.IssueURL != "https://github.com/openvibely/openvibely/issues/456" {
		t.Fatalf("expected persisted issue URL with cleared stale number, got %#v", persisted)
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
