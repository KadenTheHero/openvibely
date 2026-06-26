package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/events"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	llmoutput "github.com/openvibely/openvibely/internal/llm/output"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/util"
)

const (
	DiscordSettingBotToken             = "discord_bot_token"
	DiscordSettingBotUserID            = "discord_bot_user_id"
	DiscordSettingDefaultChannelID     = "discord_default_channel_id"
	DiscordSettingSendResponses        = "discord_send_responses"
	DiscordSettingRequireMention       = "discord_require_mention"
	DiscordSettingFreeResponseChannels = "discord_free_response_channels"

	discordProcessTimeout   = 5 * time.Minute
	discordChatHistoryLimit = 50
	discordMessageLimit     = 2000
)

var discordMentionRegex = regexp.MustCompile(`<@!?[0-9]+>`)

type DiscordConnectionStatus struct {
	Configured       bool
	Connected        bool
	Running          bool
	BotUserID        string
	DefaultChannelID string
	SendResponses    bool
	RequireMention   bool
	HasBotToken      bool
}

type DiscordService struct {
	session                  *discordgo.Session
	settingsRepo             *repository.SettingsRepo
	discordAuthRepo          *repository.DiscordAuthRepo
	discordTaskContextRepo   *repository.DiscordTaskContextRepo
	projectRepo              *repository.ProjectRepo
	llmConfigRepo            *repository.LLMConfigRepo
	taskRepo                 *repository.TaskRepo
	execRepo                 *repository.ExecutionRepo
	scheduleRepo             *repository.ScheduleRepo
	taskSvc                  *TaskService
	taskGoalSvc              *TaskGoalService
	llmSvc                   *LLMService
	workerSvc                *WorkerService
	threadInputRepo          *repository.ThreadInputRepo
	customPersonalityRepo    *repository.CustomPersonalityRepo
	agentRepo                *repository.AgentRepo
	alertSvc                 *AlertService
	chatBroadcaster          *events.ChatBroadcaster
	queuedTurnPromoter       func(projectID string)
	queuedTaskThreadPromoter func(taskID string)
	channelChatRunner        ChannelChatRunner
	channelTaskRunner        ChannelTaskRunner
	userProjects             map[string]string

	mu                       sync.RWMutex
	running                  bool
	ctx                      context.Context
	cancel                   context.CancelFunc
	sendMessageFunc          func(channelID, messageID, text string) (string, error)
	processIncomingMessageFn func(msg discordIncomingMessage)
}

func NewDiscordService(
	settingsRepo *repository.SettingsRepo,
	projectRepo *repository.ProjectRepo,
	llmConfigRepo *repository.LLMConfigRepo,
	taskRepo *repository.TaskRepo,
	execRepo *repository.ExecutionRepo,
	scheduleRepo *repository.ScheduleRepo,
	taskSvc *TaskService,
	llmSvc *LLMService,
	workerSvc *WorkerService,
	discordAuthRepo *repository.DiscordAuthRepo,
	discordTaskContextRepo *repository.DiscordTaskContextRepo,
) *DiscordService {
	return &DiscordService{
		settingsRepo:           settingsRepo,
		projectRepo:            projectRepo,
		llmConfigRepo:          llmConfigRepo,
		taskRepo:               taskRepo,
		execRepo:               execRepo,
		scheduleRepo:           scheduleRepo,
		taskSvc:                taskSvc,
		llmSvc:                 llmSvc,
		workerSvc:              workerSvc,
		discordAuthRepo:        discordAuthRepo,
		discordTaskContextRepo: discordTaskContextRepo,
		userProjects:           make(map[string]string),
	}
}

func (s *DiscordService) SetChatBroadcaster(cb *events.ChatBroadcaster) { s.chatBroadcaster = cb }
func (s *DiscordService) SetThreadInputRepo(repo *repository.ThreadInputRepo) {
	s.threadInputRepo = repo
}
func (s *DiscordService) SetCustomPersonalityRepo(repo *repository.CustomPersonalityRepo) {
	s.customPersonalityRepo = repo
}
func (s *DiscordService) SetAgentRepo(repo *repository.AgentRepo) { s.agentRepo = repo }
func (s *DiscordService) SetAlertService(svc *AlertService)       { s.alertSvc = svc }
func (s *DiscordService) SetTaskGoalService(svc *TaskGoalService) { s.taskGoalSvc = svc }
func (s *DiscordService) SetQueuedTurnPromoter(promoter func(projectID string)) {
	s.queuedTurnPromoter = promoter
}
func (s *DiscordService) SetQueuedTaskThreadPromoter(promoter func(taskID string)) {
	s.queuedTaskThreadPromoter = promoter
}
func (s *DiscordService) SetChannelChatRunner(runner ChannelChatRunner) { s.channelChatRunner = runner }
func (s *DiscordService) SetChannelTaskRunner(runner ChannelTaskRunner) { s.channelTaskRunner = runner }

