package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/events"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

type channelChatAttachmentLinkOptions struct {
	Platform     string
	UploadsDir   string
	Repo         *repository.ChatAttachmentRepo
	FallbackName string
}

type channelChatIngressQueueOptions struct {
	Platform                string
	ProjectID               string
	ActiveExecID            string
	AgentID                 string
	Message                 string
	Source                  string
	AttachmentSessionID     string
	UploadsDir              string
	BroadcastHasAttachments bool

	ThreadInputRepo *repository.ThreadInputRepo
	ChatBroadcaster *events.ChatBroadcaster

	NewThreadInput func() *models.ThreadInput
	OnQueueFailure func(context.Context)
	OnQueued       func(context.Context)
}

type channelChatIngressFirstTurnOptions struct {
	Platform          string
	ProjectID         string
	Message           string
	Source            string
	Task              *models.Task
	Agent             *models.LLMConfig
	Attachments       []models.ChatAttachment
	AttachmentContext string
	ImageAttachments  []models.Attachment
	HasAttachments    bool
	Surface           chatcontrol.Surface
	ReplyContext      ChannelReplyContext
	InitialAckID      int
	Start             time.Time
	ChatHistoryLimit  int

	TaskRepo           *repository.TaskRepo
	ExecRepo           *repository.ExecutionRepo
	ChatAttachmentRepo *repository.ChatAttachmentRepo
	ChatBroadcaster    *events.ChatBroadcaster
	ChannelChatRunner  ChannelChatRunner

	CreateTaskContext          func(context.Context, string) error
	CompleteExecution          func(context.Context, string, string, string, string, int, int64)
	LinkAttachments            func(context.Context, string, []models.ChatAttachment) ([]models.ChatAttachment, error)
	AttachmentContextAndImages func([]models.ChatAttachment) (string, []models.Attachment)
	ListChatHistory            func(context.Context, string) ([]models.Execution, error)
	FilterChatHistory          func([]models.Execution, string) []models.Execution
	BuildChatContext           func(context.Context, string) string
	GetPersonalityContext      func(context.Context, string) string
	ResolveWorkDir             func(context.Context, string) string
	OnTaskCreateFailure        func(context.Context)
	OnTaskContextFailure       func(context.Context)
	OnExecutionCreateFailure   func(context.Context)
	OnAttachmentLinkFailure    func(context.Context, string)
	PrepareRunner              func(context.Context, string, string) int
	OnRunnerUnavailable        func(context.Context, string, int)
}

type channelChatIngressDownloadResult struct {
	AttachmentContext string
	ImageAttachments  []models.Attachment
	ChatAttachments   []models.ChatAttachment
}

type channelChatIngressOptions struct {
	Platform       string
	ProjectID      string
	Message        string
	Source         string
	Surface        chatcontrol.Surface
	HasAttachments bool
	Start          time.Time

	TaskRepo        *repository.TaskRepo
	ExecRepo        *repository.ExecutionRepo
	ThreadInputRepo *repository.ThreadInputRepo
	LLMConfigRepo   *repository.LLMConfigRepo
	ChatBroadcaster *events.ChatBroadcaster
	UploadsDir      string

	DownloadAttachments                       func(context.Context) (channelChatIngressDownloadResult, error)
	ContinueWithoutAttachmentsOnDownloadError bool
	IncomingAttachmentsNeedVision             func() bool
	SavePendingAttachments                    func([]models.ChatAttachment) (string, error)
	SelectionMessage                          string
	FindActiveExecution                       func(context.Context, string) (*models.Execution, error)
	RecordAttachmentFailure                   func(context.Context, string, string)
	NewQueuedInput                            func() *models.ThreadInput
	AttachmentDownloadFailureMessage          func(error, bool) string
	OnAttachmentDownloadFailed                func(context.Context, string)
	OnQueuedAttachmentDownloadFailed          func(context.Context, string)
	OnAttachmentStoreFailed                   func(context.Context, string)
	OnModelSelectionFailed                    func(context.Context, error)
	OnActiveLookupFailed                      func(context.Context)
	OnQueueFailure                            func(context.Context)
	OnQueued                                  func(context.Context)
	FirstTurn                                 channelChatIngressFirstTurnOptions
}

func (opts channelChatIngressOptions) selectionPrompt() string {
	if strings.TrimSpace(opts.SelectionMessage) != "" {
		return opts.SelectionMessage
	}
	return opts.Message
}

