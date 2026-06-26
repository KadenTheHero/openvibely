package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/openvibely/openvibely/internal/models"
)

// EmailAuthRepo handles database operations for Email authorized senders.
type EmailAuthRepo struct {
	db *sql.DB
}

// NewEmailAuthRepo creates a new EmailAuthRepo.
func NewEmailAuthRepo(db *sql.DB) *EmailAuthRepo {
	return &EmailAuthRepo{db: db}
}

// NormalizeEmailAddress trims and lowercases an email address for storage and matching.
func NormalizeEmailAddress(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ListByProject returns all authorized email senders for a project.
func (r *EmailAuthRepo) ListByProject(ctx context.Context, projectID string) ([]models.EmailAuthorizedSender, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, email_address, display_name, added_at, added_by
		 FROM email_authorized_senders
		 WHERE project_id = ?
		 ORDER BY added_at ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list email authorized senders: %w", err)
	}
	defer rows.Close()

	var senders []models.EmailAuthorizedSender
	for rows.Next() {
		var s models.EmailAuthorizedSender
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.EmailAddress, &s.DisplayName, &s.AddedAt, &s.AddedBy); err != nil {
			return nil, fmt.Errorf("scan email authorized sender: %w", err)
		}
		senders = append(senders, s)
	}
	return senders, rows.Err()
}

// IsAuthorized checks whether an email address is authorized for a given project.
func (r *EmailAuthRepo) IsAuthorized(ctx context.Context, projectID, emailAddress string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM email_authorized_senders
		 WHERE project_id = ? AND lower(email_address) = lower(?)`,
		projectID, NormalizeEmailAddress(emailAddress)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check email authorization: %w", err)
	}
	return count > 0, nil
}

// HasAnyAuthorizedUsers checks whether a project has any authorized email senders configured.
func (r *EmailAuthRepo) HasAnyAuthorizedUsers(ctx context.Context, projectID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM email_authorized_senders WHERE project_id = ?`, projectID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count email authorized senders: %w", err)
	}
	return count > 0, nil
}

// HasAnyAuthorizedUsersAnywhere checks whether any project has email authorized senders configured.
func (r *EmailAuthRepo) HasAnyAuthorizedUsersAnywhere(ctx context.Context) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_authorized_senders`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count email authorized senders anywhere: %w", err)
	}
	return count > 0, nil
}

// IsAuthorizedAnywhere checks whether an email address is authorized in any project.
func (r *EmailAuthRepo) IsAuthorizedAnywhere(ctx context.Context, emailAddress string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM email_authorized_senders WHERE lower(email_address) = lower(?)`,
		NormalizeEmailAddress(emailAddress)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check email authorization anywhere: %w", err)
	}
	return count > 0, nil
}

// Create adds a new authorized email sender to a project.
func (r *EmailAuthRepo) Create(ctx context.Context, s *models.EmailAuthorizedSender) error {
	s.EmailAddress = NormalizeEmailAddress(s.EmailAddress)
	return r.db.QueryRowContext(ctx,
		`INSERT INTO email_authorized_senders (project_id, email_address, display_name, added_by)
		 VALUES (?, ?, ?, ?)
		 RETURNING id, added_at`,
		s.ProjectID, s.EmailAddress, s.DisplayName, s.AddedBy).
		Scan(&s.ID, &s.AddedAt)
}

// Delete removes an authorized email sender by ID.
func (r *EmailAuthRepo) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM email_authorized_senders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete email authorized sender: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("email authorized sender not found")
	}
	return nil
}

// GetByID returns a single authorized email sender by ID.
func (r *EmailAuthRepo) GetByID(ctx context.Context, id string) (*models.EmailAuthorizedSender, error) {
	var s models.EmailAuthorizedSender
	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, email_address, display_name, added_at, added_by
		 FROM email_authorized_senders WHERE id = ?`, id).
		Scan(&s.ID, &s.ProjectID, &s.EmailAddress, &s.DisplayName, &s.AddedAt, &s.AddedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get email authorized sender: %w", err)
	}
	return &s, nil
}
