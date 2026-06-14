package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/events"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/web/templates/components"
	"github.com/openvibely/openvibely/web/templates/pages"
)

const chatSSETimeout = chatProcessingTimeout + 30*time.Second

const (
	chatUIWindowLimitDefault = 5
	chatUIWindowLimitMax     = 100
)

func (h *Handler) Chat(c echo.Context) error {
	isHTMX := isHTMX(c)
	applog.Debugf("[handler] Chat requested htmx=%v", isHTMX)

	agents, err := h.llmConfigRepo.List(c.Request().Context())
	if err != nil {
		applog.Infof("[handler] Chat error listing agents: %v", err)
		return err
	}

	currentProjectID, _ := h.getCurrentProjectID(c)

	limit := parseThreadWindowLimit(c.QueryParam("limit"), chatUIWindowLimitDefault, chatUIWindowLimitMax)
	beforeExecID := strings.TrimSpace(c.QueryParam("before"))
	chatHistory, hasEarlier, err := h.loadChatExecutionWindow(c.Request().Context(), currentProjectID, beforeExecID, limit)
	if err != nil {
		applog.Infof("[handler] Chat error loading chat history: %v", err)
		// Continue even if history load fails - just show empty chat
		chatHistory = []models.Execution{}
		hasEarlier = false
	}

	chatAttachmentsByExec := h.loadChatAttachmentsForExecutions(c.Request().Context(), chatHistory, "Chat")

	pendingInputs := []models.ThreadInput{}
	if h.threadInputRepo != nil && currentProjectID != "" {
		if inputs, inputErr := h.threadInputRepo.ListPendingForChat(c.Request().Context(), currentProjectID); inputErr == nil {
			pendingInputs = inputs
		} else {
			applog.Infof("[handler] Chat error loading pending inputs: %v", inputErr)
		}
	}

	latestPlanComplete := chatHistoryHasPlanCompletion(chatHistory)

	if beforeExecID != "" {
		return render(c, http.StatusOK, pages.ChatEarlierMessages(chatHistory, chatAttachmentsByExec, currentProjectID, hasEarlier, limit))
	}

	// For HTMX requests, return just the chat content
	if isHTMX {
		return render(c, http.StatusOK, pages.ChatContent(agents, chatHistory, currentProjectID, chatAttachmentsByExec, pendingInputs, latestPlanComplete, hasEarlier, limit))
	}

	projects, _ := h.projectSvc.List(c.Request().Context())
	return render(c, http.StatusOK, pages.Chat(projects, currentProjectID, agents, chatHistory, chatAttachmentsByExec, pendingInputs, latestPlanComplete, hasEarlier, limit))
}

