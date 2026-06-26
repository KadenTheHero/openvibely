package models

import "time"

// ChannelTarget is a saved outbound destination an agent may use with send_message.
type ChannelTarget struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Platform       string    `json:"platform"`
	Name           string    `json:"name,omitempty"`
	TargetID       string    `json:"target_id"`
	ThreadID       string    `json:"thread_id,omitempty"`
	Home           bool      `json:"home"`
	DefaultSubject string    `json:"default_subject,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ChannelMessageSend records one send_message attempt for auditability.
type ChannelMessageSend struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	Platform           string    `json:"platform"`
	TargetID           string    `json:"target_id"`
	ThreadID           string    `json:"thread_id,omitempty"`
	RequestedBySurface string    `json:"requested_by_surface,omitempty"`
	RequestedByUser    string    `json:"requested_by_user,omitempty"`
	MessagePreview     string    `json:"message_preview"`
	Success            bool      `json:"success"`
	Error              string    `json:"error,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}
