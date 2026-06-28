package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// EmailSenderProjectRepo persists active project selection per email sender.
type EmailSenderProjectRepo struct {
	db *sql.DB
}

func NewEmailSenderProjectRepo(db *sql.DB) *EmailSenderProjectRepo {
	return &EmailSenderProjectRepo{db: db}
}

// SetSenderProject writes the active project selection for a normalized sender email.
func (r *EmailSenderProjectRepo) SetSenderProject(ctx context.Context, emailAddress, projectID string) error {
	emailAddress = NormalizeEmailAddress(emailAddress)
	if emailAddress == "" {
		return fmt.Errorf("set email sender project: email address is required")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO email_sender_projects (email_address, project_id, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(email_address) DO UPDATE
		 SET project_id = excluded.project_id, updated_at = datetime('now')`,
		emailAddress, projectID)
	if err != nil {
		return fmt.Errorf("set email sender project: %w", err)
	}
	return nil
}

// GetSenderProject returns the active project ID for a normalized sender email, or "" if not set.
func (r *EmailSenderProjectRepo) GetSenderProject(ctx context.Context, emailAddress string) (string, error) {
	emailAddress = NormalizeEmailAddress(emailAddress)
	var projectID string
	err := r.db.QueryRowContext(ctx,
		`SELECT project_id FROM email_sender_projects WHERE email_address = ?`,
		emailAddress).Scan(&projectID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get email sender project: %w", err)
	}
	return projectID, nil
}

// DeleteSenderProject removes the active project selection for a normalized sender email.
func (r *EmailSenderProjectRepo) DeleteSenderProject(ctx context.Context, emailAddress string) error {
	emailAddress = NormalizeEmailAddress(emailAddress)
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM email_sender_projects WHERE email_address = ?`,
		emailAddress)
	if err != nil {
		return fmt.Errorf("delete email sender project: %w", err)
	}
	return nil
}