func parseThreadWindowLimit(raw string, defaultLimit, maxLimit int) int {
	limit := defaultLimit
	if raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

func trimExecutionWindow(rows []models.Execution, limit int) ([]models.Execution, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	return rows[len(rows)-limit:], true
}

func executionIDs(executions []models.Execution) []string {
	execIDs := make([]string, len(executions))
	for i, exec := range executions {
		execIDs[i] = exec.ID
	}
	return execIDs
}

func (h *Handler) loadChatExecutionWindow(ctx context.Context, projectID, beforeExecID string, limit int) ([]models.Execution, bool, error) {
	queryLimit := limit + 1
	var rows []models.Execution
	var err error
	if beforeExecID != "" {
		rows, err = h.execRepo.ListChatHistoryBefore(ctx, projectID, beforeExecID, queryLimit)
	} else {
		rows, err = h.execRepo.ListChatHistory(ctx, projectID, queryLimit)
	}
	if err != nil {
		return nil, false, err
	}
	visible, hasEarlier := trimExecutionWindow(rows, limit)
	return visible, hasEarlier, nil
}

func (h *Handler) loadChatAttachmentsForExecutions(ctx context.Context, executions []models.Execution, label string) map[string][]models.ChatAttachment {
	execIDs := executionIDs(executions)
	chatAttachmentsByExec, err := h.chatAttachmentRepo.ListByExecutionIDs(ctx, execIDs)
	if err != nil {
		applog.Infof("[handler] %s error loading attachments: %v", label, err)
		return make(map[string][]models.ChatAttachment)
	}
	return chatAttachmentsByExec
}

func (h *Handler) ChatSend(c echo.Context) error {
	message := c.FormValue("message")
	agentID := c.FormValue("agent_id")
	chatMode := models.NormalizeChatMode(c.FormValue("chat_mode"))

	if message == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "message is required")
	}

	applog.Infof("[handler] ChatSend message=%q agent_id=%s chat_mode=%s", message, agentID, chatMode)

	hasModels, err := h.hasConfiguredModels(c)
	if err != nil {
		applog.Infof("[handler] ChatSend model availability check error: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to check model availability")
	}
	if !hasModels {
		applog.Infof("[handler] ChatSend blocked: no models configured")
		return noModelsConfiguredResponse(c)
	}

	// Check for pending image attachments (for vision-aware auto-selection)
	sessionID := c.FormValue("attachment_session_id")
	hasImages := hasPendingImages(sessionID)

	// Select agent (auto or explicit)
	agent, err := h.selectAgent(c.Request().Context(), agentID, message, hasImages)
	if err != nil {
		applog.Infof("[handler] ChatSend agent selection error: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Get project from query param or use default
	projectID, err := h.getCurrentProjectID(c)
	if err != nil || projectID == "" {
		applog.Infof("[handler] ChatSend error getting project: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "no project available")
	}

	// Note: Interactive chat intentionally bypasses task worker capacity checks.
	// Task worker limits (per-project/per-model) only gate task execution, not chat.
	// This ensures the chat orchestrator remains responsive even when all task workers are busy.

	activeChatExec, err := h.execRepo.FindLatestActiveChatExecution(c.Request().Context(), projectID)
	if err != nil {
		applog.Infof("[handler] ChatSend error checking active chat turn: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to check chat queue")
	}

	if activeChatExec != nil {
		if h.threadInputRepo == nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "chat input queue is unavailable")
		}
		queued := &models.ThreadInput{
			Scope:               models.ThreadInputScopeChat,
			ProjectID:           projectID,
			RunExecutionID:      activeChatExec.ID,
			AgentConfigID:       agent.ID,
			InputMode:           models.ThreadInputModeQueued,
			InputStatus:         models.ThreadInputPending,
			Content:             message,
			AttachmentSessionID: sessionID,
			ChatMode:            chatMode,
		}
		if err := h.threadInputRepo.CreateQueued(c.Request().Context(), queued); err != nil {
			applog.Infof("[handler] ChatSend error creating queued input: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to queue chat message")
		}
		if h.chatBroadcaster != nil {
			h.chatBroadcaster.Publish(events.ChatEvent{
				Type:      events.ChatNewMessage,
				ProjectID: projectID,
				ExecID:    queued.ID,
				Message:   message,
				Source:    "web",
				AgentName: agent.Name,
				Queued:    true,
			})
		}
		return render(c, http.StatusOK, components.ChatQueuedInputRowOOB(queued.ID, message, "/chat/queued/"+queued.ID+"/steer", queued.AttachmentSessionID != ""))
	}
	// Create a task record for the chat message (required for execution tracking)
	selectedAgentID := agent.ID
	chatTitle := fmt.Sprintf("Chat %s: %s", time.Now().Format("15:04:05.000"), message[:min(50, len(message))])
	task := &models.Task{
		ProjectID: projectID,
		Title:     chatTitle,
		Prompt:    message,
		Status:    models.StatusPending,
		Category:  models.CategoryChat,
		AgentID:   &selectedAgentID,
	}
	if err := h.taskRepo.Create(c.Request().Context(), task); err != nil {
		applog.Infof("[handler] ChatSend error creating task: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create chat task")
	}

	// Create execution record for immediate streaming delivery.
	execStatus := models.ExecRunning
	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        execStatus,
		PromptSent:    message,
	}
	if err := h.execRepo.Create(c.Request().Context(), exec); err != nil {
		applog.Infof("[handler] ChatSend error creating execution: %v", err)
		if delErr := h.taskRepo.Delete(c.Request().Context(), task.ID); delErr != nil {
			applog.Infof("[handler] ChatSend error cleaning up chat task=%s after execution create failure: %v", task.ID, delErr)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create execution")
	}

	applog.Infof("[handler] ChatSend created exec=%s for chat message status=%s", exec.ID, exec.Status)
	// Broadcast new message event so other tabs/clients update in real-time
	if h.chatBroadcaster != nil {
		h.chatBroadcaster.Publish(events.ChatEvent{
			Type:      events.ChatNewMessage,
			ProjectID: projectID,
			ExecID:    exec.ID,
			TaskID:    task.ID,
			Message:   message,
			Source:    "web",
			AgentName: agent.Name,
		})
	}

	// Handle file attachments if present
	var attachmentContext string
	var imageAttachments []models.Attachment
	var chatAttachments []models.ChatAttachment
	if sessionID != "" {
		applog.Infof("[handler] ChatSend processing attachments for session=%s", sessionID)
		var attErr error
		attachmentContext, imageAttachments, chatAttachments, attErr = h.processAttachmentsWithReturn(c.Request().Context(), sessionID, exec.ID)
		if attErr != nil {
			applog.Infof("[handler] ChatSend error processing attachments: %v", attErr)
			message = message + fmt.Sprintf("\n\n⚠️ Attachment processing error: %v", attErr)
		}
	}

	// Load recent chat history and filter for conversation context
	chatHistory, err := h.execRepo.ListChatHistory(c.Request().Context(), projectID, chatHistoryLimit)
	if err != nil {
		applog.Infof("[handler] ChatSend error loading chat history: %v", err)
		chatHistory = []models.Execution{}
	}
	priorHistory := filterChatHistory(chatHistory, exec.ID)

	// Render user message and streaming/queued placeholder
	var userMsg templ.Component
	if len(chatAttachments) > 0 {
		userMsg = components.ChatBubbleWithAttachments("User", message, chatAttachments)
	} else {
		userMsg = components.ChatBubble("User", message)
	}
	agentMsg := components.ChatBubbleStreaming("Assistant", exec.ID, "chat-messages", "", false)
	// Build context and spawn LLM processing goroutine
	availableModels, _ := h.llmConfigRepo.List(c.Request().Context())
	taskContext := h.buildChatContext(c.Request().Context(), projectID, availableModels)
	personalityContext := h.getPersonalityContext(c.Request().Context(), projectID)
	workDir := h.resolveWorkDir(c.Request().Context(), projectID)

	go h.processStreamingResponse(streamingResponseParams{
		ExecID:           exec.ID,
		TaskID:           task.ID,
		Message:          message,
		Agent:            *agent,
		ChatHistory:      priorHistory,
		ProjectID:        projectID,
		SystemContext:    combineContexts(combineContexts(taskContext, attachmentContext), personalityContext),
		WorkDir:          workDir,
		ImageAttachments: imageAttachments,
		IsTaskFollowup:   false,
		ProcessMarkers:   false,
		ChatMode:         chatMode,
		Surface:          chatcontrol.SurfaceWeb,
	})
	return render(c, http.StatusOK, templ.Join(userMsg, agentMsg))
}