func (s *DiscordService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *DiscordService) Start() error {
	botToken := strings.TrimSpace(s.getSetting(context.Background(), DiscordSettingBotToken))
	if botToken == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return fmt.Errorf("create discord session: %w", err)
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	session.AddHandler(func(sess *discordgo.Session, msg *discordgo.MessageCreate) {
		s.handleMessageCreate(context.Background(), sess, msg)
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.session = session
	s.ctx = ctx
	s.cancel = cancel
	if err := session.Open(); err != nil {
		cancel()
		s.session = nil
		return fmt.Errorf("open discord gateway: %w", err)
	}
	if session.State != nil && session.State.User != nil && strings.TrimSpace(session.State.User.ID) != "" {
		_ = s.setSetting(context.Background(), DiscordSettingBotUserID, session.State.User.ID)
	}
	s.running = true
	applog.Infof("[discord] gateway started")
	go func() {
		<-ctx.Done()
	}()
	return nil
}

func (s *DiscordService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running && s.session == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.session != nil {
		_ = s.session.Close()
	}
	s.running = false
	s.session = nil
	applog.Infof("[discord] gateway stopped")
}

func (s *DiscordService) ReloadFromSettings(ctx context.Context) error {
	s.Stop()
	return s.Start()
}

func (s *DiscordService) Disconnect(ctx context.Context) error {
	s.Stop()
	_ = s.setSetting(ctx, DiscordSettingBotToken, "")
	_ = s.setSetting(ctx, DiscordSettingBotUserID, "")
	_ = s.setSetting(ctx, DiscordSettingDefaultChannelID, "")
	_ = s.setSetting(ctx, DiscordSettingSendResponses, "")
	_ = s.setSetting(ctx, DiscordSettingRequireMention, "")
	_ = s.setSetting(ctx, DiscordSettingFreeResponseChannels, "")
	return nil
}

func (s *DiscordService) GetConnectionStatus(ctx context.Context) (DiscordConnectionStatus, error) {
	botToken := strings.TrimSpace(s.getSetting(ctx, DiscordSettingBotToken))
	status := DiscordConnectionStatus{
		HasBotToken:      botToken != "",
		BotUserID:        strings.TrimSpace(s.getSetting(ctx, DiscordSettingBotUserID)),
		DefaultChannelID: strings.TrimSpace(s.getSetting(ctx, DiscordSettingDefaultChannelID)),
		SendResponses:    s.IsSendResponsesEnabled(ctx),
		RequireMention:   s.IsRequireMentionEnabled(ctx),
		Running:          s.IsRunning(),
	}
	status.Configured = status.HasBotToken
	status.Connected = status.HasBotToken
	return status, nil
}

func (s *DiscordService) TestConnection(ctx context.Context) error {
	botToken := strings.TrimSpace(s.getSetting(ctx, DiscordSettingBotToken))
	if botToken == "" {
		return fmt.Errorf("discord bot token is not configured")
	}
	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return fmt.Errorf("create discord session: %w", err)
	}
	user, err := session.User("@me")
	if err != nil {
		return fmt.Errorf("auth test failed: %w", err)
	}
	if user != nil && strings.TrimSpace(user.ID) != "" {
		_ = s.setSetting(ctx, DiscordSettingBotUserID, user.ID)
	}
	return nil
}

func (s *DiscordService) IsSendResponsesEnabled(ctx context.Context) bool {
	val := strings.TrimSpace(strings.ToLower(s.getSetting(ctx, DiscordSettingSendResponses)))
	if val == "" {
		return true
	}
	return val != "false"
}

func (s *DiscordService) IsRequireMentionEnabled(ctx context.Context) bool {
	val := strings.TrimSpace(strings.ToLower(s.getSetting(ctx, DiscordSettingRequireMention)))
	if val == "" {
		return true
	}
	return val != "false"
}

func (s *DiscordService) handleMessageCreate(ctx context.Context, sess *discordgo.Session, msg *discordgo.MessageCreate) {
	if msg == nil || msg.Message == nil || msg.Author == nil {
		return
	}
	botID := s.botUserID(ctx, sess)
	if msg.Author.ID == botID || msg.Author.Bot {
		return
	}
	incoming := discordIncomingMessage{
		ChannelID: msg.ChannelID,
		ThreadID:  discordThreadID(msg.Message),
		MessageID: msg.ID,
		GuildID:   msg.GuildID,
		UserID:    msg.Author.ID,
		Username:  discordDisplayName(msg.Author),
		Text:      strings.TrimSpace(msg.Content),
		IsDM:      discordIsDM(sess, msg.ChannelID, msg.GuildID),
		Source:    "discord",
	}
	if strings.TrimSpace(incoming.Text) == "" && len(msg.Attachments) > 0 {
		incoming.Text = discordAttachmentPrompt(msg.Attachments)
	}
	if strings.TrimSpace(incoming.Text) == "" {
		return
	}
	if !incoming.IsDM && s.requiresMentionForMessage(ctx, incoming) && !discordMentionsBot(msg.Mentions, botID) && !strings.Contains(incoming.Text, "<@"+botID+">") && !strings.Contains(incoming.Text, "<@!"+botID+">") {
		return
	}
	incoming.Text = sanitizeDiscordText(incoming.Text, botID)
	if strings.TrimSpace(incoming.Text) == "" {
		return
	}
	if s.processIncomingMessageFn != nil {
		s.processIncomingMessageFn(incoming)
		return
	}
	go s.processIncomingMessage(incoming)
}

type discordIncomingMessage struct {
	ChannelID string
	ThreadID  string
	MessageID string
	GuildID   string
	UserID    string
	Username  string
	Text      string
	IsDM      bool
	Source    string
}

func (s *DiscordService) processIncomingMessage(msg discordIncomingMessage) {
	if msg.ChannelID == "" || msg.UserID == "" || strings.TrimSpace(msg.Text) == "" {
		return
	}
	if s.taskRepo == nil || s.execRepo == nil || s.llmConfigRepo == nil || s.llmSvc == nil || s.taskSvc == nil || s.projectRepo == nil {
		applog.Infof("[discord] incoming message ignored: service dependencies are not fully configured")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), discordProcessTimeout)
	defer cancel()
	start := time.Now()
	projectID := s.getActiveProject(ctx, msg.UserID)
	if projectID == "" {
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "No active project found. Please create a project first in the web UI.")
		return
	}
	if !s.checkAuthorization(ctx, projectID, msg.UserID) {
		applog.Infof("[discord] unauthorized access blocked for user=%s project=%s", msg.UserID, projectID)
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "You are not authorized to use Discord access for this project. Contact the project owner to get access.")
		return
	}
	agent, err := s.autoSelectAgent(ctx, msg.Text)
	if err != nil {
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, fmt.Sprintf("Error selecting model: %v", err))
		return
	}
	if activeChatExec, activeErr := s.execRepo.FindLatestActiveChatExecution(ctx, projectID); activeErr != nil {
		applog.Infof("[discord] error checking active chat turn: %v", activeErr)
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "Error checking active chat response. Please try again.")
		return
	} else if activeChatExec != nil {
		if s.queueChatInput(ctx, projectID, activeChatExec.ID, agent.ID, msg) {
			return
		}
	}
	selectedAgentID := agent.ID
	chatTitle := fmt.Sprintf("Discord %s: %s", time.Now().Format("15:04:05.000"), util.Truncate(msg.Text, 47))
	task := &models.Task{
		ProjectID:  projectID,
		Title:      chatTitle,
		Prompt:     msg.Text,
		Status:     models.StatusPending,
		Category:   models.CategoryChat,
		AgentID:    &selectedAgentID,
		CreatedVia: models.TaskOriginDiscord,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		applog.Infof("[discord] create chat task failed: %v", err)
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "Error processing your message. Please try again.")
		return
	}
	if s.discordTaskContextRepo != nil {
		if err := s.discordTaskContextRepo.Upsert(ctx, &models.DiscordTaskContext{
			TaskID:           task.ID,
			DiscordChannelID: msg.replyChannelID(),
			DiscordThreadID:  msg.ThreadID,
			DiscordMessageID: msg.MessageID,
			DiscordUserID:    msg.UserID,
		}); err != nil {
			applog.Infof("[discord] create chat context failed task=%s: %v", task.ID, err)
			if delErr := s.taskRepo.Delete(ctx, task.ID); delErr != nil {
				applog.Infof("[discord] cleanup chat task failed task=%s: %v", task.ID, delErr)
			}
			_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "Error processing your message. Please try again.")
			return
		}
	}
	exec := &models.Execution{TaskID: task.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: msg.Text}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		applog.Infof("[discord] create execution failed: %v", err)
		if delErr := s.taskRepo.Delete(ctx, task.ID); delErr != nil {
			applog.Infof("[discord] cleanup chat task failed task=%s after execution create failure: %v", task.ID, delErr)
		}
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "Error processing your message. Please try again.")
		return
	}
	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, ExecID: exec.ID, TaskID: task.ID, Message: msg.Text, Source: msg.Source, AgentName: agent.Name})
	}
	ackID, _ := s.sendDiscordMessageWithID(msg.replyChannelID(), msg.MessageID, "Thinking...")
	history, err := s.execRepo.ListChatHistory(ctx, projectID, discordChatHistoryLimit)
	if err != nil {
		history = []models.Execution{}
	}
	systemContext := s.buildChatContext(ctx, projectID)
	if personalityPrompt := s.getPersonalityContext(ctx, projectID); personalityPrompt != "" {
		systemContext += personalityPrompt
	}
	if s.channelChatRunner == nil {
		msgText := "Discord chat runner is unavailable. Please restart OpenVibely and try again."
		s.completeExecution(ctx, exec.ID, task.ID, "", msgText, 0, time.Since(start).Milliseconds())
		_ = s.editOrSendDiscordMessage(msg.replyChannelID(), ackID, msg.MessageID, msgText)
		return
	}
	s.channelChatRunner(context.Background(), ChannelChatRunRequest{
		ExecID:        exec.ID,
		TaskID:        task.ID,
		ProjectID:     projectID,
		Message:       msg.Text,
		Agent:         *agent,
		ChatHistory:   filterDiscordChatHistory(history, exec.ID),
		SystemContext: systemContext,
		WorkDir:       s.resolveWorkDir(ctx, projectID),
		Surface:       chatcontrol.SurfaceDiscord,
	})
}

