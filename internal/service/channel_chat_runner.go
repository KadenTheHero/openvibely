package service

import (
	"context"

	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/models"
)

// ChannelChatRunRequest is the service-to-handler bridge for channel-origin
// Chat turns. Channel services own inbound channel parsing and reply metadata;
// the handler owns the shared steering-aware streaming runner.
type ChannelChatRunRequest struct {
	ExecID              string
	TaskID              string
	ProjectID           string
	Message             string
	Agent               models.LLMConfig
	ChatHistory         []models.Execution
	SystemContext       string
	WorkDir             string
	ImageAttachments    []models.Attachment
	Surface             chatcontrol.Surface
	InitialAckMessageID int
}

type ChannelChatRunner func(context.Context, ChannelChatRunRequest)

type ChannelTaskRunRequest struct {
	ExecID          string
	TaskID          string
	ProjectID       string
	Message         string
	Agent           models.LLMConfig
	ChatHistory     []models.Execution
	SystemContext   string
	Surface         chatcontrol.Surface
	AgentDefinition *models.Agent
	ReplyContext    ChannelReplyContext
}

type ChannelReplyContext struct {
	Source         string
	TelegramChatID int64
	SlackTeamID    string
	SlackChannelID string
	SlackThreadTS  string
	SlackUserID    string
}

type ChannelTaskRunner func(context.Context, ChannelTaskRunRequest)