func selectChannelChatAgent(ctx context.Context, repo *repository.LLMConfigRepo, message string, hasImages bool) (*models.LLMConfig, error) {
	if repo == nil {
		return nil, fmt.Errorf("no model repository configured")
	}
	agents, err := repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents configured")
	}
	complexity := AnalyzeComplexity(message)
	if result := SelectLLMWithVision(complexity, agents, hasImages); result != nil {
		return result.LLMConfig, nil
	}
	for i := range agents {
		if agents[i].IsDefault {
			return &agents[i], nil
		}
	}
	return &agents[0], nil
}

func runChannelChatIngress(ctx context.Context, opts channelChatIngressOptions) bool {
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = "channel"
	}
	var activeChatExec *models.Execution
	if opts.FindActiveExecution != nil {
		var activeErr error
		activeChatExec, activeErr = opts.FindActiveExecution(ctx, opts.ProjectID)
		if activeErr != nil {
			applog.Infof("[%s] error checking active chat turn: %v", platform, activeErr)
			if opts.OnActiveLookupFailed != nil {
				opts.OnActiveLookupFailed(ctx)
			}
			return true
		}
	}

	attachmentContext := ""
	imageAttachments := []models.Attachment{}
	chatAttachments := []models.ChatAttachment{}
	if opts.DownloadAttachments != nil {
		downloaded, err := opts.DownloadAttachments(ctx)
		if err != nil {
			applog.Infof("[%s] attachment download error: %v", platform, err)
			msgText := fmt.Sprintf("Failed to process attachment: %v", err)
			if opts.AttachmentDownloadFailureMessage != nil {
				msgText = opts.AttachmentDownloadFailureMessage(err, activeChatExec != nil)
			} else if activeChatExec != nil {
				msgText = "Failed to process attachment: unable to download attachment. Please try again."
			}
			if activeChatExec != nil && opts.OnQueuedAttachmentDownloadFailed != nil {
				opts.OnQueuedAttachmentDownloadFailed(ctx, msgText)
			} else if opts.OnAttachmentDownloadFailed != nil {
				opts.OnAttachmentDownloadFailed(ctx, msgText)
			}
			if opts.ContinueWithoutAttachmentsOnDownloadError {
				// Preserve adapters such as Telegram that currently warn but continue the text turn.
			} else {
				agent, agentErr := selectChannelChatAgent(ctx, opts.LLMConfigRepo, opts.selectionPrompt(), opts.IncomingAttachmentsNeedVision != nil && opts.IncomingAttachmentsNeedVision())
				if agentErr != nil {
					if opts.OnModelSelectionFailed != nil {
						opts.OnModelSelectionFailed(ctx, agentErr)
					}
					return true
				}
				if opts.RecordAttachmentFailure != nil {
					opts.RecordAttachmentFailure(ctx, agent.ID, msgText)
				}
				return true
			}
		}
		attachmentContext = downloaded.AttachmentContext
		imageAttachments = downloaded.ImageAttachments
		chatAttachments = downloaded.ChatAttachments
	}

	agent, err := selectChannelChatAgent(ctx, opts.LLMConfigRepo, opts.selectionPrompt(), len(imageAttachments) > 0)
	if err != nil {
		cleanupChannelChatAttachmentSourceDirs(chatAttachments)
		if opts.OnModelSelectionFailed != nil {
			opts.OnModelSelectionFailed(ctx, err)
		}
		return true
	}

	if activeChatExec != nil {
		attachmentSessionID := ""
		if len(chatAttachments) > 0 {
			if opts.SavePendingAttachments == nil {
				cleanupChannelChatAttachmentSourceDirs(chatAttachments)
				if opts.OnAttachmentStoreFailed != nil {
					opts.OnAttachmentStoreFailed(ctx, "Failed to process attachment: unable to store attachment. Please try again.")
				}
				return true
			}
			var stageErr error
			attachmentSessionID, stageErr = opts.SavePendingAttachments(chatAttachments)
			if stageErr != nil {
				applog.Infof("[%s] queue chat attachment staging failed: %v", platform, stageErr)
				msgText := "Failed to process attachment: unable to store attachment. Please try again."
				if opts.RecordAttachmentFailure != nil {
					opts.RecordAttachmentFailure(ctx, agent.ID, msgText)
				}
				if opts.OnAttachmentStoreFailed != nil {
					opts.OnAttachmentStoreFailed(ctx, msgText)
				}
				return true
			}
		}
		return runChannelChatQueuedInput(ctx, channelChatIngressQueueOptions{
			Platform:                platform,
			ProjectID:               opts.ProjectID,
			ActiveExecID:            activeChatExec.ID,
			AgentID:                 agent.ID,
			Message:                 opts.Message,
			Source:                  opts.Source,
			AttachmentSessionID:     attachmentSessionID,
			UploadsDir:              opts.UploadsDir,
			BroadcastHasAttachments: opts.HasAttachments,
			ThreadInputRepo:         opts.ThreadInputRepo,
			ChatBroadcaster:         opts.ChatBroadcaster,
			NewThreadInput:          opts.NewQueuedInput,
			OnQueueFailure:          opts.OnQueueFailure,
			OnQueued:                opts.OnQueued,
		})
	}

	first := opts.FirstTurn
	first.Platform = platform
	first.ProjectID = opts.ProjectID
	first.Message = opts.Message
	first.Source = opts.Source
	first.Agent = agent
	first.Attachments = chatAttachments
	first.AttachmentContext = attachmentContext
	first.ImageAttachments = imageAttachments
	first.HasAttachments = opts.HasAttachments || len(chatAttachments) > 0
	first.Surface = opts.Surface
	first.Start = opts.Start
	if first.Start.IsZero() {
		first.Start = time.Now()
	}
	if first.TaskRepo == nil {
		first.TaskRepo = opts.TaskRepo
	}
	if first.ExecRepo == nil {
		first.ExecRepo = opts.ExecRepo
	}
	if first.ChatBroadcaster == nil {
		first.ChatBroadcaster = opts.ChatBroadcaster
	}
	runChannelChatFirstTurn(ctx, first)
	return true
}