func (m discordIncomingMessage) replyChannelID() string {
	if m.ThreadID != "" {
		return m.ThreadID
	}
	return m.ChannelID
}

func (s *DiscordService) queueChatInput(ctx context.Context, projectID, activeExecID, agentID string, msg discordIncomingMessage) bool {
	if s.threadInputRepo == nil {
		return false
	}
	queued := &models.ThreadInput{
		Scope:            models.ThreadInputScopeChat,
		ProjectID:        projectID,
		RunExecutionID:   activeExecID,
		AgentConfigID:    agentID,
		InputMode:        models.ThreadInputModeQueued,
		InputStatus:      models.ThreadInputPending,
		Content:          msg.Text,
		ChatMode:         models.ChatModeOrchestrate,
		Source:           models.TaskOriginDiscord,
		DiscordChannelID: msg.replyChannelID(),
		DiscordThreadID:  msg.ThreadID,
		DiscordMessageID: msg.MessageID,
		DiscordUserID:    msg.UserID,
	}
	if err := s.threadInputRepo.CreateQueued(ctx, queued); err != nil {
		applog.Infof("[discord] queue chat input failed: %v", err)
		_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "Error queueing your message. Please try again.")
		return true
	}
	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, ExecID: queued.ID, Message: msg.Text, Source: msg.Source, Queued: true})
	}
	_ = s.sendDiscordMessage(msg.replyChannelID(), msg.MessageID, "Queued. I'll send this after the current response finishes.")
	return true
}

func (s *DiscordService) SendTaskCompletionToThread(ctx context.Context, channelID, threadID, messageID, taskTitle, output, errMsg, userID string) {
	if !s.IsSendResponsesEnabled(ctx) || channelID == "" {
		return
	}
	message := formatDiscordTaskCompletion(taskTitle, output, errMsg)
	if err := s.sendDiscordMessage(channelID, messageID, message); err != nil {
		applog.Infof("[discord] send completion notification failed for channel=%s thread=%s user=%s: %v", channelID, threadID, userID, err)
	}
}