func (h *Handler) ChatSteer(c echo.Context) error {
	message := strings.TrimSpace(c.FormValue("message"))
	chatMode := models.NormalizeChatMode(c.FormValue("chat_mode"))
	if message == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "message is required")
	}
	if h.threadInputRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "chat input queue is unavailable")
	}
	projectID, err := h.getCurrentProjectID(c)
	if err != nil || projectID == "" {
		applog.Infof("[handler] ChatSteer error getting project: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "no project available")
	}
	active, err := h.execRepo.FindLatestActiveChatExecution(c.Request().Context(), projectID)
	if err != nil {
		applog.Infof("[handler] ChatSteer active execution check failed project=%s: %v", projectID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to check active response")
	}
	if active == nil {
		return echo.NewHTTPError(http.StatusConflict, "no active response to steer; send a normal message instead")
	}
	expectedTurnID := c.FormValue("expected_turn_id")
	if expectedTurnID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "expected turn id is required")
	}
	if expectedTurnID != active.ID {
		return echo.NewHTTPError(http.StatusConflict, "active turn changed; queue the message instead")
	}
	input := &models.ThreadInput{
		Scope:               models.ThreadInputScopeChat,
		ProjectID:           projectID,
		RunExecutionID:      active.ID,
		InputMode:           models.ThreadInputModeSteering,
		InputStatus:         models.ThreadInputPending,
		TurnID:              active.ID,
		ExpectedTurnID:      expectedTurnID,
		Content:             message,
		AttachmentSessionID: c.FormValue("attachment_session_id"),
		ChatMode:            chatMode,
	}
	if err := h.threadInputRepo.CreateSteeringForActiveExecution(c.Request().Context(), input, active.ID); err != nil {
		applog.Infof("[handler] ChatSteer error creating steering input: %v", err)
		if errors.Is(err, repository.ErrExpectedTurnEmpty) {
			return echo.NewHTTPError(http.StatusBadRequest, "expected turn id is required")
		}
		if errors.Is(err, repository.ErrNoActiveTurn) || errors.Is(err, repository.ErrActiveTurnChanged) {
			return echo.NewHTTPError(http.StatusConflict, "active turn changed; queue the message instead")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save steering input")
	}
	if h.chatBroadcaster != nil {
		h.chatBroadcaster.Publish(events.ChatEvent{
			Type:      events.ChatTurnSteered,
			ProjectID: projectID,
			ExecID:    input.ID,
			Message:   message,
			Source:    "web",
			Steering:  true,
		})
	}
	return render(c, http.StatusOK, components.ChatSteeringInputRow(input.ID, message))
}