func runChannelChatQueuedInput(ctx context.Context, opts channelChatIngressQueueOptions) bool {
	if opts.ThreadInputRepo == nil {
		return false
	}
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = "channel"
	}
	queued := &models.ThreadInput{
		Scope:               models.ThreadInputScopeChat,
		ProjectID:           opts.ProjectID,
		RunExecutionID:      opts.ActiveExecID,
		AgentConfigID:       opts.AgentID,
		InputMode:           models.ThreadInputModeQueued,
		InputStatus:         models.ThreadInputPending,
		Content:             opts.Message,
		AttachmentSessionID: opts.AttachmentSessionID,
		ChatMode:            models.ChatModeOrchestrate,
		Source:              opts.Source,
	}
	if opts.NewThreadInput != nil {
		custom := opts.NewThreadInput()
		if custom != nil {
			queued = custom
			queued.Scope = models.ThreadInputScopeChat
			queued.ProjectID = opts.ProjectID
			queued.RunExecutionID = opts.ActiveExecID
			queued.AgentConfigID = opts.AgentID
			queued.InputMode = models.ThreadInputModeQueued
			queued.InputStatus = models.ThreadInputPending
			queued.Content = opts.Message
			queued.AttachmentSessionID = opts.AttachmentSessionID
			queued.ChatMode = models.ChatModeOrchestrate
			queued.Source = opts.Source
		}
	}
	if err := opts.ThreadInputRepo.CreateQueued(ctx, queued); err != nil {
		applog.Infof("[%s] queue chat input failed: %v", platform, err)
		if opts.AttachmentSessionID != "" {
			_ = os.RemoveAll(filepath.Join(opts.UploadsDir, "chat", "pending", opts.AttachmentSessionID))
		}
		if opts.OnQueueFailure != nil {
			opts.OnQueueFailure(ctx)
		}
		return true
	}
	if opts.ChatBroadcaster != nil {
		opts.ChatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: opts.ProjectID, ExecID: queued.ID, Message: opts.Message, Source: opts.Source, Queued: true, HasAttachments: opts.BroadcastHasAttachments || opts.AttachmentSessionID != ""})
	}
	if opts.OnQueued != nil {
		opts.OnQueued(ctx)
	}
	return true
}