func (s *DiscordService) SendTaskCompletionNotification(ctx context.Context, task models.Task, output string, errMsg string) {
	if task.CreatedVia != models.TaskOriginDiscord && task.ID != "" && s.taskRepo != nil {
		loaded, err := s.taskRepo.GetByID(ctx, task.ID)
		if err == nil && loaded != nil {
			task = *loaded
		}
	}
	if task.CreatedVia != models.TaskOriginDiscord || task.Category == models.CategoryChat || !s.IsSendResponsesEnabled(ctx) || s.discordTaskContextRepo == nil {
		return
	}
	ctxRecord, err := s.discordTaskContextRepo.GetByTaskID(ctx, task.ID)
	if err != nil || ctxRecord == nil {
		return
	}
	if err := s.sendDiscordMessage(ctxRecord.DiscordChannelID, ctxRecord.DiscordMessageID, formatDiscordTaskCompletion(task.Title, output, errMsg)); err != nil {
		applog.Infof("[discord] send completion notification failed for task=%s: %v", task.ID, err)
	}
}

func (s *DiscordService) SendChatResponse(ctx context.Context, task models.Task, output string, errMsg string) {
	if task.CreatedVia != models.TaskOriginDiscord || task.Category != models.CategoryChat || s.discordTaskContextRepo == nil || !s.IsSendResponsesEnabled(ctx) {
		return
	}
	ctxRecord, err := s.discordTaskContextRepo.GetByTaskID(ctx, task.ID)
	if err != nil || ctxRecord == nil {
		return
	}
	message := ""
	if errMsg != "" {
		message = fmt.Sprintf("Error: %s", util.Truncate(errMsg, 220))
	} else {
		message = llmoutput.CleanChatOutputForDisplay(output)
		if message == "" {
			message = "(No response)"
		}
	}
	if err := s.editOrSendDiscordMessage(ctxRecord.DiscordChannelID, ctxRecord.DiscordMessageID, ctxRecord.DiscordMessageID, message); err != nil {
		applog.Infof("[discord] send chat response failed for task=%s: %v", task.ID, err)
	}
}

func (s *DiscordService) completeExecution(ctx context.Context, execID, taskID, output, errorMessage string, tokensUsed int, durationMs int64) {
	if s.execRepo == nil || s.taskRepo == nil {
		return
	}
	if errorMessage != "" {
		_ = s.execRepo.Complete(ctx, execID, models.ExecFailed, "", errorMessage, 0, durationMs)
		_ = s.taskRepo.UpdateStatus(ctx, taskID, models.StatusFailed)
		s.promoteQueuedChatAfterCompletion(ctx, taskID)
		return
	}
	_ = s.execRepo.Complete(ctx, execID, models.ExecCompleted, output, "", tokensUsed, durationMs)
	_ = s.taskRepo.UpdateStatus(ctx, taskID, models.StatusCompleted)
	s.promoteQueuedChatAfterCompletion(ctx, taskID)
}

func (s *DiscordService) promoteQueuedChatAfterCompletion(ctx context.Context, taskID string) {
	if s.queuedTurnPromoter == nil || s.taskRepo == nil {
		return
	}
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil || task == nil || task.Category != models.CategoryChat {
		return
	}
	s.queuedTurnPromoter(task.ProjectID)
}

func (s *DiscordService) autoSelectAgent(ctx context.Context, message string) (*models.LLMConfig, error) {
	if s.llmConfigRepo == nil {
		return nil, fmt.Errorf("no model repository configured")
	}
	agents, err := s.llmConfigRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents configured")
	}
	complexity := AnalyzeComplexity(message)
	if result := SelectLLMWithVision(complexity, agents, false); result != nil {
		return result.LLMConfig, nil
	}
	for i := range agents {
		if agents[i].IsDefault {
			return &agents[i], nil
		}
	}
	return &agents[0], nil
}

func (s *DiscordService) buildChatContext(ctx context.Context, projectID string) string {
	existingTasks := []models.Task{}
	if s.taskSvc != nil {
		if tasks, err := s.taskSvc.ListByProject(ctx, projectID, ""); err == nil {
			existingTasks = tasks
		}
	}
	availableModels := []models.LLMConfig{}
	if s.llmConfigRepo != nil {
		availableModels, _ = s.llmConfigRepo.List(ctx)
	}
	var schedules []models.Schedule
	if s.scheduleRepo != nil {
		schedules, _ = s.scheduleRepo.ListByProject(ctx, projectID)
	}
	return BuildChatContextWithAgentDefinitions(existingTasks, availableModels, s.listChatAssignableAgentDefinitions(ctx), schedules, time.Now())
}

func (s *DiscordService) listChatAssignableAgentDefinitions(ctx context.Context) []models.Agent {
	if s.agentRepo == nil {
		return nil
	}
	agents, err := s.agentRepo.List(ctx)
	if err != nil {
		applog.Infof("[discord] error listing agent definitions for context: %v", err)
		return nil
	}
	return UniqueChatAssignableAgentDefinitions(agents)
}

func (s *DiscordService) resolveWorkDir(ctx context.Context, projectID string) string {
	if s.projectRepo == nil {
		return ""
	}
	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil || project == nil {
		return ""
	}
	return project.RepoPath
}

func (s *DiscordService) getPersonalityContext(ctx context.Context, projectID string) string {
	if s.settingsRepo == nil {
		return ""
	}
	personality, err := s.settingsRepo.Get(ctx, "personality")
	if err != nil || personality == "" {
		return ""
	}
	prompt := GetPersonalityPromptWithCustom(ctx, personality, s.customPersonalityRepo)
	if prompt == "" {
		return ""
	}
	return "\n# Communication Style\n\n" + prompt
}