// isImageFile checks if a filename has a common image extension supported by Anthropic's API
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// mediaTypeFromExtension returns the MIME type for common file extensions.
// Uses the allowedFileExtensions map from the API for consistent type detection.
func mediaTypeFromExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if mt, ok := allowedFileExtensions[ext]; ok {
		return mt
	}
	return "text/plain"
}

// processAttachments moves uploaded files from pending directory to execution directory,
// creates database records, and returns text context and image attachments separately.
// Image files are returned as models.Attachment for multimodal API handling instead of
// being injected as raw bytes into the text prompt (which would cause "prompt too long" errors).
func (h *Handler) processAttachments(ctx context.Context, sessionID, execID string) (string, []models.Attachment, error) {
	textContext, imageAttachments, _, err := h.processAttachmentsWithReturn(ctx, sessionID, execID)
	return textContext, imageAttachments, err
}

// processAttachmentsWithReturn is like processAttachments but also returns the created ChatAttachment records
func (h *Handler) processAttachmentsWithReturn(ctx context.Context, sessionID, execID string) (string, []models.Attachment, []models.ChatAttachment, error) {
	if h.chatAttachmentRepo == nil {
		return "", nil, nil, fmt.Errorf("chat attachment repository is unavailable")
	}
	pendingDir := filepath.Join(uploadsDir, "chat", "pending", sessionID)

	// Check if pending directory exists
	if _, err := os.Stat(pendingDir); os.IsNotExist(err) {
		applog.Infof("[handler] processAttachmentsWithReturn pending directory not found: %s", pendingDir)
		return "", nil, nil, nil // Not an error, just no attachments
	}

	// Read files from pending directory
	files, err := os.ReadDir(pendingDir)
	if err != nil {
		return "", nil, nil, fmt.Errorf("reading pending directory: %w", err)
	}

	if len(files) == 0 {
		return "", nil, nil, nil
	}

	// Create execution-specific directory
	execDir := filepath.Join(uploadsDir, "chat", execID)
	if err := os.MkdirAll(execDir, 0755); err != nil {
		return "", nil, nil, fmt.Errorf("creating execution directory: %w", err)
	}

	var attachmentContents []string
	var imageAttachments []models.Attachment
	var chatAttachments []models.ChatAttachment

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		srcPath := filepath.Join(pendingDir, file.Name())
		destPath := filepath.Join(execDir, file.Name())

		// Move file
		if err := os.Rename(srcPath, destPath); err != nil {
			applog.Infof("[handler] processAttachments error moving file %s: %v", file.Name(), err)
			continue
		}

		// Get file info
		info, err := os.Stat(destPath)
		if err != nil {
			applog.Infof("[handler] processAttachments error getting file info %s: %v", file.Name(), err)
			continue
		}

		// Detect media type from extension
		mediaType := mediaTypeFromExtension(file.Name())

		// Create database record
		attachment := &models.ChatAttachment{
			ExecutionID: execID,
			FileName:    file.Name(),
			FilePath:    destPath,
			MediaType:   mediaType,
			FileSize:    info.Size(),
		}

		if err := h.chatAttachmentRepo.Create(ctx, attachment); err != nil {
			applog.Infof("[handler] processAttachmentsWithReturn error creating attachment record: %v", err)
			continue
		}

		// Add to chatAttachments list
		chatAttachments = append(chatAttachments, *attachment)

		if isImageFile(file.Name()) {
			// Image files: pass as multimodal attachments for the API to handle natively,
			// instead of reading binary content as text (which causes "prompt too long" errors)
			imageAttachments = append(imageAttachments, models.Attachment{
				FileName:  file.Name(),
				FilePath:  destPath,
				MediaType: mediaType,
				FileSize:  info.Size(),
			})
			applog.Infof("[handler] processAttachmentsWithReturn image attachment id=%s file=%s size=%d", attachment.ID, file.Name(), info.Size())
		} else if info.Size() <= maxTextAttachmentSize {
			// Text files within size limit: read content and include in prompt context
			content, readErr := os.ReadFile(destPath)
			if readErr != nil {
				applog.Infof("[handler] processAttachmentsWithReturn error reading file %s: %v", file.Name(), readErr)
				continue
			}
			attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s\n```\n%s\n```\n", file.Name(), string(content)))
			applog.Infof("[handler] processAttachmentsWithReturn text attachment id=%s file=%s size=%d", attachment.ID, file.Name(), info.Size())
		} else {
			// Large text files: mention but don't include content to avoid prompt overflow
			attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s (attached, %d bytes - too large to include inline)\n", file.Name(), info.Size()))
			applog.Infof("[handler] processAttachmentsWithReturn large file id=%s file=%s size=%d (skipped content)", attachment.ID, file.Name(), info.Size())
		}
	}

	// Clean up pending directory
	os.RemoveAll(pendingDir)

	var textContext string
	if len(attachmentContents) > 0 {
		textContext = "\n\n--- Attached Files ---\n" + strings.Join(attachmentContents, "")
	}

	return textContext, imageAttachments, chatAttachments, nil
}

