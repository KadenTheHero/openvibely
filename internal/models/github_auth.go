package models

import "time"

// GitHubAuthorizedActor represents a GitHub user authorized to trigger GitHub-backed automation.
type GitHubAuthorizedActor struct {
	ID           string    `json:"id"`
	GitHubUserID *int64    `json:"github_user_id,omitempty"`
	GitHubLogin  string    `json:"github_login"`
	DisplayName  string    `json:"display_name"`
	Permission   string    `json:"permission"`
	AddedAt      time.Time `json:"added_at"`
	AddedBy      string    `json:"added_by"`
}

// GitHubAgentAssigneeMapping maps a GitHub assignee identity to an OpenVibely agent for one project.
type GitHubAgentAssigneeMapping struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	AgentID      string    `json:"agent_id"`
	Role         string    `json:"role"`
	GitHubUserID *int64    `json:"github_user_id,omitempty"`
	GitHubLogin  string    `json:"github_login"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