func (s *DiscordService) checkAuthorization(ctx context.Context, projectID, discordUserID string) bool {
	if s.discordAuthRepo == nil {
		return true
	}
	if strings.TrimSpace(projectID) == "" {
		authorized, err := s.discordAuthRepo.IsAuthorizedAnywhere(ctx, discordUserID)
		if err != nil {
			applog.Infof("[discord] auth check error for user=%s anywhere: %v", discordUserID, err)
			return false
		}
		return authorized
	}
	authorized, err := s.discordAuthRepo.IsAuthorized(ctx, projectID, discordUserID)
	if err != nil {
		applog.Infof("[discord] auth check error for user=%s project=%s: %v", discordUserID, projectID, err)
		return false
	}
	if authorized {
		return true
	}
	authorizedAnywhere, err := s.discordAuthRepo.IsAuthorizedAnywhere(ctx, discordUserID)
	if err != nil {
		applog.Infof("[discord] auth check error for user=%s fallback-anywhere: %v", discordUserID, err)
		return false
	}
	return authorizedAnywhere
}

func (s *DiscordService) getActiveProject(ctx context.Context, userID string) string {
	key := strings.TrimSpace(userID)
	s.mu.RLock()
	if projectID, ok := s.userProjects[key]; ok {
		s.mu.RUnlock()
		return projectID
	}
	s.mu.RUnlock()
	if s.projectRepo == nil {
		return ""
	}
	projects, err := s.projectRepo.List(ctx)
	if err != nil || len(projects) == 0 {
		return ""
	}
	selected := projects[0].ID
	for _, p := range projects {
		if p.IsDefault {
			selected = p.ID
			break
		}
	}
	s.mu.Lock()
	s.userProjects[key] = selected
	s.mu.Unlock()
	return selected
}

func (s *DiscordService) buildDiscordActionToolRuntime(projectID string, markerCtx discordMarkerContext, collector *channelActionSummaryCollector) *llmcontracts.RuntimeTools {
	handlers := s.discordActionHandlers(projectID, markerCtx, collector)
	return &llmcontracts.RuntimeTools{
		Definitions: actionToolDefinitions(chatcontrol.SurfaceDiscord, true),
		Executor:    chatcontrol.BuildRuntimeToolExecutor(models.ChatModeOrchestrate, chatcontrol.SurfaceDiscord, handlers),
	}
}

type discordMarkerContext struct {
	ChannelID string
	ThreadID  string
	MessageID string
	UserID    string
}

func (s *DiscordService) discordActionHandlers(projectID string, markerCtx discordMarkerContext, collector *channelActionSummaryCollector) map[string]chatcontrol.RuntimeActionHandler {
	return map[string]chatcontrol.RuntimeActionHandler{
		"create_task": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req TaskCreationRequest
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Prompt) == "" {
				return "", fmt.Errorf("create_task requires title and prompt")
			}
			if req.Priority == 0 {
				req.Priority = 2
			}
			agents := []models.LLMConfig{}
			if s.llmConfigRepo != nil {
				agents, _ = s.llmConfigRepo.List(ctx)
			}
			createdTasks, summary := ExecuteTaskCreationsWithReturn(ctx, []TaskCreationRequest{req}, projectID, s.taskSvc, agents)
			for _, t := range createdTasks {
				if s.taskRepo != nil {
					if err := s.taskRepo.UpdateDiscordOrigin(ctx, t.ID); err != nil {
						applog.Infof("[discord] runtime create_task update discord origin failed for task=%s: %v", t.ID, err)
					}
				}
				if s.discordTaskContextRepo != nil {
					_ = s.discordTaskContextRepo.Upsert(ctx, &models.DiscordTaskContext{TaskID: t.ID, DiscordChannelID: markerCtx.ChannelID, DiscordThreadID: markerCtx.ThreadID, DiscordMessageID: markerCtx.MessageID, DiscordUserID: markerCtx.UserID})
				}
			}
			if collector != nil {
				collector.addCreated(summary)
			}
			return strings.TrimSpace(summary), nil
		},
		"edit_task": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req TaskEditRequest
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			if strings.TrimSpace(req.ID) == "" {
				return "", fmt.Errorf("edit_task requires id")
			}
			summary := ExecuteTaskEdits(ctx, []TaskEditRequest{req}, projectID, s.taskSvc, nil, "")
			if collector != nil {
				collector.addEdited(summary)
			}
			return strings.TrimSpace(summary), nil
		},
		"execute_tasks": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req TaskExecutionRequest
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			if strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Title) == "" && len(req.Tags) == 0 && req.MinPriority == 0 {
				return "", fmt.Errorf("execute_tasks requires task_id/title or tags/min_priority")
			}
			return strings.TrimSpace(ExecuteTaskExecutions(ctx, []TaskExecutionRequest{req}, projectID, s.taskSvc)), nil
		},
		"view_task_thread": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.discordViewTaskThread(ctx, projectID, input), nil
		},
		"send_to_task": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.discordSendToTask(ctx, projectID, input, markerCtx), nil
		},
		"set_task_goal": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelSetTaskGoal(ctx, projectID, input)
		},
		"clear_task_goal": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelClearTaskGoal(ctx, projectID, input)
		},
		"get_task_goal": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelGetTaskGoal(ctx, projectID, input)
		},
		"pause_task_goal": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelPauseTaskGoal(ctx, projectID, input)
		},
		"resume_task_goal": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelResumeTaskGoal(ctx, projectID, input)
		},
		"mark_task_goal_achieved": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelMarkTaskGoalAchieved(ctx, projectID, input)
		},
		"report_task_goal_blocked": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.executeChannelReportTaskGoalBlocked(ctx, projectID, input)
		},
		"list_projects": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.buildProjectListResult(ctx, projectID), nil
		},
		"switch_project": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req SwitchProjectRequest
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			return s.switchProjectResult(ctx, markerCtx.UserID, req.Project), nil
		},
		"list_capabilities": func(_ context.Context, _ json.RawMessage) (string, error) {
			return formatChannelCapabilities(chatcontrol.ListForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceDiscord)), nil
		},
	}
}