func runChannelChatFirstTurn(ctx context.Context, opts channelChatIngressFirstTurnOptions) (bool, []models.ChatAttachment) {
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = "channel"
	}
	if opts.TaskRepo == nil || opts.ExecRepo == nil || opts.Agent == nil || opts.Task == nil {
		applog.Infof("[%s] incoming message ignored: shared ingress dependencies are not fully configured", platform)
		cleanupChannelChatAttachmentSourceDirs(opts.Attachments)
		return false, nil
	}
	selectedAgentID := opts.Agent.ID
	opts.Task.ProjectID = opts.ProjectID
	opts.Task.Prompt = opts.Message
	opts.Task.Status = models.StatusPending
	opts.Task.Category = models.CategoryChat
	opts.Task.AgentID = &selectedAgentID
	if opts.Task.CreatedVia == "" {
		opts.Task.CreatedVia = opts.Source
	}
	if err := opts.TaskRepo.Create(ctx, opts.Task); err != nil {
		applog.Infof("[%s] create chat task failed: %v", platform, err)
		cleanupChannelChatAttachmentSourceDirs(opts.Attachments)
		if opts.OnTaskCreateFailure != nil {
			opts.OnTaskCreateFailure(ctx)
		}
		return true, nil
	}
	if opts.CreateTaskContext != nil {
		if err := opts.CreateTaskContext(ctx, opts.Task.ID); err != nil {
			applog.Infof("[%s] create chat context failed task=%s: %v", platform, opts.Task.ID, err)
			cleanupChannelChatAttachmentSourceDirs(opts.Attachments)
			if delErr := opts.TaskRepo.Delete(ctx, opts.Task.ID); delErr != nil {
				applog.Infof("[%s] cleanup chat task failed task=%s: %v", platform, opts.Task.ID, delErr)
			}
			if opts.OnTaskContextFailure != nil {
				opts.OnTaskContextFailure(ctx)
			}
			return true, nil
		}
	}
	exec := &models.Execution{TaskID: opts.Task.ID, AgentConfigID: opts.Agent.ID, Status: models.ExecRunning, PromptSent: opts.Message}
	if err := opts.ExecRepo.Create(ctx, exec); err != nil {
		applog.Infof("[%s] create execution failed: %v", platform, err)
		cleanupChannelChatAttachmentSourceDirs(opts.Attachments)
		if delErr := opts.TaskRepo.Delete(ctx, opts.Task.ID); delErr != nil {
			applog.Infof("[%s] cleanup chat task failed task=%s after execution create failure: %v", platform, opts.Task.ID, delErr)
		}
		if opts.OnExecutionCreateFailure != nil {
			opts.OnExecutionCreateFailure(ctx)
		}
		return true, nil
	}

	linkedAttachments := opts.Attachments
	hasLinkedAttachments := false
	if len(linkedAttachments) > 0 {
		linkFn := opts.LinkAttachments
		if linkFn == nil {
			linkFn = func(ctx context.Context, execID string, atts []models.ChatAttachment) ([]models.ChatAttachment, error) {
				return linkChannelChatAttachmentsToExecution(ctx, execID, atts, channelChatAttachmentLinkOptions{Platform: platform, Repo: opts.ChatAttachmentRepo})
			}
		}
		var err error
		linkedAttachments, err = linkFn(ctx, exec.ID, linkedAttachments)
		if err != nil {
			applog.Infof("[%s] attachment link error: %v", platform, err)
			msgText := "Failed to process attachment: unable to store attachment. Please try again."
			if opts.CompleteExecution != nil {
				opts.CompleteExecution(ctx, exec.ID, opts.Task.ID, "", msgText, 0, time.Since(opts.Start).Milliseconds())
			}
			if opts.ChatBroadcaster != nil {
				opts.ChatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: opts.ProjectID, ExecID: exec.ID, TaskID: opts.Task.ID, Message: opts.Message, Source: opts.Source, AgentName: opts.Agent.Name, HasAttachments: opts.HasAttachments || len(opts.Attachments) > 0})
			}
			if opts.OnAttachmentLinkFailure != nil {
				opts.OnAttachmentLinkFailure(ctx, msgText)
			}
			return true, nil
		}
		hasLinkedAttachments = true
		if opts.AttachmentContextAndImages != nil {
			opts.AttachmentContext, opts.ImageAttachments = opts.AttachmentContextAndImages(linkedAttachments)
		}
	}

	if opts.ChatBroadcaster != nil {
		opts.ChatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: opts.ProjectID, ExecID: exec.ID, TaskID: opts.Task.ID, Message: opts.Message, Source: opts.Source, AgentName: opts.Agent.Name, HasAttachments: hasLinkedAttachments})
	}

	history := []models.Execution{}
	if opts.ListChatHistory != nil {
		var err error
		history, err = opts.ListChatHistory(ctx, opts.ProjectID)
		if err != nil {
			history = []models.Execution{}
		}
	}
	if opts.FilterChatHistory != nil {
		history = opts.FilterChatHistory(history, exec.ID)
	}
	systemContext := ""
	if opts.BuildChatContext != nil {
		systemContext = opts.BuildChatContext(ctx, opts.ProjectID)
	}
	if opts.AttachmentContext != "" {
		systemContext += opts.AttachmentContext
	}
	if opts.GetPersonalityContext != nil {
		if personalityPrompt := opts.GetPersonalityContext(ctx, opts.ProjectID); personalityPrompt != "" {
			systemContext += personalityPrompt
		}
	}
	workDir := ""
	if opts.ResolveWorkDir != nil {
		workDir = opts.ResolveWorkDir(ctx, opts.ProjectID)
	}
	initialAckID := opts.InitialAckID
	if opts.PrepareRunner != nil {
		initialAckID = opts.PrepareRunner(ctx, opts.Task.ID, exec.ID)
	}
	if opts.ChannelChatRunner == nil {
		msgText := channelChatAttachmentDisplayPlatform(platform) + " chat runner is unavailable. Please restart OpenVibely and try again."
		if opts.CompleteExecution != nil {
			opts.CompleteExecution(ctx, exec.ID, opts.Task.ID, "", msgText, 0, time.Since(opts.Start).Milliseconds())
		}
		if opts.OnRunnerUnavailable != nil {
			opts.OnRunnerUnavailable(ctx, msgText, initialAckID)
		}
		return true, linkedAttachments
	}
	opts.ChannelChatRunner(context.Background(), ChannelChatRunRequest{
		ExecID:              exec.ID,
		TaskID:              opts.Task.ID,
		ProjectID:           opts.ProjectID,
		Message:             opts.Message,
		Agent:               *opts.Agent,
		ChatHistory:         history,
		SystemContext:       systemContext,
		WorkDir:             workDir,
		ImageAttachments:    opts.ImageAttachments,
		Surface:             opts.Surface,
		InitialAckMessageID: initialAckID,
		ReplyContext:        opts.ReplyContext,
	})
	return true, linkedAttachments
}