func (h *Handler) previewPendingAttachments(sessionID string) (string, []models.Attachment, error) {
	pendingDir := filepath.Join(uploadsDir, "chat", "pending", sessionID)
	if _, err := os.Stat(pendingDir); os.IsNotExist(err) {
		return "", nil, nil
	}
	files, err := os.ReadDir(pendingDir)
	if err != nil {
		return "", nil, fmt.Errorf("reading pending directory: %w", err)
	}
	var attachmentContents []string
	var imageAttachments []models.Attachment
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		path := filepath.Join(pendingDir, file.Name())
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, fmt.Errorf("getting file info %s: %w", file.Name(), err)
		}
		mediaType := mediaTypeFromExtension(file.Name())
		if isImageFile(file.Name()) {
			imageAttachments = append(imageAttachments, models.Attachment{
				FileName:  file.Name(),
				FilePath:  path,
				MediaType: mediaType,
				FileSize:  info.Size(),
			})
		} else if info.Size() <= maxTextAttachmentSize {
			content, err := os.ReadFile(path)
			if err != nil {
				return "", nil, fmt.Errorf("reading file %s: %w", file.Name(), err)
			}
			attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s\n```\n%s\n```\n", file.Name(), string(content)))
		} else {
			attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s (attached, %d bytes - too large to include inline)\n", file.Name(), info.Size()))
		}
	}
	var textContext string
	if len(attachmentContents) > 0 {
		textContext = "\n\n--- Attached Files ---\n" + strings.Join(attachmentContents, "")
	}
	return textContext, imageAttachments, nil
}

