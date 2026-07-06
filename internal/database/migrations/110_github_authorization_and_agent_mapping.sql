-- +goose Up
CREATE TABLE github_authorized_actors (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    github_user_id INTEGER,
    github_login TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    permission TEXT NOT NULL DEFAULT 'triage',
    added_at DATETIME NOT NULL DEFAULT (datetime('now')),
    added_by TEXT NOT NULL DEFAULT 'web',
    CHECK (trim(github_login) != ''),
    CHECK (permission IN ('triage', 'approve', 'admin'))
);

CREATE UNIQUE INDEX idx_github_authorized_actors_login
    ON github_authorized_actors(lower(github_login));
CREATE UNIQUE INDEX idx_github_authorized_actors_user_id
    ON github_authorized_actors(github_user_id)
    WHERE github_user_id IS NOT NULL;

CREATE TABLE github_agent_assignee_mappings (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'dev',
    github_user_id INTEGER,
    github_login TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    CHECK (trim(github_login) != ''),
    CHECK (trim(role) != '')
);

CREATE UNIQUE INDEX idx_github_agent_assignee_project_role
    ON github_agent_assignee_mappings(project_id, role);
CREATE UNIQUE INDEX idx_github_agent_assignee_project_login
    ON github_agent_assignee_mappings(project_id, lower(github_login));
CREATE INDEX idx_github_agent_assignee_agent
    ON github_agent_assignee_mappings(agent_id);
CREATE INDEX idx_github_agent_assignee_enabled
    ON github_agent_assignee_mappings(project_id, enabled);

-- +goose Down
DROP INDEX IF EXISTS idx_github_agent_assignee_enabled;
DROP INDEX IF EXISTS idx_github_agent_assignee_agent;
DROP INDEX IF EXISTS idx_github_agent_assignee_project_login;
DROP INDEX IF EXISTS idx_github_agent_assignee_project_role;
DROP TABLE IF EXISTS github_agent_assignee_mappings;

DROP INDEX IF EXISTS idx_github_authorized_actors_user_id;
DROP INDEX IF EXISTS idx_github_authorized_actors_login;
DROP TABLE IF EXISTS github_authorized_actors;
