package models

import "time"

// EmailSenderProject persists the active project selection for an email sender.
type EmailSenderProject struct {
	EmailAddress string    `db:"email_address"`
	ProjectID    string    `db:"project_id"`
	UpdatedAt    time.Time `db:"updated_at"`
}
