package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

type GitHubPRFeedbackProvider interface {
	ListPullRequestFeedback(ctx context.Context, repo *GitHubRepoRef, prNumber int) ([]GitHubPullRequestFeedback, error)
}

type GitHubPRFeedbackForwarder struct {
	github          GitHubPRFeedbackProvider
	prRepo          *repository.TaskPullRequestRepo
	feedbackRepo    *repository.GitHubPRFeedbackRepo
	authRepo        *repository.GitHubAuthRepo
	threadInputRepo *repository.ThreadInputRepo
}

type GitHubPRFeedbackForwardResult struct {
	OK                  bool                              `json:"ok"`
	ScannedPullRequests int                               `json:"scanned_pull_requests"`
	Forwarded           []GitHubPRFeedbackForwardedResult `json:"forwarded"`
	SkippedUnauthorized int                               `json:"skipped_unauthorized"`
	SkippedDuplicate    int                               `json:"skipped_duplicate"`
	SkippedEmpty        int                               `json:"skipped_empty"`
}

type GitHubPRFeedbackForwardedResult struct {
	TaskID            string `json:"task_id"`
	PullRequestNumber int    `json:"pull_request_number"`
	FeedbackKind      string `json:"feedback_kind"`
	GitHubID          string `json:"github_id"`
	AuthorLogin       string `json:"author_login"`
	QueuedMessageID   string `json:"queued_message_id"`
	URL               string `json:"url"`
}

func NewGitHubPRFeedbackForwarder(github GitHubPRFeedbackProvider, prRepo *repository.TaskPullRequestRepo, feedbackRepo *repository.GitHubPRFeedbackRepo, authRepo *repository.GitHubAuthRepo, threadInputRepo *repository.ThreadInputRepo) *GitHubPRFeedbackForwarder {
	return &GitHubPRFeedbackForwarder{github: github, prRepo: prRepo, feedbackRepo: feedbackRepo, authRepo: authRepo, threadInputRepo: threadInputRepo}
}

func (f *GitHubPRFeedbackForwarder) ForwardAuthorizedFeedback(ctx context.Context, projectID string, repo *GitHubRepoRef) (*GitHubPRFeedbackForwardResult, error) {
	if f == nil || f.github == nil {
		return nil, fmt.Errorf("github feedback provider unavailable")
	}
	if f.prRepo == nil || f.feedbackRepo == nil || f.authRepo == nil || f.threadInputRepo == nil {
		return nil, fmt.Errorf("github pr feedback forwarding dependencies unavailable")
	}
	if repo == nil {
		return nil, fmt.Errorf("repository reference is required")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	prs, err := f.prRepo.ListOpenByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := &GitHubPRFeedbackForwardResult{OK: true, ScannedPullRequests: len(prs)}
	for _, pr := range prs {
		if pr.PRNumber <= 0 || strings.TrimSpace(pr.TaskID) == "" || strings.TrimSpace(pr.ID) == "" {
			continue
		}
		items, err := f.github.ListPullRequestFeedback(ctx, repo, pr.PRNumber)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Kind) == "" {
				continue
			}
			if strings.TrimSpace(item.Body) == "" && strings.TrimSpace(item.State) == "" {
				result.SkippedEmpty++
				continue
			}
			author := repository.NormalizeGitHubLogin(item.AuthorLogin)
			if author == "" {
				result.SkippedUnauthorized++
				continue
			}
			authorized, err := f.authRepo.IsActorAuthorized(ctx, author)
			if err != nil {
				return nil, err
			}
			if !authorized {
				result.SkippedUnauthorized++
				continue
			}
			already, err := f.feedbackRepo.AlreadyForwarded(ctx, repo.FullName, pr.PRNumber, item.Kind, item.ID)
			if err != nil {
				return nil, err
			}
			if already {
				result.SkippedDuplicate++
				continue
			}
			queued := &models.ThreadInput{
				Scope:       models.ThreadInputScopeTask,
				ProjectID:   projectID,
				TaskID:      pr.TaskID,
				Content:     formatGitHubPRFeedbackForTask(pr.PRNumber, item),
				Source:      models.TaskOriginSystemAgent,
				OriginAgent: "github-dev-inbox",
			}
			createdAt := item.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			feedbackRecord := &models.GitHubPRFeedbackForwarded{
				TaskPullRequestID: pr.ID,
				TaskID:            pr.TaskID,
				RepoFullName:      repo.FullName,
				PRNumber:          pr.PRNumber,
				FeedbackKind:      item.Kind,
				GitHubID:          item.ID,
				GitHubNodeID:      item.NodeID,
				AuthorLogin:       author,
				HTMLURL:           item.URL,
				Body:              item.Body,
				CreatedAt:         createdAt,
			}
			recorded, err := f.feedbackRepo.RecordForwardedAndQueue(ctx, f.threadInputRepo, feedbackRecord, queued)
			if err != nil {
				return nil, err
			}
			if !recorded {
				result.SkippedDuplicate++
				continue
			}
			result.Forwarded = append(result.Forwarded, GitHubPRFeedbackForwardedResult{
				TaskID:            pr.TaskID,
				PullRequestNumber: pr.PRNumber,
				FeedbackKind:      item.Kind,
				GitHubID:          item.ID,
				AuthorLogin:       author,
				QueuedMessageID:   queued.ID,
				URL:               item.URL,
			})
		}
	}
	return result, nil
}

func formatGitHubPRFeedbackForTask(prNumber int, item GitHubPullRequestFeedback) string {
	kind := strings.ReplaceAll(strings.TrimSpace(item.Kind), "_", " ")
	if kind == "" {
		kind = "feedback"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GitHub PR #%d received authorized %s feedback from @%s.\n", prNumber, kind, repository.NormalizeGitHubLogin(item.AuthorLogin))
	if strings.TrimSpace(item.State) != "" {
		fmt.Fprintf(&b, "Review state: %s\n", strings.TrimSpace(item.State))
	}
	if strings.TrimSpace(item.Path) != "" {
		fmt.Fprintf(&b, "File: %s", strings.TrimSpace(item.Path))
		if item.Line > 0 {
			fmt.Fprintf(&b, ":%d", item.Line)
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(item.URL) != "" {
		fmt.Fprintf(&b, "GitHub URL: %s\n", strings.TrimSpace(item.URL))
	}
	body := strings.TrimSpace(item.Body)
	if body != "" {
		b.WriteString("\nFeedback:\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString("\nPlease address this GitHub PR feedback in the task worktree, then update or reuse the PR when ready.")
	return strings.TrimSpace(b.String())
}
