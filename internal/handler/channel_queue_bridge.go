package handler

import (
	"context"

	"github.com/openvibely/openvibely/internal/models"
)

// PromoteQueuedChatInput starts the next pending queued chat input for a project
// if no chat response is currently running. Channel services call this after
// completing their own chat-origin runs so channel/web/API follow-ups queued
// behind that run are delivered by the shared chat runner.
func (h *Handler) PromoteQueuedChatInput(projectID string) {
	if projectID == "" {
		return
	}
	h.startNextQueuedTurnAfter(context.Background(), streamingResponseParams{
		ProjectID: projectID,
		ChatMode:  models.ChatModeOrchestrate,
	}, "")
}
