package models

import "time"

// GitHubAuthorizedActor represents a GitHub user authorized to approve or trigger GitHub-backed automation.
type GitHubAuthorizedActor struct {
	ID           string    `json:"id"`
	GitHubUserID *int64    `json:"github_user_id,omitempty"`
	GitHubLogin  string    `json:"github_login"`
	DisplayName  string    `json:"display_name"`
	Permission   string    `json:"permission"`
	AddedAt      time.Time `json:"added_at"`
	AddedBy      string    `json:"added_by"`
}

// GitHubProjectInbox stores the GitHub assignee identity a project's scheduled GitHub poller should watch.
type GitHubProjectInbox struct {
	ProjectID    string    `json:"project_id"`
	GitHubUserID *int64    `json:"github_user_id,omitempty"`
	GitHubLogin  string    `json:"github_login"`
	AgentID      *string   `json:"agent_id,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
