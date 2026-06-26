-- +goose Up
CREATE TABLE IF NOT EXISTS discord_authorized_users (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    project_id TEXT NOT NULL,
    discord_user_id TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    added_by TEXT NOT NULL DEFAULT 'web',
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_discord_auth_unique_user_id
    ON discord_authorized_users(project_id, discord_user_id);
CREATE INDEX IF NOT EXISTS idx_discord_auth_project
    ON discord_authorized_users(project_id);
CREATE INDEX IF NOT EXISTS idx_discord_auth_user
    ON discord_authorized_users(discord_user_id);

CREATE TABLE IF NOT EXISTS discord_task_context (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    discord_channel_id TEXT NOT NULL,
    discord_thread_id TEXT NOT NULL DEFAULT '',
    discord_message_id TEXT NOT NULL DEFAULT '',
    discord_user_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_discord_task_context_channel ON discord_task_context(discord_channel_id, discord_thread_id);

ALTER TABLE thread_inputs ADD COLUMN discord_channel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE thread_inputs ADD COLUMN discord_thread_id TEXT NOT NULL DEFAULT '';
ALTER TABLE thread_inputs ADD COLUMN discord_message_id TEXT NOT NULL DEFAULT '';
ALTER TABLE thread_inputs ADD COLUMN discord_user_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE thread_inputs DROP COLUMN discord_user_id;
ALTER TABLE thread_inputs DROP COLUMN discord_message_id;
ALTER TABLE thread_inputs DROP COLUMN discord_thread_id;
ALTER TABLE thread_inputs DROP COLUMN discord_channel_id;
DROP TABLE IF EXISTS discord_task_context;
DROP TABLE IF EXISTS discord_authorized_users;