func (h *Handler) ClearChat(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	applog.Infof("[handler] ClearChat project=%s", projectID)

	// Cancel any running chat goroutines before deleting.
	// Without this, running goroutines continue processing with old conversation
	// history in memory, and their responses may appear stale or confusing.
	if h.workerSvc != nil {
		runningIDs, _ := h.taskRepo.ListRunningChatTaskIDs(c.Request().Context(), projectID)
		for _, id := range runningIDs {
			applog.Infof("[handler] ClearChat cancelling running chat task=%s", id)
			h.workerSvc.CancelRunningTask(id)
		}
	}
	if h.threadInputRepo != nil {
		if err := h.threadInputRepo.CancelPendingForChat(c.Request().Context(), projectID); err != nil {
			applog.Infof("[handler] ClearChat error cancelling pending chat inputs: %v", err)
			return err
		}
	}

	count, err := h.taskSvc.DeleteAllChat(c.Request().Context(), projectID)
	if err != nil {
		applog.Infof("[handler] ClearChat error: %v", err)
		return err
	}
	applog.Infof("[handler] ClearChat deleted %d chat tasks", count)

	// Return updated chat content
	agents, err := h.llmConfigRepo.List(c.Request().Context())
	if err != nil {
		applog.Infof("[handler] ClearChat error listing agents: %v", err)
		return err
	}

	// Return empty chat content
	return render(c, http.StatusOK, pages.ChatContent(agents, []models.Execution{}, projectID, make(map[string][]models.ChatAttachment), []models.ThreadInput{}, false, false, chatUIWindowLimitDefault))
}

