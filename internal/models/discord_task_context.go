package models

import "time"

// DiscordTaskContext stores Discord reply context for tasks created from Discord so
// completion notifications can be posted back to the originating channel/thread.
type DiscordTaskContext struct {
	TaskID           string    `json:"task_id"`
	DiscordChannelID string    `json:"discord_channel_id"`
	DiscordThreadID  string    `json:"discord_thread_id"`
	DiscordMessageID string    `json:"discord_message_id"`
	DiscordUserID    string    `json:"discord_user_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
