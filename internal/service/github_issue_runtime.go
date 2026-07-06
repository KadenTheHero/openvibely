package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openvibely/openvibely/internal/chatcontrol"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

type GitHubIssueRuntimeProvider interface {
	ResolveRepo(ctx context.Context, repoURL, repoPath string) (*GitHubRepoRef, error)
	PushBranch(ctx context.Context, repoPath, worktreePath, branch string, repo *GitHubRepoRef) error
	FindPullRequestByBranch(ctx context.Context, repo *GitHubRepoRef, branch string) (*GitHubPullRequest, error)
	CreatePullRequest(ctx context.Context, repo *GitHubRepoRef, createReq GitHubCreatePullRequestRequest) (*GitHubPullRequest, error)
	CreateIssue(ctx context.Context, repo *GitHubRepoRef, createReq GitHubCreateIssueRequest) (*GitHubIssue, error)
	GetIssue(ctx context.Context, repo *GitHubRepoRef, issueNumber int) (*GitHubIssue, error)
	ListAssignedIssuesWithPullRequests(ctx context.Context, repo *GitHubRepoRef, assignee string) ([]GitHubIssueWithPullRequest, error)
	FindPullRequestForIssue(ctx context.Context, repo *GitHubRepoRef, issueNumber int) (*GitHubPullRequest, error)
	CommentOnIssue(ctx context.Context, repo *GitHubRepoRef, issueNumber int, bodyText string) error
	AddLabelsToIssue(ctx context.Context, repo *GitHubRepoRef, issueNumber int, labels []string) error
}

type githubIssueRuntimeOptions struct {
	ProjectID           string
	ProjectRepo         *repository.ProjectRepo
	TaskRepo            *repository.TaskRepo
	TaskPullRequestRepo *repository.TaskPullRequestRepo
	GitHubAuthRepo      *repository.GitHubAuthRepo
	GitHub              GitHubIssueRuntimeProvider
}

type githubCreateIssueRuntimeInput struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
}

type githubIssueRuntimeInput struct {
	IssueNumber int      `json:"issue_number"`
	IssueURL    string   `json:"issue_url"`
	Assignee    string   `json:"assignee"`
	GitHubLogin string   `json:"github_login"`
	Body        string   `json:"body"`
	Labels      []string `json:"labels"`
	TaskID      string   `json:"task_id"`
	Title       string   `json:"title"`
	PRTitle     string   `json:"pr_title"`
	PRBody      string   `json:"pr_body"`
	Base        string   `json:"base"`
	Draft       bool     `json:"draft"`
}

func buildGitHubIssueRuntimeTools(opts githubIssueRuntimeOptions) *llmcontracts.RuntimeTools {
	if opts.GitHub == nil || opts.ProjectRepo == nil || strings.TrimSpace(opts.ProjectID) == "" {
		return nil
	}
	defs := gitHubIssueRuntimeToolDefs()
	if len(defs) == 0 {
		return nil
	}
	handlers := buildGitHubIssueRuntimeHandlers(opts)
	return &llmcontracts.RuntimeTools{
		Definitions: defs,
		Executor:    chatcontrol.BuildRuntimeToolExecutor(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, handlers),
	}
}