func (s *DiscordService) discordViewTaskThread(ctx context.Context, projectID string, input json.RawMessage) string {
	var req ViewThreadRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for view_task_thread."
	}
	if strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Title) == "" {
		return "view_task_thread requires task_id or title."
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return fmt.Sprintf("Error resolving task: %v", err)
	}
	executions, err := s.execRepo.ListByTaskChronological(ctx, task.ID)
	if err != nil {
		return fmt.Sprintf("Error retrieving thread for %q: %v", task.Title, err)
	}
	return strings.TrimSpace(formatThreadTranscript(task, executions, req.Offset, req.Limit))
}

func (s *DiscordService) discordSendToTask(ctx context.Context, projectID string, input json.RawMessage, markerCtx discordMarkerContext) string {
	var req SendToTaskRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for send_to_task."
	}
	if strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Title) == "" {
		return "send_to_task requires task_id or title."
	}
	if strings.TrimSpace(req.Message) == "" {
		return "send_to_task requires message."
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return fmt.Sprintf("Error resolving task: %v", err)
	}
	activeExec, queueBehindFirstTurn, activeErr := s.activeOrStartingTaskTurn(ctx, task)
	if activeErr != nil {
		return fmt.Sprintf("Error checking active turn for task %q: %v", task.Title, activeErr)
	}
	if activeExec != nil || queueBehindFirstTurn {
		if s.threadInputRepo == nil {
			return fmt.Sprintf("Task %q is currently running. Queue is unavailable, so wait for it to finish before sending a message.", task.Title)
		}
		agentID := ""
		if task.AgentID != nil {
			agentID = *task.AgentID
		}
		runExecutionID := ""
		if activeExec != nil {
			runExecutionID = activeExec.ID
		}
		queued := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: task.ProjectID, TaskID: task.ID, RunExecutionID: runExecutionID, AgentConfigID: agentID, InputMode: models.ThreadInputModeQueued, InputStatus: models.ThreadInputPending, Content: req.Message, Source: models.TaskOriginDiscord, DiscordChannelID: markerCtx.ChannelID, DiscordThreadID: markerCtx.ThreadID, DiscordMessageID: markerCtx.MessageID, DiscordUserID: markerCtx.UserID}
		if err := s.threadInputRepo.CreateQueued(ctx, queued); err != nil {
			return fmt.Sprintf("Error queueing message for task %q: %v", task.Title, err)
		}
		if queued.RunExecutionID == "" {
			_ = s.bindQueuedTaskInputToActiveExecutionIfAvailable(ctx, queued)
		}
		if shouldPromote, promoteErr := s.shouldPromotePreExecutionQueuedInput(ctx, task, queued); promoteErr == nil && shouldPromote && s.queuedTaskThreadPromoter != nil {
			go s.queuedTaskThreadPromoter(task.ID)
		}
		return fmt.Sprintf("Queued message to task %q [TASK_ID:%s]. It will run after the active thread turn finishes.", task.Title, task.ID)
	}
	agent, err := s.agentForTaskMessage(ctx, task, req.Message)
	if err != nil {
		return fmt.Sprintf("Error selecting agent for task %q: %v", task.Title, err)
	}
	exec := &models.Execution{TaskID: task.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: req.Message, IsFollowup: true}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		return fmt.Sprintf("Error creating follow-up execution for %q: %v", task.Title, err)
	}
	priorExecs, _ := s.execRepo.ListByTaskChronological(ctx, task.ID)
	priorHistory := filterDiscordChatHistory(priorExecs, exec.ID)
	systemContext := buildTelegramTaskChatContext(task.Title, len(priorHistory) > 0)
	if pCtx := s.getPersonalityContext(ctx, task.ProjectID); pCtx != "" {
		systemContext += pCtx
	}
	if s.channelTaskRunner == nil {
		msgText := "shared task runner is unavailable"
		s.completeExecution(ctx, exec.ID, task.ID, "", msgText, 0, 0)
		return fmt.Sprintf("Error sending message to task %q: %s", task.Title, msgText)
	}
	if task.Status != models.StatusRunning && task.Status != models.StatusQueued {
		if err := s.taskRepo.UpdateStatus(ctx, task.ID, models.StatusQueued); err != nil {
			s.completeExecution(ctx, exec.ID, task.ID, "", err.Error(), 0, 0)
			return fmt.Sprintf("Error updating task %q: %v", task.Title, err)
		}
	}
	if task.Category != models.CategoryActive {
		_ = s.taskRepo.UpdateCategory(ctx, task.ID, models.CategoryActive)
	}
	s.channelTaskRunner(context.Background(), ChannelTaskRunRequest{ExecID: exec.ID, TaskID: task.ID, ProjectID: task.ProjectID, Message: req.Message, Agent: *agent, ChatHistory: priorHistory, SystemContext: systemContext, Surface: chatcontrol.SurfaceDiscord, ReplyContext: ChannelReplyContext{Source: models.TaskOriginDiscord, DiscordChannelID: markerCtx.ChannelID, DiscordThreadID: markerCtx.ThreadID, DiscordMessageID: markerCtx.MessageID, DiscordUserID: markerCtx.UserID}})
	return fmt.Sprintf("Sent message to task %q [TASK_ID:%s] and started processing.", task.Title, task.ID)
}

