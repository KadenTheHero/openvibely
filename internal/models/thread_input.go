package models

import "time"

type ThreadInputMode string

const (
	ThreadInputModeQueued   ThreadInputMode = "queued"
	ThreadInputModeSteering ThreadInputMode = "steering"
)

type ThreadInputStatus string

const (
	ThreadInputPending   ThreadInputStatus = "pending"
	ThreadInputApplied   ThreadInputStatus = "applied"
	ThreadInputCancelled ThreadInputStatus = "cancelled"
)

type ThreadInputScope string

const (
	ThreadInputScopeChat ThreadInputScope = "chat"
	ThreadInputScopeTask ThreadInputScope = "task_thread"
)

type ThreadInput struct {
	ID                  string            `json:"id"`
	Scope               ThreadInputScope  `json:"scope"`
	ProjectID           string            `json:"project_id"`
	TaskID              string            `json:"task_id"`
	RunExecutionID      string            `json:"run_execution_id"`
	AgentConfigID       string            `json:"agent_config_id"`
	InputMode           ThreadInputMode   `json:"input_mode"`
	InputStatus         ThreadInputStatus `json:"input_status"`
	TurnID              string            `json:"turn_id"`
	ExpectedTurnID      string            `json:"expected_turn_id"`
	Content             string            `json:"content"`
	AttachmentSessionID string            `json:"attachment_session_id"`
	QueuePosition       int64             `json:"queue_position"`
	ChatMode            ChatMode          `json:"chat_mode"`
	Source              string            `json:"source"`
	OriginAgent         string            `json:"origin_agent"`
	TelegramChatID      int64             `json:"telegram_chat_id"`
	SlackTeamID         string            `json:"slack_team_id"`
	SlackChannelID      string            `json:"slack_channel_id"`
	SlackThreadTS       string            `json:"slack_thread_ts"`
	SlackUserID         string            `json:"slack_user_id"`
	EmailFrom           string            `json:"email_from"`
	EmailMessageID      string            `json:"email_message_id"`
	EmailReferences     string            `json:"email_references"`
	EmailSubject        string            `json:"email_subject"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	AppliedAt           *time.Time        `json:"applied_at"`
}
