package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/openvibely/openvibely/internal/models"
)

// DiscordAuthRepo handles database operations for Discord authorized users.
type DiscordAuthRepo struct {
	db *sql.DB
}

// NewDiscordAuthRepo creates a new DiscordAuthRepo.
func NewDiscordAuthRepo(db *sql.DB) *DiscordAuthRepo {
	return &DiscordAuthRepo{db: db}
}

// ListByProject returns all authorized Discord users for a project.
func (r *DiscordAuthRepo) ListByProject(ctx context.Context, projectID string) ([]models.DiscordAuthorizedUser, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, discord_user_id, display_name, added_at, added_by
		 FROM discord_authorized_users
		 WHERE project_id = ?
		 ORDER BY added_at ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list discord auth users: %w", err)
	}
	defer rows.Close()

	var users []models.DiscordAuthorizedUser
	for rows.Next() {
		var u models.DiscordAuthorizedUser
		if err := rows.Scan(&u.ID, &u.ProjectID, &u.DiscordUserID, &u.DisplayName, &u.AddedAt, &u.AddedBy); err != nil {
			return nil, fmt.Errorf("scan discord auth user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// IsAuthorized checks whether a Discord user is authorized for a given project.
func (r *DiscordAuthRepo) IsAuthorized(ctx context.Context, projectID, discordUserID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM discord_authorized_users
		 WHERE project_id = ? AND discord_user_id = ?`,
		projectID, strings.TrimSpace(discordUserID)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check discord auth: %w", err)
	}
	return count > 0, nil
}

// HasAnyAuthorizedUsers checks whether a project has any authorized Discord users configured.
func (r *DiscordAuthRepo) HasAnyAuthorizedUsers(ctx context.Context, projectID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM discord_authorized_users WHERE project_id = ?`,
		projectID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count discord auth users: %w", err)
	}
	return count > 0, nil
}

// HasAnyAuthorizedUsersAnywhere checks whether any project has Discord authorized users configured.
func (r *DiscordAuthRepo) HasAnyAuthorizedUsersAnywhere(ctx context.Context) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM discord_authorized_users`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count discord auth users anywhere: %w", err)
	}
	return count > 0, nil
}

// IsAuthorizedAnywhere checks whether a Discord user is authorized in any project.
func (r *DiscordAuthRepo) IsAuthorizedAnywhere(ctx context.Context, discordUserID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM discord_authorized_users WHERE discord_user_id = ?`,
		strings.TrimSpace(discordUserID)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check discord auth anywhere: %w", err)
	}
	return count > 0, nil
}

// Create adds a new authorized Discord user to a project.
func (r *DiscordAuthRepo) Create(ctx context.Context, u *models.DiscordAuthorizedUser) error {
	u.DiscordUserID = strings.TrimSpace(u.DiscordUserID)
	return r.db.QueryRowContext(ctx,
		`INSERT INTO discord_authorized_users (project_id, discord_user_id, display_name, added_by)
		 VALUES (?, ?, ?, ?)
		 RETURNING id, added_at`,
		u.ProjectID, u.DiscordUserID, u.DisplayName, u.AddedBy).
		Scan(&u.ID, &u.AddedAt)
}

// DeleteByProject removes all authorized Discord users for a project.
func (r *DiscordAuthRepo) DeleteByProject(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM discord_authorized_users WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("delete discord auth users for project: %w", err)
	}
	return nil
}

// Delete removes an authorized Discord user by ID.
func (r *DiscordAuthRepo) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM discord_authorized_users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete discord auth user: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("discord auth user not found")
	}
	return nil
}

// GetByID returns a single authorized Discord user by ID.
func (r *DiscordAuthRepo) GetByID(ctx context.Context, id string) (*models.DiscordAuthorizedUser, error) {
	var u models.DiscordAuthorizedUser
	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, discord_user_id, display_name, added_at, added_by
		 FROM discord_authorized_users WHERE id = ?`, id).
		Scan(&u.ID, &u.ProjectID, &u.DiscordUserID, &u.DisplayName, &u.AddedAt, &u.AddedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get discord auth user: %w", err)
	}
	return &u, nil
}