func (s *DiscordService) taskHasStartingFirstTurn(ctx context.Context, task *models.Task) (bool, error) {
	if task == nil || task.ID == "" || s.execRepo == nil {
		return false, nil
	}
	if task.Category != models.CategoryActive || (task.Status != models.StatusPending && task.Status != models.StatusQueued && task.Status != models.StatusRunning) {
		return false, nil
	}
	execs, err := s.execRepo.ListByTaskChronological(ctx, task.ID)
	if err != nil {
		return false, err
	}
	return len(execs) == 0, nil
}
func (s *DiscordService) activeOrStartingTaskTurn(ctx context.Context, task *models.Task) (*models.Execution, bool, error) {
	if s.execRepo == nil {
		return nil, false, nil
	}
	activeExec, err := s.execRepo.FindActiveTaskExecution(ctx, task.ID, "")
	if err != nil || activeExec != nil {
		return activeExec, false, err
	}
	starting, err := s.taskHasStartingFirstTurn(ctx, task)
	return nil, starting, err
}
func (s *DiscordService) bindQueuedTaskInputToActiveExecutionIfAvailable(ctx context.Context, input *models.ThreadInput) error {
	if input == nil || input.RunExecutionID != "" || s.execRepo == nil || s.threadInputRepo == nil {
		return nil
	}
	active, err := s.execRepo.FindActiveTaskExecution(ctx, input.TaskID, "")
	if err != nil || active == nil {
		return err
	}
	if err := s.threadInputRepo.BindPreExecutionQueuedTaskInputs(ctx, input.TaskID, active.ID); err != nil {
		return err
	}
	input.RunExecutionID = active.ID
	return nil
}
func (s *DiscordService) shouldPromotePreExecutionQueuedInput(ctx context.Context, task *models.Task, input *models.ThreadInput) (bool, error) {
	if task == nil || input == nil || input.RunExecutionID != "" {
		return false, nil
	}
	starting, err := s.taskHasStartingFirstTurn(ctx, task)
	if err != nil || starting {
		return false, err
	}
	return true, nil
}
func (s *DiscordService) resolveTaskReference(ctx context.Context, projectID, taskID, title string) (*models.Task, error) {
	if s.taskRepo == nil {
		return nil, fmt.Errorf("task repository not configured")
	}
	if strings.TrimSpace(taskID) != "" {
		task, err := s.taskRepo.GetByID(ctx, strings.TrimSpace(taskID))
		if err != nil {
			return nil, err
		}
		if task == nil || task.ProjectID != projectID {
			return nil, fmt.Errorf("task %s not found", taskID)
		}
		return task, nil
	}
	tasks, err := s.taskSvc.ListByProject(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if strings.EqualFold(tasks[i].Title, strings.TrimSpace(title)) {
			return &tasks[i], nil
		}
	}
	return nil, fmt.Errorf("task %q not found", title)
}

func (s *DiscordService) agentForTaskMessage(ctx context.Context, task *models.Task, message string) (*models.LLMConfig, error) {
	if task.AgentID != nil {
		if agent, _ := s.llmConfigRepo.GetByID(ctx, *task.AgentID); agent != nil {
			return agent, nil
		}
	}
	return s.autoSelectAgent(ctx, message)
}

func (s *DiscordService) buildProjectListResult(ctx context.Context, projectID string) string {
	if s.projectRepo == nil {
		return "Error retrieving projects: project repository not configured"
	}
	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return "Error retrieving projects: " + err.Error()
	}
	var sb strings.Builder
	sb.WriteString("Available Projects:\n")
	if len(projects) == 0 {
		sb.WriteString("No projects found.")
		return sb.String()
	}
	for _, p := range projects {
		marker := ""
		if p.ID == projectID {
			marker = " <- current"
		}
		desc := ""
		if p.Description != "" {
			desc = " - " + p.Description
		}
		sb.WriteString(fmt.Sprintf("- %s%s%s\n", p.Name, desc, marker))
	}
	sb.WriteString("Ask me to switch projects by name when needed.")
	return strings.TrimSpace(sb.String())
}
func (s *DiscordService) switchProjectResult(ctx context.Context, userID, targetProject string) string {
	targetProject = strings.TrimSpace(targetProject)
	if targetProject == "" {
		return "Project switch requires a project name or ID."
	}
	if s.projectRepo == nil {
		return "Error loading projects: project repository not configured"
	}
	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return "Error loading projects: " + err.Error()
	}
	var target *models.Project
	for i := range projects {
		if strings.EqualFold(projects[i].Name, targetProject) || projects[i].ID == targetProject {
			target = &projects[i]
			break
		}
	}
	if target == nil {
		names := make([]string, 0, len(projects))
		for _, p := range projects {
			names = append(names, p.Name)
		}
		return fmt.Sprintf("Project not found: %q. Available projects: %s", targetProject, strings.Join(names, ", "))
	}
	s.mu.Lock()
	s.userProjects[strings.TrimSpace(userID)] = target.ID
	s.mu.Unlock()
	return fmt.Sprintf("Switched to project: %s", target.Name)
}

func (s *DiscordService) executeChannelSetTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	goal, err := s.taskGoalSvc.SetGoal(ctx, task.ID, req.Goal, GoalOptions{Actor: "assistant"})
	if err != nil {
		return "", err
	}
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) executeChannelClearTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	if err := s.taskGoalSvc.ClearGoal(ctx, task.ID, "assistant"); err != nil {
		return "", err
	}
	goal, _ := s.taskGoalSvc.GetGoal(ctx, task.ID)
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) executeChannelGetTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	goal, err := s.taskGoalSvc.GetGoal(ctx, task.ID)
	if err != nil {
		return "", err
	}
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) executeChannelPauseTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	if err := s.taskGoalSvc.PauseGoal(ctx, task.ID, "assistant"); err != nil {
		return "", err
	}
	goal, _ := s.taskGoalSvc.GetGoal(ctx, task.ID)
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) executeChannelResumeTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	if err := s.taskGoalSvc.ResumeGoal(ctx, task.ID, "assistant"); err != nil {
		return "", err
	}
	goal, _ := s.taskGoalSvc.GetGoal(ctx, task.ID)
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) executeChannelMarkTaskGoalAchieved(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	goal, err := s.taskGoalSvc.MarkAchieved(ctx, task.ID, req.GoalID, req.Reason)
	if err != nil {
		return "", err
	}
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) executeChannelReportTaskGoalBlocked(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
	if s.taskGoalSvc == nil {
		return "", fmt.Errorf("task goal service unavailable")
	}
	var req channelGoalToolInput
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "", err
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return "", err
	}
	goal, err := s.taskGoalSvc.RecordBlockedReport(ctx, task.ID, req.GoalID, req.BlockerKey, req.Reason)
	if err != nil {
		return "", err
	}
	return channelGoalToolJSON(goal)
}

