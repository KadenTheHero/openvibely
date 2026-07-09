package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type fakePRFeedbackProvider struct {
	authenticatedUser *GitHubAuthenticatedUser
	items             []GitHubPullRequestFeedback
	calls             int
}

func (f *fakePRFeedbackProvider) GetAuthenticatedUser(ctx context.Context) (*GitHubAuthenticatedUser, error) {
	if f.authenticatedUser != nil {
		return f.authenticatedUser, nil
	}
	return &GitHubAuthenticatedUser{Login: "openvibely", Source: GitHubAuthModePAT}, nil
}

func (f *fakePRFeedbackProvider) ListPullRequestFeedback(ctx context.Context, repo *GitHubRepoRef, prNumber int) ([]GitHubPullRequestFeedback, error) {
	f.calls++
	return f.items, nil
}

func TestGitHubPRFeedbackForwarderQueuesAuthorizedFeedbackOnce(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	prRepo := repository.NewTaskPullRequestRepo(db)
	feedbackRepo := repository.NewGitHubPRFeedbackRepo(db)
	authRepo := repository.NewGitHubAuthRepo(db)
	threadInputRepo := repository.NewThreadInputRepo(db)

	project := &models.Project{ID: "proj-pr-feedback", Name: "PR Feedback", RepoPath: t.TempDir(), RepoURL: "https://github.com/openvibely/openvibely"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &models.Task{ID: "task-pr-feedback", ProjectID: project.ID, Title: "Implement issue", Category: models.CategoryActive, Status: models.StatusPending}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	for _, login := range []string{"Alice", "openvibely", "ci-bot"} {
		if err := authRepo.UpsertAuthorizedActor(ctx, &models.GitHubAuthorizedActor{GitHubLogin: login, Permission: "triage", AddedBy: "test"}); err != nil {
			t.Fatalf("authorize actor %s: %v", login, err)
		}
	}
	if err := prRepo.Upsert(ctx, &models.TaskPullRequest{TaskID: task.ID, PRNumber: 42, PRURL: "https://github.com/openvibely/openvibely/pull/42", PRState: "open"}); err != nil {
		t.Fatalf("upsert pr: %v", err)
	}
	provider := &fakePRFeedbackProvider{items: []GitHubPullRequestFeedback{
		{Kind: "issue_comment", ID: "100", AuthorLogin: "alice", AuthorType: "User", Body: "Please add tests.", URL: "https://github.com/openvibely/openvibely/pull/42#issuecomment-100", CreatedAt: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)},
		{Kind: "review_comment", ID: "101", AuthorLogin: "mallory", AuthorType: "User", Body: "Unauthorized steer", URL: "https://github.com/openvibely/openvibely/pull/42#discussion_r101", CreatedAt: time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC)},
		{Kind: "issue_comment", ID: "102", AuthorLogin: "openvibely", AuthorType: "User", Body: "Self-authored bot steer", URL: "https://github.com/openvibely/openvibely/pull/42#issuecomment-102", CreatedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)},
		{Kind: "review", ID: "103", AuthorLogin: "ci-bot", AuthorType: "Bot", Body: "Automated review", State: "commented", URL: "https://github.com/openvibely/openvibely/pull/42#pullrequestreview-103", CreatedAt: time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC)},
	}}

	forwarder := NewGitHubPRFeedbackForwarder(provider, prRepo, feedbackRepo, authRepo, threadInputRepo)
	result, err := forwarder.ForwardAuthorizedFeedback(ctx, project.ID, &GitHubRepoRef{Owner: "openvibely", Name: "openvibely", FullName: "openvibely/openvibely"})
	if err != nil {
		t.Fatalf("forward feedback: %v", err)
	}
	if len(result.Forwarded) != 1 || result.SkippedUnauthorized != 1 || result.SkippedSelfOrBot != 2 || result.SkippedDuplicate != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	pending, err := threadInputRepo.ListPendingForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected one queued task message, got %d", len(pending))
	}
	if !strings.Contains(pending[0].Content, "Please add tests.") || !strings.Contains(pending[0].Content, "@alice") {
		t.Fatalf("queued feedback missing content/author: %q", pending[0].Content)
	}

	second, err := forwarder.ForwardAuthorizedFeedback(ctx, project.ID, &GitHubRepoRef{Owner: "openvibely", Name: "openvibely", FullName: "openvibely/openvibely"})
	if err != nil {
		t.Fatalf("forward feedback second time: %v", err)
	}
	if len(second.Forwarded) != 0 || second.SkippedDuplicate != 1 {
		t.Fatalf("expected duplicate skip on second run, got %#v", second)
	}
	pending, err = threadInputRepo.ListPendingForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list pending second: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected no duplicate queued messages, got %d", len(pending))
	}
}