// chatHistoryHasPlanCompletion checks if the latest completed assistant response
// in the chat history contains a <proposed_plan> block, indicating a plan-mode
// completion that should show the "Switch to Orchestrate" CTA.
func chatHistoryHasPlanCompletion(history []models.Execution) bool {
	for i := len(history) - 1; i >= 0; i-- {
		exec := history[i]
		if exec.Status == models.ExecCompleted && exec.Output != "" {
			return strings.Contains(exec.Output, "<proposed_plan>")
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// copyChatAttachmentsToTask copies attachments from a chat execution to a task.
// It also appends an attachment reference to the task's prompt so the executing agent
// knows about the attached files. Returns the number of attachments copied and any error.
func (h *Handler) copyChatAttachmentsToTask(ctx context.Context, executionID, taskID string) (int, error) {
	// Get chat attachments for this execution
	chatAttachments, err := h.chatAttachmentRepo.ListByExecution(ctx, executionID)
	if err != nil {
		return 0, fmt.Errorf("listing chat attachments: %w", err)
	}

	if len(chatAttachments) == 0 {
		return 0, nil
	}

	// Create task-specific directory
	taskDir := filepath.Join(uploadsDir, "tasks", taskID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return 0, fmt.Errorf("creating task directory: %w", err)
	}

	copiedCount := 0
	var copiedFileNames []string
	for _, chatAtt := range chatAttachments {
		// Copy file from chat directory to task directory
		srcPath := chatAtt.FilePath
		destPath := filepath.Join(taskDir, chatAtt.FileName)

		// Read source file
		data, err := os.ReadFile(srcPath)
		if err != nil {
			applog.Infof("[handler] copyChatAttachmentsToTask error reading file %s: %v", srcPath, err)
			continue
		}

		// Write to destination
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			applog.Infof("[handler] copyChatAttachmentsToTask error writing file %s: %v", destPath, err)
			continue
		}

		// Create task attachment record
		taskAttachment := &models.Attachment{
			TaskID:    taskID,
			FileName:  chatAtt.FileName,
			FilePath:  destPath,
			MediaType: chatAtt.MediaType,
			FileSize:  chatAtt.FileSize,
		}

		if err := h.attachmentRepo.Create(ctx, taskAttachment); err != nil {
			applog.Infof("[handler] copyChatAttachmentsToTask error creating attachment record: %v", err)
			// Clean up the copied file
			os.Remove(destPath)
			continue
		}

		applog.Infof("[handler] copyChatAttachmentsToTask copied attachment file=%s from exec=%s to task=%s", chatAtt.FileName, executionID, taskID)
		copiedCount++
		copiedFileNames = append(copiedFileNames, chatAtt.FileName)
	}

	// Append attachment context to the task prompt so the executing agent knows about the files.
	// Include absolute file paths so the agent can find them regardless of working directory.
	if copiedCount > 0 {
		task, getErr := h.taskRepo.GetByID(ctx, taskID)
		if getErr == nil && task != nil {
			var fileRefs []string
			for _, name := range copiedFileNames {
				absPath := filepath.Join(uploadsDir, "tasks", taskID, name)
				fileRefs = append(fileRefs, fmt.Sprintf("%s (path: %s)", name, absPath))
			}
			task.Prompt += fmt.Sprintf("\n\n[Attached files from chat:\n%s]", strings.Join(fileRefs, "\n"))
			if updateErr := h.taskRepo.Update(ctx, task); updateErr != nil {
				applog.Infof("[handler] copyChatAttachmentsToTask error updating task prompt: %v", updateErr)
			}
		}
	}

	return copiedCount, nil
}

// writeSSEData writes a potentially multi-line string as properly formatted SSE data.
// SSE spec requires each line to be prefixed with "data: ". The browser's EventSource
// automatically joins multiple "data:" lines with "\n" when firing onmessage.
func writeSSEData(c echo.Context, data string) {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		fmt.Fprintf(c.Response(), "data: %s\n", line)
	}
	fmt.Fprintf(c.Response(), "\n") // Empty line terminates the event
}

// ChatStreamSSE streams chat execution output via SSE
func (h *Handler) ChatStreamSSE(c echo.Context) error {
	execID := c.Param("exec_id")
	if execID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "exec_id is required")
	}

	applog.Infof("[handler] ChatStreamSSE exec=%s connected", execID)

	// Set headers for SSE
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	ctx := c.Request().Context()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastOutput := ""
	timeout := time.After(chatSSETimeout)

	for {
		select {
		case <-ctx.Done():
			applog.Infof("[handler] ChatStreamSSE exec=%s client disconnected", execID)
			return nil

		case <-timeout:
			applog.Infof("[handler] ChatStreamSSE exec=%s timeout", execID)
			fmt.Fprintf(c.Response(), "event: error\ndata: timeout\n\n")
			c.Response().Flush()
			return nil

		case <-ticker.C:
			// Get current execution state
			exec, err := h.execRepo.GetByID(ctx, execID)
			if err != nil {
				applog.Infof("[handler] ChatStreamSSE exec=%s error: %v", execID, err)
				fmt.Fprintf(c.Response(), "event: error\ndata: %s\n\n", err.Error())
				c.Response().Flush()
				return nil
			}

			if exec == nil {
				applog.Infof("[handler] ChatStreamSSE exec=%s not found", execID)
				fmt.Fprintf(c.Response(), "event: error\ndata: execution not found\n\n")
				c.Response().Flush()
				return nil
			}

			output := exec.Output

			// Send new output if changed
			if output != lastOutput && len(output) > len(lastOutput) {
				// Send only the delta (new content)
				delta := output[len(lastOutput):]
				// applog.Debugf("[handler] ChatStreamSSE exec=%s delta_len=%d delta=%q", execID, len(delta), delta)
				// SSE requires multi-line data to have each line prefixed with "data:".
				// Without this, content after the first newline is silently dropped
				// by the browser's EventSource parser.
				writeSSEData(c, delta)
				c.Response().Flush()
				lastOutput = output
			} else if output != lastOutput {
				// Output was modified (not just appended) — update tracking
				lastOutput = output
			}

			// Check if execution is complete
			if exec.Status == models.ExecCompleted {
				// applog.Debugf("[handler] ChatStreamSSE exec=%s completed total_output_len=%d total_output=%q", execID, len(exec.Output), exec.Output)
				fmt.Fprintf(c.Response(), "event: done\ndata: completed\n\n")
				c.Response().Flush()
				return nil
			} else if exec.Status == models.ExecFailed {
				applog.Infof("[handler] ChatStreamSSE exec=%s failed: %s", execID, exec.ErrorMessage)
				fmt.Fprintf(c.Response(), "event: error\ndata: %s\n\n", exec.ErrorMessage)
				c.Response().Flush()
				return nil
			}
		}
	}
}