func (s *DiscordService) sendDiscordMessage(channelID, messageID, text string) error {
	_, err := s.sendDiscordMessageWithID(channelID, messageID, text)
	return err
}
func (s *DiscordService) sendDiscordMessageWithID(channelID, messageID, text string) (string, error) {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(text) == "" {
		return "", nil
	}
	if s.sendMessageFunc != nil {
		return s.sendMessageFunc(channelID, messageID, text)
	}
	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()
	if session == nil {
		botToken := strings.TrimSpace(s.getSetting(context.Background(), DiscordSettingBotToken))
		if botToken == "" {
			return "", fmt.Errorf("discord bot token is not configured")
		}
		var err error
		session, err = discordgo.New("Bot " + botToken)
		if err != nil {
			return "", err
		}
	}
	chunks := splitDiscordMessage(text)
	var firstID string
	for _, chunk := range chunks {
		msg, err := session.ChannelMessageSendReply(channelID, chunk, &discordgo.MessageReference{MessageID: messageID, ChannelID: channelID})
		if err != nil {
			msg, err = session.ChannelMessageSend(channelID, chunk)
		}
		if err != nil {
			return firstID, fmt.Errorf("send discord message: %w", err)
		}
		if firstID == "" && msg != nil {
			firstID = msg.ID
		}
	}
	return firstID, nil
}

func (s *DiscordService) editOrSendDiscordMessage(channelID, editMessageID, replyMessageID, text string) error {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(text) == "" {
		return nil
	}
	if editMessageID != "" && s.sendMessageFunc == nil {
		s.mu.RLock()
		session := s.session
		s.mu.RUnlock()
		if session != nil {
			chunk := text
			if len(chunk) > discordMessageLimit {
				chunk = chunk[:discordMessageLimit-3] + "..."
			}
			if _, err := session.ChannelMessageEdit(channelID, editMessageID, chunk); err == nil {
				return nil
			}
		}
	}
	return s.sendDiscordMessage(channelID, replyMessageID, text)
}

func splitDiscordMessage(text string) []string { return splitMessage(text, discordMessageLimit) }
func formatDiscordTaskCompletion(taskTitle, output, errMsg string) string {
	if errMsg != "" {
		return fmt.Sprintf("Task failed: %s\n\n%s", taskTitle, util.Truncate(errMsg, 500))
	}
	cleaned := llmoutput.CleanChatOutputForDisplay(output)
	if cleaned == "" {
		cleaned = "(No output)"
	}
	return fmt.Sprintf("Task completed: %s\n\n%s", taskTitle, util.Truncate(cleaned, 3500))
}
func filterDiscordChatHistory(executions []models.Execution, currentExecID string) []models.Execution {
	return filterSlackChatHistory(executions, currentExecID)
}
func sanitizeDiscordText(text, botID string) string {
	cleaned := discordMentionRegex.ReplaceAllString(text, "")
	if botID != "" {
		cleaned = strings.ReplaceAll(cleaned, "<@"+botID+">", "")
		cleaned = strings.ReplaceAll(cleaned, "<@!"+botID+">", "")
	}
	return strings.TrimSpace(cleaned)
}
func discordMentionsBot(mentions []*discordgo.User, botID string) bool {
	if botID == "" {
		return false
	}
	for _, user := range mentions {
		if user != nil && user.ID == botID {
			return true
		}
	}
	return false
}
func discordThreadID(msg *discordgo.Message) string {
	if msg == nil || msg.Thread == nil {
		return ""
	}
	return msg.Thread.ID
}
func discordDisplayName(user *discordgo.User) string {
	if user == nil {
		return ""
	}
	if user.GlobalName != "" {
		return user.GlobalName
	}
	return user.Username
}
func discordAttachmentPrompt(attachments []*discordgo.MessageAttachment) string {
	names := make([]string, 0, len(attachments))
	for _, att := range attachments {
		if att != nil && att.Filename != "" {
			names = append(names, att.Filename)
		}
	}
	if len(names) == 0 {
		return "User sent an attachment."
	}
	return "User sent attachment(s): " + strings.Join(names, ", ")
}
func discordIsDM(sess *discordgo.Session, channelID, guildID string) bool {
	if guildID == "" {
		return true
	}
	if sess == nil {
		return false
	}
	ch, err := sess.State.Channel(channelID)
	if err != nil || ch == nil {
		ch, _ = sess.Channel(channelID)
	}
	return ch != nil && ch.Type == discordgo.ChannelTypeDM
}
func (s *DiscordService) requiresMentionForMessage(ctx context.Context, msg discordIncomingMessage) bool {
	if msg.IsDM {
		return false
	}
	free := parseDiscordChannelList(s.getSetting(ctx, DiscordSettingFreeResponseChannels))
	if free[msg.ChannelID] || (msg.ThreadID != "" && free[msg.ThreadID]) {
		return false
	}
	return s.IsRequireMentionEnabled(ctx)
}
func parseDiscordChannelList(value string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}
func (s *DiscordService) botUserID(ctx context.Context, sess *discordgo.Session) string {
	if sess != nil && sess.State != nil && sess.State.User != nil && sess.State.User.ID != "" {
		return sess.State.User.ID
	}
	return strings.TrimSpace(s.getSetting(ctx, DiscordSettingBotUserID))
}
func (s *DiscordService) getSetting(ctx context.Context, key string) string {
	if s.settingsRepo == nil {
		return ""
	}
	val, _ := s.settingsRepo.Get(ctx, key)
	return val
}
func (s *DiscordService) setSetting(ctx context.Context, key, value string) error {
	if s.settingsRepo == nil {
		return nil
	}
	return s.settingsRepo.Set(ctx, key, value)
}
