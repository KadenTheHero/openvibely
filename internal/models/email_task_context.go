package models

import "time"

// EmailTaskContext stores email reply metadata for email-origin tasks.
type EmailTaskContext struct {
	TaskID          string    `json:"task_id"`
	EmailFrom       string    `json:"email_from"`
	EmailMessageID  string    `json:"email_message_id"`
	EmailReferences string    `json:"email_references"`
	EmailSubject    string    `json:"email_subject"`
	EmailSessionKey string    `json:"email_session_key"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
