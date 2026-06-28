package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// DiscordUserProjectRepo persists active project selection per Discord user.
type DiscordUserProjectRepo struct {
	db *sql.DB
}

func NewDiscordUserProjectRepo(db *sql.DB) *DiscordUserProjectRepo {
	return &DiscordUserProjectRepo{db: db}
}

func (r *DiscordUserProjectRepo) SetUserProject(ctx context.Context, discordUserID, projectID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO discord_user_projects (discord_user_id, project_id, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(discord_user_id) DO UPDATE
		 SET project_id = excluded.project_id, updated_at = datetime('now')`,
		discordUserID, projectID)
	if err != nil {
		return fmt.Errorf("set discord user project: %w", err)
	}
	return nil
}

func (r *DiscordUserProjectRepo) GetUserProject(ctx context.Context, discordUserID string) (string, error) {
	var projectID string
	err := r.db.QueryRowContext(ctx,
		`SELECT project_id FROM discord_user_projects WHERE discord_user_id = ?`,
		discordUserID).Scan(&projectID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get discord user project: %w", err)
	}
	return projectID, nil
}

func (r *DiscordUserProjectRepo) DeleteUserProject(ctx context.Context, discordUserID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM discord_user_projects WHERE discord_user_id = ?`,
		discordUserID)
	if err != nil {
		return fmt.Errorf("delete discord user project: %w", err)
	}
	return nil
}