func channelChatAttachmentContextAndImages(chatAttachments []models.ChatAttachment, maxTextFileSize int64) (string, []models.Attachment) {
	var imageAttachments []models.Attachment
	var attachmentContents []string
	for _, att := range chatAttachments {
		if isSlackImageFile(att.MediaType) {
			imageAttachments = append(imageAttachments, models.Attachment{
				FileName:  att.FileName,
				FilePath:  att.FilePath,
				MediaType: att.MediaType,
				FileSize:  att.FileSize,
			})
			continue
		}
		if att.FileSize <= maxTextFileSize {
			content, readErr := os.ReadFile(att.FilePath)
			if readErr == nil {
				attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s\n```\n%s\n```\n", att.FileName, string(content)))
				continue
			}
		}
		attachmentContents = append(attachmentContents, fmt.Sprintf("\nFile: %s (attached, %d bytes - too large to include inline)\n", att.FileName, att.FileSize))
	}
	attachmentContext := ""
	if len(attachmentContents) > 0 {
		attachmentContext = "\n\n--- Attached Files ---\n" + strings.Join(attachmentContents, "")
	}
	return attachmentContext, imageAttachments
}

func saveChannelChatAttachmentsToPendingSession(uploadsDir, fallbackName string, attachments []models.ChatAttachment) (string, error) {
	if len(attachments) == 0 {
		return "", nil
	}
	sessionID := generateSlackPendingSessionID()
	sessionDir := filepath.Join(uploadsDir, "chat", "pending", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return "", fmt.Errorf("creating pending upload directory: %w", err)
	}
	cleanupDirs := make(map[string]struct{})
	for _, att := range attachments {
		fileName := safeChannelChatAttachmentFileName(att.FileName, fallbackName)
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
		destPath := filepath.Join(sessionDir, fmt.Sprintf("%s_%s", generateSlackPendingSessionID()[:8], fileName))
		if err := moveOrCopyFile(att.FilePath, destPath); err != nil {
			_ = os.RemoveAll(sessionDir)
			cleanupChannelChatAttachmentDirs(cleanupDirs)
			return "", fmt.Errorf("staging %s: %w", fileName, err)
		}
	}
	cleanupChannelChatAttachmentDirs(cleanupDirs)
	return sessionID, nil
}