func gitHubIssueRuntimeToolDefs() []llmcontracts.RuntimeToolDefinition {
	defs := chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, false)
	filtered := make([]llmcontracts.RuntimeToolDefinition, 0, 6)
	for _, def := range defs {
		if strings.HasPrefix(strings.ToLower(def.Name), "github_") {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func buildGitHubIssueRuntimeHandlers(opts githubIssueRuntimeOptions) map[string]chatcontrol.RuntimeActionHandler {
	return map[string]chatcontrol.RuntimeActionHandler{
		"github_create_issue": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req githubCreateIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			repo, err := resolveGitHubRepoForRuntimeTool(ctx, opts)
			if err != nil {
				return "", err
			}
			issue, err := opts.GitHub.CreateIssue(ctx, repo, GitHubCreateIssueRequest{Title: req.Title, Body: req.Body, Labels: req.Labels, Assignees: req.Assignees})
			if err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "issue": issue})
		},
		"github_get_issue": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			repo, err := resolveGitHubRepoForRuntimeTool(ctx, opts)
			if err != nil {
				return "", err
			}
			issue, err := opts.GitHub.GetIssue(ctx, repo, req.IssueNumber)
			if err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "issue": issue})
		},
		"github_get_project_inbox": func(ctx context.Context, _ json.RawMessage) (string, error) {
			if opts.GitHubAuthRepo == nil {
				return "", fmt.Errorf("github auth repository unavailable")
			}
			inbox, err := opts.GitHubAuthRepo.GetEnabledProjectInbox(ctx, opts.ProjectID)
			if err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "configured": inbox != nil, "inbox": inbox})
		},
		"github_is_actor_authorized": func(ctx context.Context, input json.RawMessage) (string, error) {
			if opts.GitHubAuthRepo == nil {
				return "", fmt.Errorf("github auth repository unavailable")
			}
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			login := strings.TrimSpace(req.GitHubLogin)
			if login == "" {
				return "", fmt.Errorf("github_login is required")
			}
			authorized, err := opts.GitHubAuthRepo.IsActorAuthorized(ctx, login)
			if err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "github_login": repository.NormalizeGitHubLogin(login), "authorized": authorized})
		},
		"github_list_assigned_issues_with_prs": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			if strings.TrimSpace(req.Assignee) == "" {
				return "", fmt.Errorf("assignee is required")
			}
			repo, err := resolveGitHubRepoForRuntimeTool(ctx, opts)
			if err != nil {
				return "", err
			}
			items, err := opts.GitHub.ListAssignedIssuesWithPullRequests(ctx, repo, req.Assignee)
			if err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "items": items, "skipped_without_pr": "Assigned issues without an associated pull request are skipped."})
		},
		"github_comment_on_issue": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			repo, err := resolveGitHubRepoForRuntimeTool(ctx, opts)
			if err != nil {
				return "", err
			}
			if err := opts.GitHub.CommentOnIssue(ctx, repo, req.IssueNumber, req.Body); err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "issue_number": req.IssueNumber})
		},
		"github_add_issue_labels": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			repo, err := resolveGitHubRepoForRuntimeTool(ctx, opts)
			if err != nil {
				return "", err
			}
			if err := opts.GitHub.AddLabelsToIssue(ctx, repo, req.IssueNumber, req.Labels); err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "issue_number": req.IssueNumber, "labels": req.Labels})
		},
		"github_link_task_to_issue": func(ctx context.Context, input json.RawMessage) (string, error) {
			if opts.TaskPullRequestRepo == nil {
				return "", fmt.Errorf("task pull request repository unavailable")
			}
			if opts.TaskRepo == nil {
				return "", fmt.Errorf("task repository unavailable")
			}
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			task, err := resolveGitHubRuntimeTask(ctx, opts.TaskRepo, opts.ProjectID, req.TaskID, req.Title)
			if err != nil {
				return "", err
			}
			repo, err := resolveGitHubRepoForRuntimeTool(ctx, opts)
			if err != nil {
				return "", err
			}
			issue, err := opts.GitHub.GetIssue(ctx, repo, req.IssueNumber)
			if err != nil {
				return "", err
			}
			pr, err := opts.GitHub.FindPullRequestForIssue(ctx, repo, req.IssueNumber)
			if err != nil {
				return "", err
			}
			if pr == nil {
				return githubIssueRuntimeJSON(map[string]any{"ok": false, "skipped": true, "issue_number": req.IssueNumber, "reason": "assigned issue has no associated pull request"})
			}
			issueNumber := req.IssueNumber
			issueURL := ""
			if issue != nil {
				issueURL = issue.URL
			}
			record := &models.TaskPullRequest{TaskID: task.ID, PRNumber: pr.Number, PRURL: pr.URL, PRState: pr.State, IssueNumber: &issueNumber, IssueURL: issueURL}
			if err := opts.TaskPullRequestRepo.Upsert(ctx, record); err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "task_id": task.ID, "issue_number": issueNumber, "issue_url": issueURL, "pull_request": pr})
		},
		"github_open_pull_request": func(ctx context.Context, input json.RawMessage) (string, error) {
			if opts.TaskPullRequestRepo == nil {
				return "", fmt.Errorf("task pull request repository unavailable")
			}
			if opts.TaskRepo == nil {
				return "", fmt.Errorf("task repository unavailable")
			}
			var req githubIssueRuntimeInput
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			project, err := resolveGitHubRuntimeProject(ctx, opts)
			if err != nil {
				return "", err
			}
			task, err := resolveGitHubRuntimeTask(ctx, opts.TaskRepo, opts.ProjectID, req.TaskID, req.Title)
			if err != nil {
				return "", err
			}
			var issueNumber *int
			if req.IssueNumber > 0 {
				issueNumber = &req.IssueNumber
			}
			result, err := NewTaskPullRequestService(opts.GitHub, opts.TaskPullRequestRepo).OpenForTask(ctx, project, task, OpenTaskPullRequestOptions{
				Title:       req.PRTitle,
				Body:        req.PRBody,
				Base:        req.Base,
				Draft:       req.Draft,
				IssueNumber: issueNumber,
				IssueURL:    req.IssueURL,
			})
			if err != nil {
				return "", err
			}
			return githubIssueRuntimeJSON(map[string]any{"ok": true, "task_id": task.ID, "pull_request": result.PullRequest, "reused_existing_record": result.ReusedExistingRecord, "reused_remote": result.ReusedRemote, "created": result.Created})
		},
	}
}

func resolveGitHubRuntimeProject(ctx context.Context, opts githubIssueRuntimeOptions) (*models.Project, error) {
	project, err := opts.ProjectRepo.GetByID(ctx, opts.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("current project not found")
	}
	return project, nil
}

func resolveGitHubRepoForRuntimeTool(ctx context.Context, opts githubIssueRuntimeOptions) (*GitHubRepoRef, error) {
	project, err := resolveGitHubRuntimeProject(ctx, opts)
	if err != nil {
		return nil, err
	}
	return opts.GitHub.ResolveRepo(ctx, project.RepoURL, project.RepoPath)
}

func resolveGitHubRuntimeTask(ctx context.Context, taskRepo *repository.TaskRepo, projectID, taskID, title string) (*models.Task, error) {
	if taskRepo == nil {
		return nil, fmt.Errorf("task repository unavailable")
	}
	if strings.TrimSpace(taskID) != "" {
		task, err := taskRepo.GetByID(ctx, strings.TrimSpace(taskID))
		if err != nil {
			return nil, err
		}
		if task == nil || task.ProjectID != projectID {
			return nil, fmt.Errorf("task not found in current project")
		}
		return task, nil
	}
	if strings.TrimSpace(title) != "" {
		task, err := taskRepo.GetByProjectAndTitle(ctx, projectID, strings.TrimSpace(title))
		if err != nil {
			return nil, err
		}
		if task == nil {
			return nil, fmt.Errorf("task not found in current project")
		}
		return task, nil
	}
	return nil, fmt.Errorf("task_id or title is required")
}

func githubIssueRuntimeJSON(payload map[string]any) (string, error) {
	b, err := json.Marshal(payload)
	return string(b), err
}