func linkChannelChatAttachmentsToExecution(ctx context.Context, execID string, attachments []models.ChatAttachment, opts channelChatAttachmentLinkOptions) ([]models.ChatAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = "channel"
	}
	if opts.Repo == nil {
		cleanupChannelChatAttachmentSourceDirs(attachments)
		return nil, fmt.Errorf("chat attachment repository is unavailable")
	}
	displayPlatform := channelChatAttachmentDisplayPlatform(platform)
	execDir := filepath.Join(opts.UploadsDir, "chat", execID)
	if err := os.MkdirAll(execDir, 0755); err != nil {
		applog.Infof("[%s] error creating exec dir %s: %v", platform, execDir, err)
		cleanupChannelChatAttachmentSourceDirs(attachments)
		return nil, fmt.Errorf("storing %s attachment: %w", displayPlatform, err)
	}
	cleanupDirs := make(map[string]struct{})
	linked := make([]models.ChatAttachment, 0, len(attachments))
	var linkErrs []string
	for i := range attachments {
		att := &attachments[i]
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
		destPath := filepath.Join(execDir, uniqueSlackTempFilename(execDir, safeChannelChatAttachmentFileName(att.FileName, opts.FallbackName)))
		if err := moveOrCopyFile(att.FilePath, destPath); err != nil {
			applog.Infof("[%s] error moving attachment file=%s: %v", platform, att.FileName, err)
			linkErrs = append(linkErrs, fmt.Sprintf("%s: %v", att.FileName, err))
			continue
		}
		absPath, err := filepath.Abs(destPath)
		if err != nil {
			absPath = destPath
		}
		att.FilePath = absPath
		att.ExecutionID = execID
		if err := opts.Repo.Create(ctx, att); err != nil {
			applog.Infof("[%s] error creating chat attachment record: %v", platform, err)
			_ = os.Remove(destPath)
			linkErrs = append(linkErrs, fmt.Sprintf("%s: %v", att.FileName, err))
		} else {
			linked = append(linked, *att)
			applog.Infof("[%s] linked attachment id=%s file=%s to exec=%s", platform, att.ID, att.FileName, execID)
		}
	}
	cleanupChannelChatAttachmentDirs(cleanupDirs)
	if len(linkErrs) > 0 {
		cleanupLinkedChannelChatAttachments(ctx, opts.Repo, platform, linked)
		return nil, fmt.Errorf("storing %s attachment failed for %d of %d file(s): %s", displayPlatform, len(linkErrs), len(attachments), strings.Join(linkErrs, "; "))
	}
	return linked, nil
}

func updateChannelChatImageAttachmentPaths(imageAttachments []models.Attachment, chatAttachments []models.ChatAttachment) []models.Attachment {
	for i := range imageAttachments {
		for _, ca := range chatAttachments {
			if ca.FileName == imageAttachments[i].FileName {
				imageAttachments[i].FilePath = ca.FilePath
				break
			}
		}
	}
	return imageAttachments
}

func cleanupLinkedChannelChatAttachments(ctx context.Context, repo *repository.ChatAttachmentRepo, platform string, attachments []models.ChatAttachment) {
	for _, att := range attachments {
		if strings.TrimSpace(att.ID) != "" && repo != nil {
			if err := repo.Delete(ctx, att.ID); err != nil {
				applog.Infof("[%s] error deleting partial chat attachment record id=%s: %v", platform, att.ID, err)
			}
		}
		if strings.TrimSpace(att.FilePath) != "" {
			if err := os.Remove(att.FilePath); err != nil && !os.IsNotExist(err) {
				applog.Infof("[%s] error deleting partial chat attachment file=%s: %v", platform, att.FilePath, err)
			}
		}
	}
}

func cleanupChannelChatAttachmentSourceDirs(attachments []models.ChatAttachment) {
	cleanupDirs := make(map[string]struct{})
	for _, att := range attachments {
		if strings.TrimSpace(att.FilePath) == "" {
			continue
		}
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
	}
	cleanupChannelChatAttachmentDirs(cleanupDirs)
}

func cleanupChannelChatAttachmentDirs(dirs map[string]struct{}) {
	for dir := range dirs {
		_ = os.RemoveAll(dir)
	}
}

func safeChannelChatAttachmentFileName(name, fallbackName string) string {
	fileName := filepath.Base(name)
	if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
		fileName = strings.TrimSpace(fallbackName)
		if fileName == "" {
			fileName = "channel-attachment"
		}
	}
	return fileName
}

func channelChatAttachmentDisplayPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return "Channel"
	}
	return strings.ToUpper(platform[:1]) + platform[1:]
}
