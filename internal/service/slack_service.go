package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/events"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	llmoutput "github.com/openvibely/openvibely/internal/llm/output"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/util"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	SlackSettingClientID         = "slack_client_id"
	SlackSettingClientSecret     = "slack_client_secret"
	SlackSettingAppToken         = "slack_app_token"
	SlackSettingBotToken         = "slack_bot_token"
	SlackSettingBotTokenOverride = "slack_bot_token_override"
	SlackSettingBotTokenSource   = "slack_bot_token_source"
	SlackSettingBotUserID        = "slack_bot_user_id"
	SlackSettingTeamID           = "slack_team_id"
	SlackSettingTeamName         = "slack_team_name"
	SlackSettingConnectedAt      = "slack_connected_at"
	SlackSettingOAuthState       = "slack_oauth_state"
	SlackSettingSendResponses    = "slack_send_responses"

	SlackBotTokenSourceOAuth  = "oauth"
	SlackBotTokenSourceManual = "manual"

	defaultSlackAPIBaseURL  = "https://slack.com/api"
	slackProcessTimeout     = 5 * time.Minute
	slackChatHistoryLimit   = 50
	slackMaxFileSize        = 10 << 20
	slackMaxTextFileSize    = 100 * 1024
	slackMaxFilesPerMessage = 3
)

var slackMentionRegex = regexp.MustCompile(`<@[^>]+>`)

type SlackConnectionStatus struct {
	Configured bool
	Connected  bool
	Running    bool

	TeamID    string
	TeamName  string
	BotUserID string

	HasClientID         bool
	HasClientSecret     bool
	HasAppToken         bool
	HasBotToken         bool
	HasBotTokenOverride bool
	BotTokenSource      string
}

// SlackService manages Slack OAuth, Socket Mode event processing, and
// Slack-origin task completion notifications.
type SlackService struct {
	settingsRepo             *repository.SettingsRepo
	projectRepo              *repository.ProjectRepo
	llmConfigRepo            *repository.LLMConfigRepo
	taskRepo                 *repository.TaskRepo
	execRepo                 *repository.ExecutionRepo
	scheduleRepo             *repository.ScheduleRepo
	chatAttachmentRepo       *repository.ChatAttachmentRepo
	taskSvc                  *TaskService
	taskGoalSvc              *TaskGoalService
	llmSvc                   *LLMService
	workerSvc                *WorkerService
	slackUserProjectRepo     *repository.SlackUserProjectRepo
	slackTaskContextRepo     *repository.SlackTaskContextRepo
	threadInputRepo          *repository.ThreadInputRepo
	customPersonalityRepo    *repository.CustomPersonalityRepo
	slackAuthRepo            *repository.SlackAuthRepo
	agentRepo                *repository.AgentRepo
	chatBroadcaster          *events.ChatBroadcaster
	queuedTurnPromoter       func(projectID string)
	queuedTaskThreadPromoter func(taskID string)
	channelChatRunner        ChannelChatRunner
	channelTaskRunner        ChannelTaskRunner
	alertSvc                 *AlertService
	channelMessageRouter     *ChannelMessageRouter
	uploadsDir               string

	httpClient   *http.Client
	oauthBaseURL string

	mu                       sync.RWMutex
	botClient                *slack.Client
	socketClient             *socketmode.Client
	running                  bool
	ctx                      context.Context
	cancel                   context.CancelFunc
	userProjects             map[string]string
	processedMessageEvents   map[string]time.Time
	postMessageFn            func(channelID, threadTS, text string) (string, error)
	openConversationFn       func(userID string) (string, error)
	processIncomingMessageFn func(msg slackIncomingMessage)
}

func NewSlackService(
	settingsRepo *repository.SettingsRepo,
	projectRepo *repository.ProjectRepo,
	llmConfigRepo *repository.LLMConfigRepo,
	taskRepo *repository.TaskRepo,
	execRepo *repository.ExecutionRepo,
	scheduleRepo *repository.ScheduleRepo,
	taskSvc *TaskService,
	llmSvc *LLMService,
	workerSvc *WorkerService,
	slackUserProjectRepo *repository.SlackUserProjectRepo,
	slackTaskContextRepo *repository.SlackTaskContextRepo,
	slackAuthRepo *repository.SlackAuthRepo,
) *SlackService {
	return &SlackService{
		settingsRepo:         settingsRepo,
		projectRepo:          projectRepo,
		llmConfigRepo:        llmConfigRepo,
		taskRepo:             taskRepo,
		execRepo:             execRepo,
		scheduleRepo:         scheduleRepo,
		taskSvc:              taskSvc,
		llmSvc:               llmSvc,
		workerSvc:            workerSvc,
		slackUserProjectRepo: slackUserProjectRepo,
		slackTaskContextRepo: slackTaskContextRepo,
		slackAuthRepo:        slackAuthRepo,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		oauthBaseURL:           defaultSlackAPIBaseURL,
		uploadsDir:             "uploads",
		userProjects:           make(map[string]string),
		processedMessageEvents: make(map[string]time.Time),
	}
}

func (s *SlackService) SetCustomPersonalityRepo(repo *repository.CustomPersonalityRepo) {
	s.customPersonalityRepo = repo
}

func (s *SlackService) SetChatBroadcaster(cb *events.ChatBroadcaster) {
	s.chatBroadcaster = cb
}

func (s *SlackService) SetThreadInputRepo(repo *repository.ThreadInputRepo) {
	s.threadInputRepo = repo
}

func (s *SlackService) SetChatAttachmentRepo(repo *repository.ChatAttachmentRepo) {
	s.chatAttachmentRepo = repo
}

func (s *SlackService) SetUploadsDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	s.uploadsDir = dir
}

func (s *SlackService) SetAgentRepo(repo *repository.AgentRepo) {
	s.agentRepo = repo
}

func (s *SlackService) SetQueuedTurnPromoter(promoter func(projectID string)) {
	s.queuedTurnPromoter = promoter
}

func (s *SlackService) SetQueuedTaskThreadPromoter(promoter func(taskID string)) {
	s.queuedTaskThreadPromoter = promoter
}

func (s *SlackService) SetChannelChatRunner(runner ChannelChatRunner) {
	s.channelChatRunner = runner
}

func (s *SlackService) SetChannelTaskRunner(runner ChannelTaskRunner) {
	s.channelTaskRunner = runner
}

func (s *SlackService) SetAlertService(svc *AlertService) {
	s.alertSvc = svc
}

func (s *SlackService) SetChannelMessageRouter(router *ChannelMessageRouter) {
	s.channelMessageRouter = router
}

// SetTaskGoalService injects the task goal service so Slack can execute
// goal-related chat-control tools with the same durable TaskGoalService
// behavior as web/API chat.
func (s *SlackService) SetTaskGoalService(svc *TaskGoalService) {
	s.taskGoalSvc = svc
}

func (s *SlackService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *SlackService) Start() error {
	appToken := s.getSetting(context.Background(), SlackSettingAppToken)
	botToken := s.resolveBotToken(context.Background())
	if strings.TrimSpace(appToken) == "" || strings.TrimSpace(botToken) == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	botClient := slack.New(botToken, slack.OptionAppLevelToken(appToken), slack.OptionHTTPClient(s.httpClient))
	socketClient := socketmode.New(botClient)
	ctx, cancel := context.WithCancel(context.Background())

	s.botClient = botClient
	s.socketClient = socketClient
	s.ctx = ctx
	s.cancel = cancel
	s.running = true

	go s.runSocketLoop(ctx, socketClient)
	go socketClient.RunContext(ctx)

	applog.Infof("[slack] socket mode started")
	return nil
}

func (s *SlackService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
	s.socketClient = nil
	s.botClient = nil
	applog.Infof("[slack] socket mode stopped")
}

func (s *SlackService) ReloadFromSettings(ctx context.Context) error {
	s.Stop()
	return s.Start()
}

func (s *SlackService) GetConnectionStatus(ctx context.Context) (SlackConnectionStatus, error) {
	clientID := strings.TrimSpace(s.getSetting(ctx, SlackSettingClientID))
	clientSecret := strings.TrimSpace(s.getSetting(ctx, SlackSettingClientSecret))
	appToken := strings.TrimSpace(s.getSetting(ctx, SlackSettingAppToken))
	oauthBotToken := strings.TrimSpace(s.getSetting(ctx, SlackSettingBotToken))
	overrideBotToken := strings.TrimSpace(s.getSetting(ctx, SlackSettingBotTokenOverride))
	botToken := strings.TrimSpace(s.resolveBotToken(ctx))
	teamID := strings.TrimSpace(s.getSetting(ctx, SlackSettingTeamID))
	teamName := strings.TrimSpace(s.getSetting(ctx, SlackSettingTeamName))
	botUserID := strings.TrimSpace(s.getSetting(ctx, SlackSettingBotUserID))
	botTokenSource := s.getBotTokenSource(ctx)

	status := SlackConnectionStatus{
		HasClientID:         clientID != "",
		HasClientSecret:     clientSecret != "",
		HasAppToken:         appToken != "",
		HasBotToken:         botToken != "",
		HasBotTokenOverride: overrideBotToken != "",
		BotTokenSource:      botTokenSource,
		TeamID:              teamID,
		TeamName:            teamName,
		BotUserID:           botUserID,
		Running:             s.IsRunning(),
	}
	status.Configured = status.HasClientID || status.HasClientSecret || status.HasAppToken || status.HasBotToken
	status.Connected = oauthBotToken != "" || (botTokenSource == SlackBotTokenSourceManual && overrideBotToken != "")
	return status, nil
}

func (s *SlackService) ConnectURL(ctx context.Context, redirectURI string) (string, error) {
	clientID := strings.TrimSpace(s.getSetting(ctx, SlackSettingClientID))
	clientSecret := strings.TrimSpace(s.getSetting(ctx, SlackSettingClientSecret))
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("slack client id and client secret are required")
	}

	state, err := generateOAuthState()
	if err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	if err := s.setSetting(ctx, SlackSettingOAuthState, state); err != nil {
		return "", fmt.Errorf("save oauth state: %w", err)
	}

	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("scope", "app_mentions:read,channels:history,groups:history,im:history,mpim:history,chat:write,im:write,files:read")
	v.Set("redirect_uri", redirectURI)
	v.Set("state", state)
	return "https://slack.com/oauth/v2/authorize?" + v.Encode(), nil
}

func (s *SlackService) HandleOAuthCallback(ctx context.Context, code, state, redirectURI string) error {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || state == "" {
		return fmt.Errorf("missing oauth code or state")
	}

	expectedState := strings.TrimSpace(s.getSetting(ctx, SlackSettingOAuthState))
	if expectedState == "" || state != expectedState {
		return fmt.Errorf("invalid oauth state")
	}

	clientID := strings.TrimSpace(s.getSetting(ctx, SlackSettingClientID))
	clientSecret := strings.TrimSpace(s.getSetting(ctx, SlackSettingClientSecret))
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("slack client id and client secret are required")
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", redirectURI)

	resp, err := s.httpClient.PostForm(strings.TrimRight(s.oauthBaseURL, "/")+"/oauth.v2.access", form)
	if err != nil {
		return fmt.Errorf("exchange oauth code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read oauth response: %w", err)
	}

	var payload struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		BotUserID   string `json:"bot_user_id"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode oauth response: %w", err)
	}
	if !payload.OK {
		if payload.Error == "" {
			payload.Error = "oauth exchange failed"
		}
		return fmt.Errorf("slack oauth error: %s", payload.Error)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return fmt.Errorf("oauth response missing bot access token")
	}

	if err := s.setSetting(ctx, SlackSettingBotToken, strings.TrimSpace(payload.AccessToken)); err != nil {
		return err
	}
	_ = s.setSetting(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceOAuth)
	_ = s.setSetting(ctx, SlackSettingBotUserID, strings.TrimSpace(payload.BotUserID))
	_ = s.setSetting(ctx, SlackSettingTeamID, strings.TrimSpace(payload.Team.ID))
	_ = s.setSetting(ctx, SlackSettingTeamName, strings.TrimSpace(payload.Team.Name))
	_ = s.setSetting(ctx, SlackSettingConnectedAt, time.Now().UTC().Format(time.RFC3339))
	_ = s.setSetting(ctx, SlackSettingOAuthState, "")

	if strings.TrimSpace(s.getSetting(ctx, SlackSettingSendResponses)) == "" {
		_ = s.setSetting(ctx, SlackSettingSendResponses, "true")
	}

	return s.ReloadFromSettings(ctx)
}

func (s *SlackService) Disconnect(ctx context.Context) error {
	s.Stop()
	_ = s.setSetting(ctx, SlackSettingBotToken, "")
	_ = s.setSetting(ctx, SlackSettingBotTokenOverride, "")
	_ = s.setSetting(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceOAuth)
	_ = s.setSetting(ctx, SlackSettingBotUserID, "")
	_ = s.setSetting(ctx, SlackSettingTeamID, "")
	_ = s.setSetting(ctx, SlackSettingTeamName, "")
	_ = s.setSetting(ctx, SlackSettingConnectedAt, "")
	_ = s.setSetting(ctx, SlackSettingOAuthState, "")
	return nil
}

func (s *SlackService) TestConnection(ctx context.Context) error {
	botToken := strings.TrimSpace(s.resolveBotToken(ctx))
	if botToken == "" {
		return fmt.Errorf("slack bot token is not configured")
	}
	client := slack.New(botToken)
	if _, err := client.AuthTestContext(ctx); err != nil {
		return fmt.Errorf("auth test failed: %w", err)
	}
	return nil
}

func (s *SlackService) IsSendResponsesEnabled(ctx context.Context) bool {
	val := s.getSetting(ctx, SlackSettingSendResponses)
	if strings.TrimSpace(val) == "" {
		return true
	}
	return strings.TrimSpace(strings.ToLower(val)) != "false"
}

func (s *SlackService) SendTaskCompletionToThread(ctx context.Context, channelID, threadTS, taskTitle, output, errMsg, userID string) {
	if !s.IsSendResponsesEnabled(ctx) || channelID == "" {
		return
	}
	var message string
	if errMsg != "" {
		message = fmt.Sprintf("❌ *Task failed:* %s\n\n%s", taskTitle, util.Truncate(errMsg, 500))
	} else {
		cleaned := llmoutput.CleanChatOutputForDisplay(output)
		if cleaned == "" {
			cleaned = "(No output)"
		}
		message = fmt.Sprintf("✅ *Task completed:* %s\n\n%s", taskTitle, util.Truncate(cleaned, 3500))
	}
	if err := s.sendSlackMessage(channelID, threadTS, message); err != nil {
		applog.Infof("[slack] send completion notification failed for channel=%s thread=%s user=%s: %v", channelID, threadTS, userID, err)
	}
}

func (s *SlackService) SendTaskCompletionNotification(ctx context.Context, task models.Task, output string, errMsg string) {
	if task.CreatedVia != models.TaskOriginSlack && task.ID != "" && s.taskRepo != nil {
		loaded, err := s.taskRepo.GetByID(ctx, task.ID)
		if err == nil && loaded != nil {
			task = *loaded
		}
	}
	if task.CreatedVia != models.TaskOriginSlack {
		return
	}
	if task.Category == models.CategoryChat {
		return
	}
	if !s.IsSendResponsesEnabled(ctx) {
		return
	}
	if s.slackTaskContextRepo == nil {
		return
	}
	ctxRecord, err := s.slackTaskContextRepo.GetByTaskID(ctx, task.ID)
	if err != nil || ctxRecord == nil {
		return
	}

	var message string
	if errMsg != "" {
		message = fmt.Sprintf("❌ *Task failed:* %s\n\n%s", task.Title, util.Truncate(errMsg, 500))
	} else {
		cleaned := llmoutput.CleanChatOutputForDisplay(output)
		if cleaned == "" {
			cleaned = "(No output)"
		}
		message = fmt.Sprintf("✅ *Task completed:* %s\n\n%s", task.Title, util.Truncate(cleaned, 3500))
	}

	if err := s.sendSlackMessage(ctxRecord.SlackChannelID, ctxRecord.SlackThreadTS, message); err != nil {
		applog.Infof("[slack] send completion notification failed for task=%s: %v", task.ID, err)
	}
}

func (s *SlackService) runSocketLoop(ctx context.Context, client *socketmode.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-client.Events:
			if !ok {
				return
			}
			s.handleSocketEvent(ctx, client, evt)
		}
	}
}

func (s *SlackService) handleSocketEvent(ctx context.Context, client *socketmode.Client, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		if evt.Request != nil {
			client.Ack(*evt.Request)
		}

		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if eventsAPIEvent.Type != slackevents.CallbackEvent {
			return
		}

		teamID := strings.TrimSpace(eventsAPIEvent.TeamID)
		switch e := eventsAPIEvent.InnerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			s.handleAppMention(ctx, teamID, *e)
		case slackevents.AppMentionEvent:
			s.handleAppMention(ctx, teamID, e)
		case *slackevents.MessageEvent:
			s.handleMessageEvent(ctx, teamID, *e)
		case slackevents.MessageEvent:
			s.handleMessageEvent(ctx, teamID, e)
		}
	}
}

func (s *SlackService) handleAppMention(ctx context.Context, teamID string, event slackevents.AppMentionEvent) {
	if strings.TrimSpace(event.User) == "" {
		return
	}
	if strings.TrimSpace(event.BotID) != "" {
		return
	}
	botUserID := strings.TrimSpace(s.getSetting(ctx, SlackSettingBotUserID))
	if botUserID != "" && strings.TrimSpace(event.User) == botUserID {
		return
	}

	files := slackIncomingFilesFromAppMention(event)
	text := slackMessageTextOrAttachmentPrompt(sanitizeSlackText(event.Text), len(files) > 0)
	if text == "" {
		return
	}

	threadTS := strings.TrimSpace(event.ThreadTimeStamp)
	if threadTS == "" {
		threadTS = strings.TrimSpace(event.TimeStamp)
	}

	s.processIncoming(slackIncomingMessage{
		TeamID:    teamID,
		ChannelID: strings.TrimSpace(event.Channel),
		ThreadTS:  threadTS,
		UserID:    strings.TrimSpace(event.User),
		Text:      text,
		Source:    "slack",
		EventKey:  slackIncomingEventKey(teamID, event.Channel, event.TimeStamp, event.User),
		Files:     files,
	})
}

func (s *SlackService) handleMessageEvent(ctx context.Context, teamID string, event slackevents.MessageEvent) {
	channelType := strings.TrimSpace(event.ChannelType)
	if strings.TrimSpace(event.User) == "" {
		return
	}
	if strings.TrimSpace(event.BotID) != "" {
		return
	}
	if subtype := strings.TrimSpace(event.SubType); subtype != "" && subtype != "file_share" {
		return
	}
	botUserID := strings.TrimSpace(s.getSetting(ctx, SlackSettingBotUserID))
	if botUserID != "" && strings.TrimSpace(event.User) == botUserID {
		return
	}
	if channelType != "im" && !slackMessageMentionsBot(event, botUserID) {
		return
	}

	files := slackIncomingFilesFromMessage(event.Message)
	text := slackMessageTextOrAttachmentPrompt(sanitizeSlackText(event.Text), len(files) > 0)
	if text == "" {
		return
	}

	threadTS := strings.TrimSpace(event.ThreadTimeStamp)
	if threadTS == "" {
		threadTS = strings.TrimSpace(event.TimeStamp)
	}
	eventTS := strings.TrimSpace(event.TimeStamp)
	if eventTS == "" && event.Message != nil {
		eventTS = strings.TrimSpace(event.Message.Timestamp)
	}

	s.processIncoming(slackIncomingMessage{
		TeamID:    teamID,
		ChannelID: strings.TrimSpace(event.Channel),
		ThreadTS:  threadTS,
		UserID:    strings.TrimSpace(event.User),
		Text:      text,
		Source:    "slack",
		EventKey:  slackIncomingEventKey(teamID, event.Channel, eventTS, event.User),
		Files:     files,
	})
}

type slackIncomingMessage struct {
	TeamID    string
	ChannelID string
	ThreadTS  string
	UserID    string
	Text      string
	Source    string
	EventKey  string
	Files     []slackIncomingFile
}

type slackIncomingFile struct {
	ID                 string
	Name               string
	Title              string
	Mimetype           string
	Size               int
	URLPrivate         string
	URLPrivateDownload string
	FileAccess         string
	Thumb360           string
	Thumb480           string
	Thumb720           string
	Thumb960           string
	Thumb1024          string
}

func (s *SlackService) processIncoming(msg slackIncomingMessage) {
	if !s.claimIncomingMessage(msg) {
		return
	}
	if s.processIncomingMessageFn != nil {
		s.processIncomingMessageFn(msg)
		return
	}
	s.processIncomingMessage(msg)
}

const slackEventDedupeTTL = 10 * time.Minute

func slackIncomingEventKey(teamID, channelID, eventTS, userID string) string {
	channelID = strings.TrimSpace(channelID)
	eventTS = strings.TrimSpace(eventTS)
	userID = strings.TrimSpace(userID)
	if channelID == "" || eventTS == "" || userID == "" {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(teamID),
		channelID,
		eventTS,
		userID,
	}, "|")
}

func (s *SlackService) claimIncomingMessage(msg slackIncomingMessage) bool {
	key := strings.TrimSpace(msg.EventKey)
	if key == "" {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-slackEventDedupeTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processedMessageEvents == nil {
		s.processedMessageEvents = make(map[string]time.Time)
	}
	if processedAt, ok := s.processedMessageEvents[key]; ok && processedAt.After(cutoff) {
		applog.Infof("[slack] ignoring duplicate Slack message event key=%s", key)
		return false
	}
	for eventKey, processedAt := range s.processedMessageEvents {
		if processedAt.Before(cutoff) {
			delete(s.processedMessageEvents, eventKey)
		}
	}
	s.processedMessageEvents[key] = now
	return true
}

func slackIncomingFilesFromAppMention(event slackevents.AppMentionEvent) []slackIncomingFile {
	files := slackIncomingFilesFromSlackFiles(event.Files)
	files = append(files, slackIncomingFilesFromAttachments(event.Attachments)...)
	files = append(files, slackIncomingFilesFromBlocks(event.Blocks)...)
	return dedupeSlackIncomingFiles(files)
}

func slackIncomingFilesFromSlackFiles(files []slack.File) []slackIncomingFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]slackIncomingFile, 0, len(files))
	for _, f := range files {
		out = append(out, slackIncomingFileFromSlackFile(f))
	}
	return out
}

func slackIncomingFileFromSlackFile(f slack.File) slackIncomingFile {
	return slackIncomingFile{
		ID:                 f.ID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Size:               f.Size,
		URLPrivate:         f.URLPrivate,
		URLPrivateDownload: f.URLPrivateDownload,
		Thumb360:           f.Thumb360,
		Thumb480:           f.Thumb480,
		Thumb720:           f.Thumb720,
		Thumb960:           f.Thumb960,
		Thumb1024:          f.Thumb1024,
	}
}

func slackIncomingFilesFromMessage(msg *slack.Msg) []slackIncomingFile {
	if msg == nil {
		return nil
	}
	files := slackIncomingFilesFromSlackFiles(msg.Files)
	files = append(files, slackIncomingFilesFromAttachments(msg.Attachments)...)
	files = append(files, slackIncomingFilesFromBlocks(msg.Blocks)...)
	return dedupeSlackIncomingFiles(files)
}

func slackIncomingFilesFromAttachments(attachments []slack.Attachment) []slackIncomingFile {
	if len(attachments) == 0 {
		return nil
	}
	var files []slackIncomingFile
	for _, att := range attachments {
		name := strings.TrimSpace(att.Title)
		if name == "" {
			name = strings.TrimSpace(att.Fallback)
		}
		if slackIsTrustedFileURL(att.ImageURL) {
			files = append(files, slackIncomingFile{
				Name:       name,
				Title:      att.Title,
				Mimetype:   mediaTypeFromSlackFilename(name),
				Size:       att.ImageBytes,
				URLPrivate: att.ImageURL,
			})
		}
		if slackIsTrustedFileURL(att.ThumbURL) {
			files = append(files, slackIncomingFile{
				Name:       name,
				Title:      att.Title,
				Mimetype:   mediaTypeFromSlackFilename(name),
				Size:       att.ImageBytes,
				URLPrivate: att.ThumbURL,
			})
		}
		files = append(files, slackIncomingFilesFromBlocks(att.Blocks)...)
	}
	return files
}

func slackIncomingFilesFromBlocks(blocks slack.Blocks) []slackIncomingFile {
	if len(blocks.BlockSet) == 0 {
		return nil
	}
	var files []slackIncomingFile
	for _, block := range blocks.BlockSet {
		imageBlock, ok := block.(*slack.ImageBlock)
		if !ok || imageBlock == nil {
			continue
		}
		name := ""
		if imageBlock.Title != nil {
			name = strings.TrimSpace(imageBlock.Title.Text)
		}
		if name == "" {
			name = strings.TrimSpace(imageBlock.AltText)
		}
		if imageBlock.SlackFile != nil {
			file := slackIncomingFile{
				ID:       strings.TrimSpace(imageBlock.SlackFile.ID),
				Name:     name,
				Title:    name,
				Mimetype: mediaTypeFromSlackFilename(name),
			}
			if slackIsTrustedFileURL(imageBlock.SlackFile.URL) {
				file.URLPrivate = strings.TrimSpace(imageBlock.SlackFile.URL)
			}
			if file.ID != "" || file.URLPrivate != "" {
				files = append(files, file)
			}
			continue
		}
		if slackIsTrustedFileURL(imageBlock.ImageURL) {
			files = append(files, slackIncomingFile{
				Name:       name,
				Title:      name,
				Mimetype:   mediaTypeFromSlackFilename(name),
				URLPrivate: strings.TrimSpace(imageBlock.ImageURL),
			})
		}
	}
	return files
}

func dedupeSlackIncomingFiles(files []slackIncomingFile) []slackIncomingFile {
	if len(files) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(files))
	out := make([]slackIncomingFile, 0, len(files))
	for _, f := range files {
		key := strings.TrimSpace(f.ID)
		if key == "" {
			key = strings.TrimSpace(f.URLPrivateDownload)
		}
		if key == "" {
			key = strings.TrimSpace(f.URLPrivate)
		}
		if key == "" {
			key = strings.TrimSpace(f.Name) + "|" + strings.TrimSpace(f.Title)
		}
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		out = append(out, f)
	}
	return out
}

func (s *SlackService) queueChatInput(ctx context.Context, projectID, activeExecID string, msg slackIncomingMessage) bool {
	if s.threadInputRepo == nil {
		return false
	}
	attachmentSessionID := ""
	hasImages := false
	if len(msg.Files) > 0 {
		chatAttachments, err := s.downloadSlackFiles(ctx, msg.Files)
		if err != nil {
			applog.Infof("[slack] queue chat attachment download failed: %v", err)
			msgText := "Failed to process attachment: unable to download attachment. Please try again."
			agent, agentErr := s.autoSelectAgent(ctx, msg.Text, slackIncomingFilesRequireVision(msg.Files))
			if agentErr != nil {
				_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, fmt.Sprintf("Error selecting model: %v", agentErr))
				return true
			}
			s.recordQueuedAttachmentFailure(ctx, projectID, agent.ID, msg, msgText)
			_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "⚠️ "+msgText)
			return true
		}
		hasImages = slackChatAttachmentsContainImage(chatAttachments)
		attachmentSessionID, err = s.saveChatAttachmentsToPendingSession(chatAttachments)
		if err != nil {
			applog.Infof("[slack] queue chat attachment staging failed: %v", err)
			msgText := "Failed to process attachment: unable to store attachment. Please try again."
			agent, agentErr := s.autoSelectAgent(ctx, msg.Text, hasImages)
			if agentErr != nil {
				_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, fmt.Sprintf("Error selecting model: %v", agentErr))
				return true
			}
			s.recordQueuedAttachmentFailure(ctx, projectID, agent.ID, msg, msgText)
			_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "⚠️ "+msgText)
			return true
		}
	}
	agent, err := s.autoSelectAgent(ctx, msg.Text, hasImages)
	if err != nil {
		if attachmentSessionID != "" {
			_ = os.RemoveAll(filepath.Join(s.uploadsDir, "chat", "pending", attachmentSessionID))
		}
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, fmt.Sprintf("Error selecting model: %v", err))
		return true
	}
	queued := &models.ThreadInput{
		Scope:               models.ThreadInputScopeChat,
		ProjectID:           projectID,
		RunExecutionID:      activeExecID,
		AgentConfigID:       agent.ID,
		InputMode:           models.ThreadInputModeQueued,
		InputStatus:         models.ThreadInputPending,
		Content:             msg.Text,
		AttachmentSessionID: attachmentSessionID,
		ChatMode:            models.ChatModeOrchestrate,
		Source:              models.TaskOriginSlack,
		SlackTeamID:         msg.TeamID,
		SlackChannelID:      msg.ChannelID,
		SlackThreadTS:       msg.ThreadTS,
		SlackUserID:         msg.UserID,
	}
	if err := s.threadInputRepo.CreateQueued(ctx, queued); err != nil {
		applog.Infof("[slack] queue chat input failed: %v", err)
		if attachmentSessionID != "" {
			_ = os.RemoveAll(filepath.Join(s.uploadsDir, "chat", "pending", attachmentSessionID))
		}
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "Error queueing your message. Please try again.")
		return true
	}
	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{
			Type:           events.ChatNewMessage,
			ProjectID:      projectID,
			ExecID:         queued.ID,
			Message:        msg.Text,
			Source:         msg.Source,
			Queued:         true,
			HasAttachments: attachmentSessionID != "",
		})
	}
	_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "Queued. I'll send this after the current response finishes.")
	return true
}

func (s *SlackService) recordQueuedAttachmentFailure(ctx context.Context, projectID, agentID string, msg slackIncomingMessage, msgText string) {
	if s.taskRepo == nil || s.execRepo == nil {
		if s.chatBroadcaster != nil {
			s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, Message: msg.Text, Source: msg.Source, Queued: true, HasAttachments: len(msg.Files) > 0})
		}
		return
	}
	task := &models.Task{
		ProjectID:  projectID,
		Title:      fmt.Sprintf("Slack %s: %s", time.Now().Format("15:04:05.000"), util.Truncate(msg.Text, 47)),
		Prompt:     msg.Text,
		Status:     models.StatusPending,
		Category:   models.CategoryChat,
		AgentID:    &agentID,
		CreatedVia: models.TaskOriginSlack,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		applog.Infof("[slack] create queued attachment failure task failed: %v", err)
		if s.chatBroadcaster != nil {
			s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, Message: msg.Text, Source: msg.Source, Queued: true, HasAttachments: len(msg.Files) > 0})
		}
		return
	}
	if s.slackTaskContextRepo != nil {
		if err := s.slackTaskContextRepo.Upsert(ctx, &models.SlackTaskContext{
			TaskID:         task.ID,
			SlackTeamID:    msg.TeamID,
			SlackChannelID: msg.ChannelID,
			SlackThreadTS:  msg.ThreadTS,
			SlackUserID:    msg.UserID,
		}); err != nil {
			applog.Infof("[slack] create queued attachment failure context failed task=%s: %v", task.ID, err)
		}
	}
	exec := &models.Execution{TaskID: task.ID, AgentConfigID: agentID, Status: models.ExecRunning, PromptSent: msg.Text}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		applog.Infof("[slack] create queued attachment failure execution failed task=%s: %v", task.ID, err)
		_ = s.taskRepo.Delete(ctx, task.ID)
		if s.chatBroadcaster != nil {
			s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, Message: msg.Text, Source: msg.Source, Queued: true, HasAttachments: len(msg.Files) > 0})
		}
		return
	}
	s.completeExecution(ctx, exec.ID, task.ID, "", msgText, 0, 0)
	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, ExecID: exec.ID, TaskID: task.ID, Message: msg.Text, Source: msg.Source, HasAttachments: len(msg.Files) > 0})
	}
}

func (s *SlackService) processIncomingMessage(msg slackIncomingMessage) {
	msg.Text = slackMessageTextOrAttachmentPrompt(msg.Text, len(msg.Files) > 0)
	if msg.ChannelID == "" || msg.UserID == "" || strings.TrimSpace(msg.Text) == "" {
		return
	}
	if s.taskRepo == nil || s.execRepo == nil || s.llmConfigRepo == nil || s.llmSvc == nil || s.taskSvc == nil || s.projectRepo == nil {
		applog.Infof("[slack] incoming message ignored: service dependencies are not fully configured")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), slackProcessTimeout)
	defer cancel()
	start := time.Now()

	projectID := s.getActiveProject(ctx, msg.TeamID, msg.UserID)
	if projectID == "" {
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "No active project found. Please create a project first in the web UI.")
		return
	}
	if !s.checkAuthorization(ctx, projectID, msg.UserID) {
		applog.Infof("[slack] unauthorized access blocked for user=%s team=%s project=%s", msg.UserID, msg.TeamID, projectID)
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "You are not authorized to use Slack access for this project. Contact the project owner to get access.")
		return
	}

	msg.Files = s.resolveSlackIncomingFilesForRouting(ctx, msg.Files)
	agent, err := s.autoSelectAgent(ctx, msg.Text, slackIncomingFilesContainImage(msg.Files))
	if err != nil {
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, fmt.Sprintf("Error selecting model: %v", err))
		return
	}

	if activeChatExec, activeErr := s.execRepo.FindLatestActiveChatExecution(ctx, projectID); activeErr != nil {
		applog.Infof("[slack] error checking active chat turn: %v", activeErr)
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "Error checking active chat response. Please try again.")
		return
	} else if activeChatExec != nil {
		if s.queueChatInput(ctx, projectID, activeChatExec.ID, msg) {
			return
		}
	}

	selectedAgentID := agent.ID
	chatTitle := fmt.Sprintf("Slack %s: %s", time.Now().Format("15:04:05.000"), util.Truncate(msg.Text, 47))
	task := &models.Task{
		ProjectID:  projectID,
		Title:      chatTitle,
		Prompt:     msg.Text,
		Status:     models.StatusPending,
		Category:   models.CategoryChat,
		AgentID:    &selectedAgentID,
		CreatedVia: models.TaskOriginSlack,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		applog.Infof("[slack] create chat task failed: %v", err)
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "Error processing your message. Please try again.")
		return
	}

	if s.slackTaskContextRepo != nil {
		if err := s.slackTaskContextRepo.Upsert(ctx, &models.SlackTaskContext{
			TaskID:         task.ID,
			SlackTeamID:    msg.TeamID,
			SlackChannelID: msg.ChannelID,
			SlackThreadTS:  msg.ThreadTS,
			SlackUserID:    msg.UserID,
		}); err != nil {
			applog.Infof("[slack] create chat context failed task=%s: %v", task.ID, err)
			if delErr := s.taskRepo.Delete(ctx, task.ID); delErr != nil {
				applog.Infof("[slack] cleanup chat task failed task=%s: %v", task.ID, delErr)
			}
			_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "Error processing your message. Please try again.")
			return
		}
	}
	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        models.ExecRunning,
		PromptSent:    msg.Text,
	}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		applog.Infof("[slack] create execution failed: %v", err)
		if delErr := s.taskRepo.Delete(ctx, task.ID); delErr != nil {
			applog.Infof("[slack] cleanup chat task failed task=%s after execution create failure: %v", task.ID, delErr)
		}
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "Error processing your message. Please try again.")
		return
	}

	var attachmentContext string
	var imageAttachments []models.Attachment
	hasLinkedAttachments := false
	if len(msg.Files) > 0 {
		attCtx, imgAtts, chatAtts, err := s.downloadSlackAttachments(ctx, msg.Files)
		if err != nil {
			applog.Infof("[slack] attachment download error: %v", err)
			msgText := fmt.Sprintf("Failed to process attachment: %v", err)
			s.completeExecution(ctx, exec.ID, task.ID, "", msgText, 0, time.Since(start).Milliseconds())
			if s.chatBroadcaster != nil {
				s.chatBroadcaster.Publish(events.ChatEvent{
					Type:           events.ChatNewMessage,
					ProjectID:      projectID,
					ExecID:         exec.ID,
					TaskID:         task.ID,
					Message:        msg.Text,
					Source:         msg.Source,
					AgentName:      agent.Name,
					HasAttachments: len(msg.Files) > 0,
				})
			}
			_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "⚠️ "+msgText)
			return
		}
		attachmentContext = attCtx
		imageAttachments = imgAtts
		if len(chatAtts) > 0 {
			var err error
			chatAtts, err = s.linkAttachmentsToExecution(ctx, exec.ID, chatAtts)
			if err != nil {
				applog.Infof("[slack] attachment link error: %v", err)
				msgText := "Failed to process attachment: unable to store attachment. Please try again."
				s.completeExecution(ctx, exec.ID, task.ID, "", msgText, 0, time.Since(start).Milliseconds())
				if s.chatBroadcaster != nil {
					s.chatBroadcaster.Publish(events.ChatEvent{
						Type:           events.ChatNewMessage,
						ProjectID:      projectID,
						ExecID:         exec.ID,
						TaskID:         task.ID,
						Message:        msg.Text,
						Source:         msg.Source,
						AgentName:      agent.Name,
						HasAttachments: len(msg.Files) > 0,
					})
				}
				_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, "⚠️ "+msgText)
				return
			}
			attachmentContext, imageAttachments = slackAttachmentContextAndImages(chatAtts)
			hasLinkedAttachments = true
		}
	}
	if len(imageAttachments) > 0 {
		if selectedAgent, selectErr := s.autoSelectAgent(ctx, msg.Text, true); selectErr == nil && selectedAgent != nil {
			agent = selectedAgent
		} else if selectErr != nil {
			applog.Infof("[slack] vision model selection after attachment download failed; using initially selected model: %v", selectErr)
		}
	}

	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{
			Type:           events.ChatNewMessage,
			ProjectID:      projectID,
			ExecID:         exec.ID,
			TaskID:         task.ID,
			Message:        msg.Text,
			Source:         msg.Source,
			AgentName:      agent.Name,
			HasAttachments: hasLinkedAttachments,
		})
	}

	chatHistory, err := s.execRepo.ListChatHistory(ctx, projectID, slackChatHistoryLimit)
	if err != nil {
		chatHistory = []models.Execution{}
	}
	priorHistory := filterSlackChatHistory(chatHistory, exec.ID)

	systemContext := s.buildChatContext(ctx, projectID)
	if attachmentContext != "" {
		systemContext = systemContext + attachmentContext
	}
	if personalityPrompt := s.getPersonalityContext(ctx, projectID); personalityPrompt != "" {
		systemContext = systemContext + personalityPrompt
	}
	workDir := s.resolveWorkDir(ctx, projectID)

	if s.channelChatRunner == nil {
		msgText := "Slack chat runner is unavailable. Please restart OpenVibely and try again."
		s.completeExecution(ctx, exec.ID, task.ID, "", msgText, 0, time.Since(start).Milliseconds())
		_ = s.sendSlackMessage(msg.ChannelID, msg.ThreadTS, msgText)
		return
	}
	s.channelChatRunner(context.Background(), ChannelChatRunRequest{
		ExecID:           exec.ID,
		TaskID:           task.ID,
		ProjectID:        projectID,
		Message:          msg.Text,
		Agent:            *agent,
		ChatHistory:      priorHistory,
		SystemContext:    systemContext,
		WorkDir:          workDir,
		ImageAttachments: imageAttachments,
		Surface:          chatcontrol.SurfaceSlack,
		ReplyContext: ChannelReplyContext{
			Source:         models.TaskOriginSlack,
			SlackTeamID:    msg.TeamID,
			SlackChannelID: msg.ChannelID,
			SlackThreadTS:  msg.ThreadTS,
			SlackUserID:    msg.UserID,
		},
	})
	return
}

func (s *SlackService) completeExecution(ctx context.Context, execID, taskID, output, errorMessage string, tokensUsed int, durationMs int64) {
	if s.execRepo == nil || s.taskRepo == nil {
		return
	}
	if errorMessage != "" {
		if err := s.execRepo.Complete(ctx, execID, models.ExecFailed, "", errorMessage, 0, durationMs); err != nil {
			applog.Infof("[slack] complete failed execution error: %v", err)
		}
		if err := s.taskRepo.UpdateStatus(ctx, taskID, models.StatusFailed); err != nil {
			applog.Infof("[slack] update failed task status error: %v", err)
		}
		s.promoteQueuedChatAfterCompletion(ctx, taskID)
		return
	}

	if err := s.execRepo.Complete(ctx, execID, models.ExecCompleted, output, "", tokensUsed, durationMs); err != nil {
		applog.Infof("[slack] complete execution error: %v", err)
	}
	if err := s.taskRepo.UpdateStatus(ctx, taskID, models.StatusCompleted); err != nil {
		applog.Infof("[slack] update task status error: %v", err)
	}
	s.promoteQueuedChatAfterCompletion(ctx, taskID)
}

func (s *SlackService) promoteQueuedChatAfterCompletion(ctx context.Context, taskID string) {
	if s.queuedTurnPromoter == nil || s.taskRepo == nil {
		return
	}
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil || task == nil || task.Category != models.CategoryChat {
		return
	}
	s.queuedTurnPromoter(task.ProjectID)
}

func (s *SlackService) autoSelectAgent(ctx context.Context, message string, hasImages bool) (*models.LLMConfig, error) {
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

func (s *SlackService) buildChatContext(ctx context.Context, projectID string) string {
	existingTasks := []models.Task{}
	if s.taskSvc != nil {
		tasks, err := s.taskSvc.ListByProject(ctx, projectID, "")
		if err == nil {
			existingTasks = tasks
		}
	}
	availableModels := []models.LLMConfig{}
	if s.llmConfigRepo != nil {
		availableModels, _ = s.llmConfigRepo.List(ctx)
	}
	var schedules []models.Schedule
	if s.scheduleRepo != nil {
		var err error
		schedules, err = s.scheduleRepo.ListByProject(ctx, projectID)
		if err != nil {
			schedules = []models.Schedule{}
		}
	}
	return BuildChatContextWithAgentDefinitions(existingTasks, availableModels, s.listChatAssignableAgentDefinitions(ctx), schedules, time.Now())
}

func (s *SlackService) listChatAssignableAgentDefinitions(ctx context.Context) []models.Agent {
	if s.agentRepo == nil {
		return nil
	}
	agents, err := s.agentRepo.List(ctx)
	if err != nil {
		applog.Infof("[slack] error listing agent definitions for context: %v", err)
		return nil
	}
	return UniqueChatAssignableAgentDefinitions(agents)
}

func (s *SlackService) resolveWorkDir(ctx context.Context, projectID string) string {
	if s.projectRepo == nil {
		return ""
	}
	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil || project == nil {
		return ""
	}
	return project.RepoPath
}

func (s *SlackService) getPersonalityContext(ctx context.Context, projectID string) string {
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

type slackMarkerContext struct {
	TeamID    string
	ChannelID string
	ThreadTS  string
	UserID    string
}

func (s *SlackService) buildSlackActionToolRuntime(projectID string, markerCtx slackMarkerContext, collector *channelActionSummaryCollector) *llmcontracts.RuntimeTools {
	handlers := s.slackActionHandlers(projectID, markerCtx, collector)
	return &llmcontracts.RuntimeTools{
		Definitions: actionToolDefinitions(chatcontrol.SurfaceSlack, true),
		Executor:    chatcontrol.BuildRuntimeToolExecutor(models.ChatModeOrchestrate, chatcontrol.SurfaceSlack, handlers),
	}
}

func (s *SlackService) slackActionHandlers(projectID string, markerCtx slackMarkerContext, collector *channelActionSummaryCollector) map[string]chatcontrol.RuntimeActionHandler {
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
					if err := s.taskRepo.UpdateSlackOrigin(ctx, t.ID); err != nil {
						applog.Infof("[slack] runtime create_task update slack origin failed for task=%s: %v", t.ID, err)
					}
				}
				if s.slackTaskContextRepo != nil {
					_ = s.slackTaskContextRepo.Upsert(ctx, &models.SlackTaskContext{
						TaskID:         t.ID,
						SlackTeamID:    markerCtx.TeamID,
						SlackChannelID: markerCtx.ChannelID,
						SlackThreadTS:  markerCtx.ThreadTS,
						SlackUserID:    markerCtx.UserID,
					})
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
			summary := ExecuteTaskExecutions(ctx, []TaskExecutionRequest{req}, projectID, s.taskSvc)
			return strings.TrimSpace(summary), nil
		},
		"view_task_thread": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackViewTaskThread(ctx, projectID, input), nil
		},
		"send_to_task": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackSendToTask(ctx, projectID, input, markerCtx), nil
		},
		"send_message": func(ctx context.Context, input json.RawMessage) (string, error) {
			if s.channelMessageRouter == nil {
				return "", fmt.Errorf("channel message router unavailable")
			}
			return ExecuteSendMessageTool(ctx, s.channelMessageRouter.WithAuditContext(string(chatcontrol.SurfaceSlack), markerCtx.UserID), projectID, input)
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
		"schedule_task": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackScheduleTask(ctx, projectID, input), nil
		},
		"delete_schedule": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackDeleteSchedule(ctx, projectID, input), nil
		},
		"modify_schedule": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackModifySchedule(ctx, projectID, input), nil
		},
		"list_personalities": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackListPersonalities(ctx), nil
		},
		"set_personality": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackSetPersonality(ctx, input), nil
		},
		"list_models": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackListModels(ctx), nil
		},
		"list_agents": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackListAgents(ctx), nil
		},
		"view_settings": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackViewSettings(ctx), nil
		},
		"project_info": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackProjectInfo(ctx, projectID), nil
		},
		"create_alert": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackCreateAlert(ctx, projectID, input), nil
		},
		"delete_alert": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackDeleteAlert(ctx, input), nil
		},
		"toggle_alert": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackToggleAlert(ctx, input), nil
		},
		"list_projects": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.buildProjectListResult(ctx, projectID), nil
		},
		"switch_project": func(ctx context.Context, input json.RawMessage) (string, error) {
			var req SwitchProjectRequest
			if err := decodeRuntimeToolInput(input, &req); err != nil {
				return "", err
			}
			return s.switchProjectResult(ctx, markerCtx.TeamID, markerCtx.UserID, req.Project), nil
		},
		"get_personality": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackGetPersonality(ctx), nil
		},
		"get_model": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackGetModel(ctx, input), nil
		},
		"get_current_project": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackGetCurrentProject(ctx, projectID), nil
		},
		"list_alerts": func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.slackListAlerts(ctx, projectID), nil
		},
		"get_alert": func(ctx context.Context, input json.RawMessage) (string, error) {
			return s.slackGetAlert(ctx, input), nil
		},
		"get_chat_mode": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "Current chat mode: orchestrate", nil
		},
		"set_chat_mode": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "Chat mode changes are not supported on Slack. Slack always uses orchestrate mode.", nil
		},
		"list_capabilities": func(_ context.Context, _ json.RawMessage) (string, error) {
			summaries := chatcontrol.ListForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceSlack)
			return formatChannelCapabilities(summaries), nil
		},
	}
}

func (s *SlackService) buildProjectListResult(ctx context.Context, projectID string) string {
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

func (s *SlackService) switchProjectResult(ctx context.Context, teamID, userID, targetProject string) string {
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
		var names []string
		for _, p := range projects {
			names = append(names, p.Name)
		}
		return fmt.Sprintf("Project not found: %q. Available projects: %s", targetProject, strings.Join(names, ", "))
	}

	s.setActiveProject(ctx, teamID, userID, target.ID)
	return fmt.Sprintf("Switched to project: %s", target.Name)
}

// ---- New channel action executors for Slack ----

func (s *SlackService) slackGetPersonality(ctx context.Context) string {
	if s.settingsRepo == nil {
		return "Current personality: default (no personality set)"
	}
	current, err := s.settingsRepo.Get(ctx, "personality")
	if err != nil {
		applog.Infof("[slack] slackGetPersonality error: %v", err)
		return "Error retrieving personality setting."
	}
	if current == "" {
		return "Current personality: default (base, no personality modifier active)"
	}
	return fmt.Sprintf("Current personality: %s", current)
}

func (s *SlackService) slackGetModel(ctx context.Context, input json.RawMessage) string {
	var req struct {
		ModelID string `json:"model_id"`
		Name    string `json:"name"`
	}
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for get_model."
	}
	configs, err := s.llmConfigRepo.List(ctx)
	if err != nil {
		return "Error retrieving model configurations."
	}
	for _, c := range configs {
		if (req.ModelID != "" && c.ID == req.ModelID) ||
			(req.Name != "" && strings.EqualFold(c.Name, req.Name)) {
			defaultStr := ""
			if c.IsDefault {
				defaultStr = " (default)"
			}
			return fmt.Sprintf("Model: %s%s\n  Provider: %s\n  Model ID: %s\n  Auth: %s",
				c.Name, defaultStr, c.Provider, c.Model, c.AuthMethod)
		}
	}
	if req.ModelID != "" {
		return fmt.Sprintf("Model with id %q not found.", req.ModelID)
	}
	return fmt.Sprintf("Model with name %q not found.", req.Name)
}

func (s *SlackService) slackGetCurrentProject(ctx context.Context, projectID string) string {
	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil || project == nil {
		return fmt.Sprintf("Current project ID: %s (details unavailable)", projectID)
	}
	return fmt.Sprintf("Current project: %s (id: %s)", project.Name, project.ID)
}

func (s *SlackService) slackListAlerts(ctx context.Context, projectID string) string {
	if s.alertSvc == nil {
		return "Alert service not available."
	}
	alerts, err := s.alertSvc.ListByProject(ctx, projectID, 50)
	if err != nil {
		return "Error retrieving alerts: " + err.Error()
	}
	if len(alerts) == 0 {
		return "No alerts found. You're all clear!"
	}
	unreadCount, _ := s.alertSvc.CountUnread(ctx, projectID)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d alerts (%d unread):\n", len(alerts), unreadCount))
	for _, a := range alerts {
		readStr := "unread"
		if a.IsRead {
			readStr = "read"
		}
		sb.WriteString(fmt.Sprintf("- %s (id: %s, severity: %s, %s)\n", a.Title, a.ID, a.Severity, readStr))
	}
	return strings.TrimSpace(sb.String())
}

func (s *SlackService) slackGetAlert(ctx context.Context, input json.RawMessage) string {
	var req struct {
		AlertID string `json:"alert_id"`
	}
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for get_alert."
	}
	if req.AlertID == "" {
		return "get_alert requires alert_id."
	}
	if s.alertSvc == nil {
		return "Alert service not available."
	}
	alert, err := s.alertSvc.GetByID(ctx, req.AlertID)
	if err != nil {
		return fmt.Sprintf("Error retrieving alert %q: %v", req.AlertID, err)
	}
	if alert == nil {
		return fmt.Sprintf("Alert %q not found.", req.AlertID)
	}
	readStr := "unread"
	if alert.IsRead {
		readStr = "read"
	}
	return fmt.Sprintf("Alert: %s\n  ID: %s\n  Type: %s\n  Severity: %s\n  Status: %s\n  Message: %s",
		alert.Title, alert.ID, alert.Type, alert.Severity, readStr, alert.Message)
}

func (s *SlackService) slackViewTaskThread(ctx context.Context, projectID string, input json.RawMessage) string {
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

func (s *SlackService) taskHasStartingFirstTurn(ctx context.Context, task *models.Task) (bool, error) {
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

func (s *SlackService) activeOrStartingTaskTurn(ctx context.Context, task *models.Task) (*models.Execution, bool, error) {
	if s.execRepo == nil {
		return nil, false, nil
	}
	activeExec, err := s.execRepo.FindActiveTaskExecution(ctx, task.ID, "")
	if err != nil {
		return nil, false, err
	}
	if activeExec != nil {
		return activeExec, false, nil
	}
	starting, err := s.taskHasStartingFirstTurn(ctx, task)
	return nil, starting, err
}

func (s *SlackService) bindQueuedTaskInputToActiveExecutionIfAvailable(ctx context.Context, input *models.ThreadInput) error {
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

func (s *SlackService) shouldPromotePreExecutionQueuedInput(ctx context.Context, task *models.Task, input *models.ThreadInput) (bool, error) {
	if task == nil || input == nil || input.RunExecutionID != "" {
		return false, nil
	}
	starting, err := s.taskHasStartingFirstTurn(ctx, task)
	if err != nil || starting {
		return false, err
	}
	return true, nil
}

func (s *SlackService) slackSendToTask(ctx context.Context, projectID string, input json.RawMessage, markerCtx slackMarkerContext) string {
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
	if activeExec == nil && !queueBehindFirstTurn {
		activeExec, _, activeErr = s.activeOrStartingTaskTurn(ctx, task)
		if activeErr != nil {
			return fmt.Sprintf("Error checking active turn for task %q: %v", task.Title, activeErr)
		}
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
		queued := &models.ThreadInput{
			Scope:          models.ThreadInputScopeTask,
			ProjectID:      task.ProjectID,
			TaskID:         task.ID,
			RunExecutionID: runExecutionID,
			AgentConfigID:  agentID,
			InputMode:      models.ThreadInputModeQueued,
			InputStatus:    models.ThreadInputPending,
			Content:        req.Message,
			Source:         models.TaskOriginSlack,
			SlackTeamID:    markerCtx.TeamID,
			SlackChannelID: markerCtx.ChannelID,
			SlackThreadTS:  markerCtx.ThreadTS,
			SlackUserID:    markerCtx.UserID,
		}
		if err := s.threadInputRepo.CreateQueued(ctx, queued); err != nil {
			return fmt.Sprintf("Error queueing message for task %q: %v", task.Title, err)
		}
		if queued.RunExecutionID == "" {
			if err := s.bindQueuedTaskInputToActiveExecutionIfAvailable(ctx, queued); err != nil {
				applog.Infof("[slack] send_to_task task=%s input=%s active execution bind skipped: %v", task.ID, queued.ID, err)
			}
		}
		if shouldPromote, promoteErr := s.shouldPromotePreExecutionQueuedInput(ctx, task, queued); promoteErr != nil {
			applog.Infof("[slack] send_to_task task=%s input=%s promotion recheck skipped: %v", task.ID, queued.ID, promoteErr)
		} else if shouldPromote && s.queuedTaskThreadPromoter != nil {
			go s.queuedTaskThreadPromoter(task.ID)
		}
		return fmt.Sprintf("Queued message to task %q [TASK_ID:%s]. It will run after the active thread turn finishes.", task.Title, task.ID)
	}
	var agent *models.LLMConfig
	if task.AgentID != nil {
		agent, _ = s.llmConfigRepo.GetByID(ctx, *task.AgentID)
	}
	if agent == nil {
		agent, err = s.autoSelectAgent(ctx, req.Message, false)
		if err != nil {
			return fmt.Sprintf("Error selecting agent for task %q: %v", task.Title, err)
		}
	}
	exec := &models.Execution{TaskID: task.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: req.Message, IsFollowup: true}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		return fmt.Sprintf("Error creating follow-up execution for %q: %v", task.Title, err)
	}
	priorExecs, _ := s.execRepo.ListByTaskChronological(ctx, task.ID)
	priorHistory := filterSlackChatHistory(priorExecs, exec.ID)
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
		if err := s.taskRepo.UpdateCategory(ctx, task.ID, models.CategoryActive); err != nil {
			applog.Infof("[slack] runtime send_to_task error updating category for task %s: %v", task.ID, err)
		}
	}
	s.channelTaskRunner(context.Background(), ChannelTaskRunRequest{
		ExecID:        exec.ID,
		TaskID:        task.ID,
		ProjectID:     task.ProjectID,
		Message:       req.Message,
		Agent:         *agent,
		ChatHistory:   priorHistory,
		SystemContext: systemContext,
		Surface:       chatcontrol.SurfaceSlack,
		ReplyContext: ChannelReplyContext{
			Source:         models.TaskOriginSlack,
			SlackTeamID:    markerCtx.TeamID,
			SlackChannelID: markerCtx.ChannelID,
			SlackThreadTS:  markerCtx.ThreadTS,
			SlackUserID:    markerCtx.UserID,
		},
	})
	return fmt.Sprintf("Sent message to task %q [TASK_ID:%s] and started processing.", task.Title, task.ID)
}

func (s *SlackService) slackScheduleTask(ctx context.Context, projectID string, input json.RawMessage) string {
	var req ScheduleTaskRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for schedule_task."
	}
	if strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Title) == "" {
		return "schedule_task requires task_id or title."
	}
	if strings.TrimSpace(req.Time) == "" {
		return "schedule_task requires time."
	}
	if s.scheduleRepo == nil {
		return "Error scheduling task: schedule repository not available."
	}
	task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
	if err != nil {
		return fmt.Sprintf("Could not find task: %v", err)
	}
	var hourVal, minuteVal int
	if _, err := fmt.Sscanf(req.Time, "%d:%d", &hourVal, &minuteVal); err != nil || hourVal < 0 || hourVal > 23 || minuteVal < 0 || minuteVal > 59 {
		return fmt.Sprintf("Invalid time %q (expected HH:MM).", req.Time)
	}
	repeatType := models.RepeatDaily
	switch strings.ToLower(req.Repeat) {
	case "once":
		repeatType = models.RepeatOnce
	case "daily", "":
		repeatType = models.RepeatDaily
	case "weekly":
		repeatType = models.RepeatWeekly
	case "monthly":
		repeatType = models.RepeatMonthly
	case "hours", "hourly":
		repeatType = models.RepeatHours
	case "minutes":
		repeatType = models.RepeatMinutes
	case "seconds":
		repeatType = models.RepeatSeconds
	}
	repeatInterval := 1
	if req.Interval > 0 {
		repeatInterval = req.Interval
	}
	now := time.Now().Local()
	runAt := time.Date(now.Year(), now.Month(), now.Day(), hourVal, minuteVal, 0, 0, time.Local)
	schedule := &models.Schedule{TaskID: task.ID, RunAt: runAt.UTC(), RepeatType: repeatType, RepeatInterval: repeatInterval, Enabled: true}
	if err := s.scheduleRepo.Create(ctx, schedule); err != nil {
		return fmt.Sprintf("Error scheduling task %q: %v", task.Title, err)
	}
	if task.Category != models.CategoryScheduled {
		_ = s.taskRepo.UpdateCategory(ctx, task.ID, models.CategoryScheduled)
		if task.Status != models.StatusPending {
			_ = s.taskRepo.UpdateStatus(ctx, task.ID, models.StatusPending)
		}
	}
	return fmt.Sprintf("Scheduled task %q [TASK_ID:%s] at %s (%s).", task.Title, task.ID, req.Time, FormatRepeatPattern(repeatType, repeatInterval))
}

func (s *SlackService) slackDeleteSchedule(ctx context.Context, projectID string, input json.RawMessage) string {
	var req DeleteScheduleRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for delete_schedule."
	}
	if strings.TrimSpace(req.ScheduleID) == "" && strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Title) == "" {
		return "delete_schedule requires schedule_id, task_id, or title."
	}
	if s.scheduleRepo == nil {
		return "Error deleting schedule: schedule repository not available."
	}
	schedule, task, err := s.resolveScheduleReference(ctx, projectID, req.ScheduleID, req.TaskID, req.Title)
	if err != nil {
		return fmt.Sprintf("Could not find schedule: %v", err)
	}
	if err := s.scheduleRepo.Delete(ctx, schedule.ID); err != nil {
		return fmt.Sprintf("Error deleting schedule for task %q: %v", task.Title, err)
	}
	remaining, _ := s.scheduleRepo.ListByTask(ctx, task.ID)
	if len(remaining) == 0 && task.Category == models.CategoryScheduled {
		_ = s.taskRepo.UpdateCategory(ctx, task.ID, models.CategoryBacklog)
	}
	return fmt.Sprintf("Deleted schedule for task %q [TASK_ID:%s].", task.Title, task.ID)
}

func (s *SlackService) slackModifySchedule(ctx context.Context, projectID string, input json.RawMessage) string {
	var req ModifyScheduleRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for modify_schedule."
	}
	if strings.TrimSpace(req.ScheduleID) == "" && strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Title) == "" {
		return "modify_schedule requires schedule_id, task_id, or title."
	}
	if s.scheduleRepo == nil {
		return "Error modifying schedule: schedule repository not available."
	}
	schedule, task, err := s.resolveScheduleReference(ctx, projectID, req.ScheduleID, req.TaskID, req.Title)
	if err != nil {
		return fmt.Sprintf("Could not find schedule: %v", err)
	}
	var changes []string
	if req.Time != "" {
		var hourVal, minuteVal int
		if _, err := fmt.Sscanf(req.Time, "%d:%d", &hourVal, &minuteVal); err != nil || hourVal < 0 || hourVal > 23 || minuteVal < 0 || minuteVal > 59 {
			return fmt.Sprintf("Invalid time %q.", req.Time)
		}
		oldLocal := schedule.RunAt.Local()
		schedule.RunAt = time.Date(oldLocal.Year(), oldLocal.Month(), oldLocal.Day(), hourVal, minuteVal, 0, 0, time.Local).UTC()
		changes = append(changes, fmt.Sprintf("time→%s", req.Time))
	}
	if req.Repeat != "" {
		switch strings.ToLower(req.Repeat) {
		case "once":
			schedule.RepeatType = models.RepeatOnce
		case "daily":
			schedule.RepeatType = models.RepeatDaily
		case "weekly":
			schedule.RepeatType = models.RepeatWeekly
		case "monthly":
			schedule.RepeatType = models.RepeatMonthly
		case "hours", "hourly":
			schedule.RepeatType = models.RepeatHours
		case "minutes":
			schedule.RepeatType = models.RepeatMinutes
		case "seconds":
			schedule.RepeatType = models.RepeatSeconds
		default:
			return fmt.Sprintf("Unknown repeat type %q.", req.Repeat)
		}
		changes = append(changes, fmt.Sprintf("repeat→%s", req.Repeat))
	}
	if req.Interval != nil {
		if *req.Interval < 1 {
			return fmt.Sprintf("Invalid interval %d.", *req.Interval)
		}
		schedule.RepeatInterval = *req.Interval
		changes = append(changes, fmt.Sprintf("interval→%d", *req.Interval))
	}
	if req.Enabled != nil {
		schedule.Enabled = *req.Enabled
		if *req.Enabled {
			changes = append(changes, "enabled→true")
		} else {
			changes = append(changes, "enabled→false")
		}
	}
	if len(changes) == 0 {
		return fmt.Sprintf("No changes specified for schedule on task %q.", task.Title)
	}
	// Match HTTP-toggle semantics: recompute only when time-related fields
	// changed; on re-enable recompute stale NextRun; on disable preserve it.
	slackTimeChanged := req.Time != "" || req.Repeat != "" || req.Interval != nil
	if slackTimeChanged {
		schedule.NextRun = schedule.ComputeNextRun(time.Now())
	} else if req.Enabled != nil && *req.Enabled {
		sNow := time.Now()
		if schedule.NextRun == nil || schedule.NextRun.Before(sNow) {
			if next := schedule.ComputeNextRun(sNow); next != nil {
				schedule.NextRun = next
			}
		}
	}
	if err := s.scheduleRepo.Update(ctx, schedule); err != nil {
		return fmt.Sprintf("Error updating schedule for task %q: %v", task.Title, err)
	}
	return fmt.Sprintf("Updated schedule for task %q [TASK_ID:%s]: %s.", task.Title, task.ID, strings.Join(changes, ", "))
}

func (s *SlackService) slackListPersonalities(ctx context.Context) string {
	personalities := AllPersonalitiesWithCustom(ctx, s.customPersonalityRepo)
	if len(personalities) == 0 {
		return "No personalities available."
	}
	var sb strings.Builder
	sb.WriteString("Available Personalities:\n")
	for _, p := range personalities {
		if p.Key == "" {
			sb.WriteString(fmt.Sprintf("- %s (default): %s\n", p.Name, p.Description))
		} else if p.IsCustom {
			sb.WriteString(fmt.Sprintf("- %s (key: %s, custom): %s\n", p.Name, p.Key, p.Description))
		} else {
			sb.WriteString(fmt.Sprintf("- %s (key: %s): %s\n", p.Name, p.Key, p.Description))
		}
	}
	if s.settingsRepo != nil {
		if current, err := s.settingsRepo.Get(ctx, "personality"); err == nil {
			if current == "" {
				current = "default"
			}
			sb.WriteString(fmt.Sprintf("\nCurrent personality: %s", current))
		}
	}
	return strings.TrimSpace(sb.String())
}

func (s *SlackService) slackSetPersonality(ctx context.Context, input json.RawMessage) string {
	var req struct {
		Personality string `json:"personality"`
	}
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for set_personality."
	}
	key := strings.TrimSpace(req.Personality)
	if key == "" {
		return "set_personality requires personality."
	}
	valid := false
	for _, p := range AllPersonalitiesWithCustom(ctx, s.customPersonalityRepo) {
		if p.Key == key {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Sprintf("Unknown personality %q. Use list_personalities to view options.", key)
	}
	if s.settingsRepo == nil {
		return "Error setting personality: settings repository not configured."
	}
	if err := s.settingsRepo.Set(ctx, "personality", key); err != nil {
		return fmt.Sprintf("Error setting personality to %q: %v", key, err)
	}
	return fmt.Sprintf("Personality changed to %q.", key)
}

func (s *SlackService) slackListModels(ctx context.Context) string {
	configs, err := s.llmConfigRepo.List(ctx)
	if err != nil {
		return "Error retrieving model configurations."
	}
	if len(configs) == 0 {
		return "No models configured."
	}
	var sb strings.Builder
	sb.WriteString("Configured Models:\n")
	for _, c := range configs {
		defaultStr := ""
		if c.IsDefault {
			defaultStr = " (default)"
		}
		auth := string(c.AuthMethod)
		if auth == "" {
			auth = "cli"
		}
		sb.WriteString(fmt.Sprintf("- %s%s — provider: %s, model: %s, auth: %s\n", c.Name, defaultStr, c.Provider, c.Model, auth))
	}
	return strings.TrimSpace(sb.String())
}

func (s *SlackService) slackListAgents(ctx context.Context) string {
	return "Agent listing is currently unavailable on Slack (no agent repository configured on this surface)."
}

func (s *SlackService) slackViewSettings(ctx context.Context) string {
	var sb strings.Builder
	sb.WriteString("App Settings:\n")
	if s.settingsRepo != nil {
		personality, err := s.settingsRepo.Get(ctx, "personality")
		if err == nil {
			if personality == "" {
				personality = "default"
			}
			sb.WriteString(fmt.Sprintf("- Personality: %s\n", personality))
		}
	}
	if configs, err := s.llmConfigRepo.List(ctx); err == nil {
		sb.WriteString(fmt.Sprintf("- Configured models: %d\n", len(configs)))
	}
	if s.projectRepo != nil {
		if projects, err := s.projectRepo.List(ctx); err == nil {
			sb.WriteString(fmt.Sprintf("- Projects: %d\n", len(projects)))
		}
	}
	return strings.TrimSpace(sb.String())
}

func (s *SlackService) slackProjectInfo(ctx context.Context, projectID string) string {
	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil || project == nil {
		return "Error retrieving project details."
	}
	counts, err := s.taskRepo.CountByProjectAndCategory(ctx, projectID)
	if err != nil {
		counts = map[string]int{}
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Project: %s (id: %s)\n", project.Name, project.ID))
	if project.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n", project.Description))
	}
	if project.RepoPath != "" {
		sb.WriteString(fmt.Sprintf("Repository: %s\n", project.RepoPath))
	}
	sb.WriteString(fmt.Sprintf("Total tasks: %d\n", total))
	for category, count := range counts {
		sb.WriteString(fmt.Sprintf("- %s: %d\n", category, count))
	}
	return strings.TrimSpace(sb.String())
}

func (s *SlackService) slackCreateAlert(ctx context.Context, projectID string, input json.RawMessage) string {
	var req CreateAlertRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for create_alert."
	}
	if strings.TrimSpace(req.Title) == "" {
		return "create_alert requires title."
	}
	if s.alertSvc == nil {
		return "Alert service not available."
	}
	severity := models.SeverityInfo
	switch strings.ToLower(strings.TrimSpace(req.Severity)) {
	case "warning":
		severity = models.SeverityWarning
	case "error":
		severity = models.SeverityError
	case "", "info":
		severity = models.SeverityInfo
	default:
		return fmt.Sprintf("Invalid severity %q.", req.Severity)
	}
	alertType := models.AlertCustom
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "task_failed":
		alertType = models.AlertTaskFailed
	case "task_needs_followup":
		alertType = models.AlertTaskNeedsFollowup
	case "", "custom":
		alertType = models.AlertCustom
	default:
		return fmt.Sprintf("Invalid alert type %q.", req.Type)
	}
	a := &models.Alert{ProjectID: projectID, Type: alertType, Severity: severity, Title: req.Title, Message: req.Message}
	if strings.TrimSpace(req.TaskID) != "" {
		tid := strings.TrimSpace(req.TaskID)
		a.TaskID = &tid
	}
	if err := s.alertSvc.Create(ctx, a); err != nil {
		return fmt.Sprintf("Error creating alert %q: %v", req.Title, err)
	}
	return fmt.Sprintf("Created alert %q (id: %s, severity: %s)", req.Title, a.ID, severity)
}

func (s *SlackService) slackDeleteAlert(ctx context.Context, input json.RawMessage) string {
	var req DeleteAlertRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for delete_alert."
	}
	if strings.TrimSpace(req.AlertID) == "" {
		return "delete_alert requires alert_id."
	}
	if s.alertSvc == nil {
		return "Alert service not available."
	}
	if err := s.alertSvc.Delete(ctx, req.AlertID); err != nil {
		return fmt.Sprintf("Error deleting alert %q: %v", req.AlertID, err)
	}
	return fmt.Sprintf("Deleted alert %s.", req.AlertID)
}

func (s *SlackService) slackToggleAlert(ctx context.Context, input json.RawMessage) string {
	var req ToggleAlertRequest
	if err := decodeRuntimeToolInput(input, &req); err != nil {
		return "Invalid input for toggle_alert."
	}
	if strings.TrimSpace(req.AlertID) == "" {
		return "toggle_alert requires alert_id."
	}
	if s.alertSvc == nil {
		return "Alert service not available."
	}
	if err := s.alertSvc.MarkRead(ctx, req.AlertID); err != nil {
		return fmt.Sprintf("Error marking alert %q as read: %v", req.AlertID, err)
	}
	return fmt.Sprintf("Marked alert %s as read.", req.AlertID)
}

func (s *SlackService) resolveTaskReference(ctx context.Context, projectID, taskID, title string) (*models.Task, error) {
	if taskID != "" {
		task, err := s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("error looking up task %s: %w", taskID, err)
		}
		if task == nil {
			return nil, fmt.Errorf("task %s not found", taskID)
		}
		if task.ProjectID != projectID {
			return nil, fmt.Errorf("task %s belongs to a different project", taskID)
		}
		return task, nil
	}
	if title != "" {
		tasks, err := s.taskRepo.SearchByTitle(ctx, projectID, title)
		if err != nil {
			return nil, fmt.Errorf("error searching for task %q: %w", title, err)
		}
		if len(tasks) == 0 {
			return nil, fmt.Errorf("no task found matching %q", title)
		}
		return &tasks[0], nil
	}
	return nil, fmt.Errorf("no task_id or title provided")
}

func (s *SlackService) processScheduleTask(ctx context.Context, execID, projectID, output string) string {
	requests := ParseScheduleTask(output)
	if len(requests) == 0 {
		return output
	}
	var results []string
	for _, req := range requests {
		task, err := s.resolveTaskReference(ctx, projectID, req.TaskID, req.Title)
		if err != nil {
			results = append(results, fmt.Sprintf("- Could not find task: %v", err))
			continue
		}
		if s.scheduleRepo == nil {
			results = append(results, fmt.Sprintf("- Error scheduling task %q: schedule repository not available", task.Title))
			continue
		}
		var hourVal, minuteVal int
		if _, err := fmt.Sscanf(req.Time, "%d:%d", &hourVal, &minuteVal); err != nil || hourVal < 0 || hourVal > 23 || minuteVal < 0 || minuteVal > 59 {
			results = append(results, fmt.Sprintf("- Invalid time %q for task %q (expected HH:MM)", req.Time, task.Title))
			continue
		}
		repeatType := models.RepeatDaily
		switch strings.ToLower(req.Repeat) {
		case "once":
			repeatType = models.RepeatOnce
		case "daily", "":
			repeatType = models.RepeatDaily
		case "weekly":
			repeatType = models.RepeatWeekly
		case "monthly":
			repeatType = models.RepeatMonthly
		case "hours", "hourly":
			repeatType = models.RepeatHours
		case "minutes":
			repeatType = models.RepeatMinutes
		case "seconds":
			repeatType = models.RepeatSeconds
		}
		repeatInterval := 1
		if req.Interval > 0 {
			repeatInterval = req.Interval
		}
		now := time.Now().Local()
		runAt := time.Date(now.Year(), now.Month(), now.Day(), hourVal, minuteVal, 0, 0, time.Local)
		runAtUTC := runAt.UTC()
		schedule := &models.Schedule{TaskID: task.ID, RunAt: runAtUTC, RepeatType: repeatType, RepeatInterval: repeatInterval, Enabled: true}
		if err := s.scheduleRepo.Create(ctx, schedule); err != nil {
			results = append(results, fmt.Sprintf("- Error scheduling task %q: %v", task.Title, err))
			continue
		}
		if task.Category != models.CategoryScheduled {
			_ = s.taskRepo.UpdateCategory(ctx, task.ID, models.CategoryScheduled)
			if task.Status != models.StatusPending {
				_ = s.taskRepo.UpdateStatus(ctx, task.ID, models.StatusPending)
			}
		}
		repeatDesc := FormatRepeatPattern(repeatType, repeatInterval)
		results = append(results, fmt.Sprintf("- Scheduled task %q [TASK_ID:%s] at %s (%s)", task.Title, task.ID, req.Time, repeatDesc))
	}
	if len(results) > 0 {
		output += "\n\n---\nSchedule Results:\n" + strings.Join(results, "\n")
	}
	return output
}

func (s *SlackService) processDeleteSchedule(ctx context.Context, execID, projectID, output string) string {
	requests := ParseDeleteSchedule(output)
	if len(requests) == 0 {
		return output
	}
	if s.scheduleRepo == nil {
		return output + "\n\n---\nSchedule Delete Results:\n- Error: schedule repository not available"
	}
	var results []string
	for _, req := range requests {
		schedule, task, err := s.resolveScheduleReference(ctx, projectID, req.ScheduleID, req.TaskID, req.Title)
		if err != nil {
			results = append(results, fmt.Sprintf("- Could not find schedule: %v", err))
			continue
		}
		if err := s.scheduleRepo.Delete(ctx, schedule.ID); err != nil {
			results = append(results, fmt.Sprintf("- Error deleting schedule for task %q: %v", task.Title, err))
			continue
		}
		remaining, _ := s.scheduleRepo.ListByTask(ctx, task.ID)
		if len(remaining) == 0 && task.Category == models.CategoryScheduled {
			_ = s.taskRepo.UpdateCategory(ctx, task.ID, models.CategoryBacklog)
		}
		results = append(results, fmt.Sprintf("- Deleted schedule for task %q [TASK_ID:%s]", task.Title, task.ID))
	}
	if len(results) > 0 {
		output += "\n\n---\nSchedule Delete Results:\n" + strings.Join(results, "\n")
	}
	return output
}

func (s *SlackService) processModifySchedule(ctx context.Context, execID, projectID, output string) string {
	requests := ParseModifySchedule(output)
	if len(requests) == 0 {
		return output
	}
	if s.scheduleRepo == nil {
		return output + "\n\n---\nSchedule Modify Results:\n- Error: schedule repository not available"
	}
	var results []string
	for _, req := range requests {
		schedule, task, err := s.resolveScheduleReference(ctx, projectID, req.ScheduleID, req.TaskID, req.Title)
		if err != nil {
			results = append(results, fmt.Sprintf("- Could not find schedule: %v", err))
			continue
		}
		var changes []string
		if req.Time != "" {
			var hourVal, minuteVal int
			if _, err := fmt.Sscanf(req.Time, "%d:%d", &hourVal, &minuteVal); err != nil || hourVal < 0 || hourVal > 23 || minuteVal < 0 || minuteVal > 59 {
				results = append(results, fmt.Sprintf("- Invalid time %q for task %q", req.Time, task.Title))
				continue
			}
			oldLocal := schedule.RunAt.Local()
			schedule.RunAt = time.Date(oldLocal.Year(), oldLocal.Month(), oldLocal.Day(), hourVal, minuteVal, 0, 0, time.Local).UTC()
			changes = append(changes, fmt.Sprintf("time→%s", req.Time))
		}
		if req.Repeat != "" {
			switch strings.ToLower(req.Repeat) {
			case "once":
				schedule.RepeatType = models.RepeatOnce
			case "daily":
				schedule.RepeatType = models.RepeatDaily
			case "weekly":
				schedule.RepeatType = models.RepeatWeekly
			case "monthly":
				schedule.RepeatType = models.RepeatMonthly
			case "hours", "hourly":
				schedule.RepeatType = models.RepeatHours
			case "minutes":
				schedule.RepeatType = models.RepeatMinutes
			case "seconds":
				schedule.RepeatType = models.RepeatSeconds
			default:
				results = append(results, fmt.Sprintf("- Unknown repeat type %q for task %q", req.Repeat, task.Title))
				continue
			}
			changes = append(changes, fmt.Sprintf("repeat→%s", req.Repeat))
		}
		if req.Interval != nil {
			if *req.Interval < 1 {
				results = append(results, fmt.Sprintf("- Invalid interval %d for task %q", *req.Interval, task.Title))
				continue
			}
			schedule.RepeatInterval = *req.Interval
			changes = append(changes, fmt.Sprintf("interval→%d", *req.Interval))
		}
		if req.Enabled != nil {
			schedule.Enabled = *req.Enabled
			if *req.Enabled {
				changes = append(changes, "enabled→true")
			} else {
				changes = append(changes, "enabled→false")
			}
		}
		if len(changes) == 0 {
			results = append(results, fmt.Sprintf("- No changes specified for schedule on task %q", task.Title))
			continue
		}
		// Match HTTP-toggle semantics: recompute only when time-related fields
		// changed; on re-enable recompute stale NextRun; on disable preserve it.
		batchTimeChanged := req.Time != "" || req.Repeat != "" || req.Interval != nil
		if batchTimeChanged {
			schedule.NextRun = schedule.ComputeNextRun(time.Now())
		} else if req.Enabled != nil && *req.Enabled {
			bNow := time.Now()
			if schedule.NextRun == nil || schedule.NextRun.Before(bNow) {
				if next := schedule.ComputeNextRun(bNow); next != nil {
					schedule.NextRun = next
				}
			}
		}
		if err := s.scheduleRepo.Update(ctx, schedule); err != nil {
			results = append(results, fmt.Sprintf("- Error updating schedule for task %q: %v", task.Title, err))
			continue
		}
		results = append(results, fmt.Sprintf("- Updated schedule for task %q [TASK_ID:%s]: %s", task.Title, task.ID, strings.Join(changes, ", ")))
	}
	if len(results) > 0 {
		output += "\n\n---\nSchedule Modify Results:\n" + strings.Join(results, "\n")
	}
	return output
}

func (s *SlackService) resolveScheduleReference(ctx context.Context, projectID, scheduleID, taskID, title string) (*models.Schedule, *models.Task, error) {
	if scheduleID != "" {
		schedule, err := s.scheduleRepo.GetByID(ctx, scheduleID)
		if err != nil {
			return nil, nil, fmt.Errorf("error looking up schedule %s: %w", scheduleID, err)
		}
		if schedule == nil {
			return nil, nil, fmt.Errorf("schedule %s not found", scheduleID)
		}
		task, err := s.taskRepo.GetByID(ctx, schedule.TaskID)
		if err != nil || task == nil {
			return nil, nil, fmt.Errorf("task for schedule %s not found", scheduleID)
		}
		if task.ProjectID != projectID {
			return nil, nil, fmt.Errorf("schedule %s belongs to a different project", scheduleID)
		}
		return schedule, task, nil
	}
	task, err := s.resolveTaskReference(ctx, projectID, taskID, title)
	if err != nil {
		return nil, nil, err
	}
	schedules, err := s.scheduleRepo.ListByTask(ctx, task.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("error listing schedules for task %s: %w", task.ID, err)
	}
	if len(schedules) == 0 {
		return nil, nil, fmt.Errorf("no schedules found for task %q", task.Title)
	}
	return &schedules[0], task, nil
}

func (s *SlackService) processListProjects(ctx context.Context, projectID, output string) string {
	if !HasListProjects(output) {
		return output
	}
	if s.projectRepo == nil {
		return output + "\n\n---\nAvailable Projects:\n- Error retrieving projects: project repository not configured"
	}

	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		output += "\n\n---\nAvailable Projects:\n- Error retrieving projects: " + err.Error()
		return output
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\nAvailable Projects:\n")
	if len(projects) == 0 {
		sb.WriteString("No projects found.\n")
	} else {
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
	}
	sb.WriteString("\nAsk me to switch projects by name when needed.\n")
	return output + sb.String()
}

func (s *SlackService) processSwitchProject(ctx context.Context, output, teamID, userID string) string {
	requests := ParseSwitchProject(output)
	if len(requests) == 0 {
		return output
	}
	if s.projectRepo == nil {
		return output + "\n\n---\nProject Switch Results:\n- Error loading projects: project repository not configured"
	}

	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return output + "\n\n---\nProject Switch Results:\n- Error loading projects: " + err.Error()
	}

	var results []string
	for _, req := range requests {
		var target *models.Project
		for i := range projects {
			if strings.EqualFold(projects[i].Name, req.Project) || projects[i].ID == req.Project {
				target = &projects[i]
				break
			}
		}
		if target == nil {
			var names []string
			for _, p := range projects {
				names = append(names, p.Name)
			}
			results = append(results, fmt.Sprintf("- Project not found: %q. Available projects: %s", req.Project, strings.Join(names, ", ")))
			continue
		}
		s.setActiveProject(ctx, teamID, userID, target.ID)
		results = append(results, fmt.Sprintf("- Switched to project: %s", target.Name))
	}

	if len(results) == 0 {
		return output
	}
	return output + "\n\n---\nProject Switch Results:\n" + strings.Join(results, "\n")
}

func (s *SlackService) setActiveProject(ctx context.Context, teamID, userID, projectID string) {
	key := slackUserProjectKey(teamID, userID)
	s.mu.Lock()
	s.userProjects[key] = projectID
	s.mu.Unlock()
	if s.slackUserProjectRepo != nil {
		if err := s.slackUserProjectRepo.SetUserProject(ctx, teamID, userID, projectID); err != nil {
			applog.Infof("[slack] persist active project failed: %v", err)
		}
	}
}

func (s *SlackService) getActiveProject(ctx context.Context, teamID, userID string) string {
	key := slackUserProjectKey(teamID, userID)

	s.mu.RLock()
	if projectID, ok := s.userProjects[key]; ok {
		s.mu.RUnlock()
		return projectID
	}
	s.mu.RUnlock()

	if s.slackUserProjectRepo != nil {
		if saved, err := s.slackUserProjectRepo.GetUserProject(ctx, teamID, userID); err == nil && saved != "" {
			s.mu.Lock()
			s.userProjects[key] = saved
			s.mu.Unlock()
			return saved
		}
	}

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

func slackUserProjectKey(teamID, userID string) string {
	return strings.TrimSpace(teamID) + ":" + strings.TrimSpace(userID)
}

func (s *SlackService) checkAuthorization(ctx context.Context, projectID, slackUserID string) bool {
	if s.slackAuthRepo == nil {
		return true
	}

	if strings.TrimSpace(projectID) == "" {
		authorized, err := s.slackAuthRepo.IsAuthorizedAnywhere(ctx, slackUserID)
		if err != nil {
			applog.Infof("[slack] auth check error for user=%s anywhere: %v", slackUserID, err)
			return true
		}
		return authorized
	}

	authorized, err := s.slackAuthRepo.IsAuthorized(ctx, projectID, slackUserID)
	if err != nil {
		applog.Infof("[slack] auth check error for user=%s project=%s: %v", slackUserID, projectID, err)
		return true
	}
	if authorized {
		return true
	}

	authorizedAnywhere, err := s.slackAuthRepo.IsAuthorizedAnywhere(ctx, slackUserID)
	if err != nil {
		applog.Infof("[slack] auth check error for user=%s fallback-anywhere: %v", slackUserID, err)
		return true
	}
	return authorizedAnywhere
}

func sanitizeSlackText(text string) string {
	cleaned := strings.TrimSpace(slackMentionRegex.ReplaceAllString(text, ""))
	return strings.TrimSpace(cleaned)
}

func slackMessageTextOrAttachmentPrompt(text string, hasFiles bool) string {
	text = strings.TrimSpace(text)
	if text == "" && hasFiles {
		return "Please analyze the attachment."
	}
	return text
}

func slackMessageMentionsBot(event slackevents.MessageEvent, botUserID string) bool {
	botUserID = strings.TrimSpace(botUserID)
	if botUserID == "" {
		return false
	}
	needle := "<@" + botUserID + ">"
	if strings.Contains(event.Text, needle) {
		return true
	}
	if event.Message != nil && strings.Contains(event.Message.Text, needle) {
		return true
	}
	return false
}

func (s *SlackService) downloadSlackAttachments(ctx context.Context, files []slackIncomingFile) (string, []models.Attachment, []models.ChatAttachment, error) {
	chatAttachments, err := s.downloadSlackFiles(ctx, files)
	if err != nil {
		return "", nil, nil, err
	}
	attachmentContext, imageAttachments := slackAttachmentContextAndImages(chatAttachments)
	return attachmentContext, imageAttachments, chatAttachments, nil
}

func slackAttachmentContextAndImages(chatAttachments []models.ChatAttachment) (string, []models.Attachment) {
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
		if att.FileSize <= slackMaxTextFileSize {
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

func (s *SlackService) downloadSlackFiles(ctx context.Context, files []slackIncomingFile) ([]models.ChatAttachment, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > slackMaxFilesPerMessage {
		return nil, fmt.Errorf("too many files (%d, max %d)", len(files), slackMaxFilesPerMessage)
	}
	botToken := strings.TrimSpace(s.resolveBotToken(ctx))
	if botToken == "" {
		return nil, fmt.Errorf("slack bot token is not configured")
	}
	tmpDir, err := os.MkdirTemp("", "slack-attachment-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	attachments := make([]models.ChatAttachment, 0, len(files))
	for _, f := range files {
		var err error
		f, err = s.resolveSlackFileInfo(ctx, f)
		if err != nil {
			return nil, err
		}
		if f.Size > slackMaxFileSize {
			return nil, fmt.Errorf("file %q too large (%d bytes, max %d)", slackFileDisplayName(f), f.Size, slackMaxFileSize)
		}
		fileName := slackSafeFileName(f)
		mediaType := slackIncomingFileMediaType(f, fileName)
		destPath, mediaType, err := s.downloadSlackFileCandidate(ctx, botToken, tmpDir, fileName, mediaType, f)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(destPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat file %q: %w", fileName, err)
		}
		absPath, err := filepath.Abs(destPath)
		if err != nil {
			absPath = destPath
		}
		attachments = append(attachments, models.ChatAttachment{
			FileName:  fileName,
			FilePath:  absPath,
			MediaType: mediaType,
			FileSize:  info.Size(),
		})
		applog.Infof("[slack] downloaded attachment file=%s size=%d mime=%s path=%s", fileName, info.Size(), mediaType, absPath)
	}
	cleanup = false
	return attachments, nil
}

func (s *SlackService) resolveSlackFileInfo(ctx context.Context, f slackIncomingFile) (slackIncomingFile, error) {
	mediaType := slackIncomingFileMediaType(f, slackSafeFileName(f))
	forceInfo := strings.EqualFold(strings.TrimSpace(f.FileAccess), "check_file_info") || !slackIncomingFileHasDownloadURL(f)
	optionalInfo := isSlackImageFile(mediaType) && strings.TrimSpace(f.URLPrivateDownload) == "" && strings.TrimSpace(f.URLPrivate) != ""
	needsInfo := strings.TrimSpace(f.ID) != "" && (forceInfo || optionalInfo)
	if !needsInfo {
		return f, nil
	}
	client := s.slackClientForFiles()
	if client == nil {
		if optionalInfo && !forceInfo {
			return f, nil
		}
		return f, fmt.Errorf("slack bot token is not configured")
	}
	info, _, _, err := client.GetFileInfoContext(ctx, strings.TrimSpace(f.ID), 0, 0)
	if err != nil {
		if optionalInfo && !forceInfo {
			applog.Infof("[slack] optional file info refresh failed for %s; using event file URL: %v", strings.TrimSpace(f.ID), err)
			return f, nil
		}
		return f, fmt.Errorf("failed to fetch Slack file info for %s: %w", strings.TrimSpace(f.ID), err)
	}
	if info == nil || strings.TrimSpace(info.ID) == "" {
		if optionalInfo && !forceInfo {
			applog.Infof("[slack] optional file info refresh returned empty file for %s; using event file URL", strings.TrimSpace(f.ID))
			return f, nil
		}
		return f, fmt.Errorf("Slack file info for %s was empty", strings.TrimSpace(f.ID))
	}
	resolved := slackIncomingFileFromSlackFile(*info)
	return mergeSlackIncomingFile(f, resolved), nil
}

func (s *SlackService) resolveSlackIncomingFilesForRouting(ctx context.Context, files []slackIncomingFile) []slackIncomingFile {
	if len(files) == 0 {
		return files
	}
	resolved := make([]slackIncomingFile, len(files))
	copy(resolved, files)
	for i, f := range resolved {
		mediaType := slackIncomingFileMediaType(f, slackSafeFileName(f))
		if isSlackImageFile(mediaType) {
			continue
		}
		if strings.TrimSpace(f.ID) == "" {
			continue
		}
		needsFileInfo := strings.EqualFold(strings.TrimSpace(f.FileAccess), "check_file_info")
		needsFileInfo = needsFileInfo || mediaType == "" || mediaType == "application/octet-stream"
		if !needsFileInfo && slackIncomingFileHasDownloadURL(f) {
			continue
		}
		if needsFileInfo {
			f.FileAccess = "check_file_info"
		}
		fileInfo, err := s.resolveSlackFileInfo(ctx, f)
		if err != nil {
			applog.Infof("[slack] file info refresh for routing failed for %s: %v", strings.TrimSpace(f.ID), err)
			continue
		}
		fileInfo.FileAccess = ""
		resolved[i] = fileInfo
	}
	return resolved
}

func slackIncomingFileHasDownloadURL(f slackIncomingFile) bool {
	return strings.TrimSpace(f.URLPrivateDownload) != "" || strings.TrimSpace(f.URLPrivate) != "" || strings.TrimSpace(f.Thumb1024) != "" || strings.TrimSpace(f.Thumb960) != "" || strings.TrimSpace(f.Thumb720) != "" || strings.TrimSpace(f.Thumb480) != "" || strings.TrimSpace(f.Thumb360) != ""
}

func mergeSlackIncomingFile(original, resolved slackIncomingFile) slackIncomingFile {
	if strings.TrimSpace(resolved.ID) == "" {
		resolved.ID = original.ID
	}
	if strings.TrimSpace(resolved.Name) == "" {
		resolved.Name = original.Name
	}
	if strings.TrimSpace(resolved.Title) == "" {
		resolved.Title = original.Title
	}
	if strings.TrimSpace(resolved.Mimetype) == "" || strings.TrimSpace(resolved.Mimetype) == "application/octet-stream" {
		if mt := strings.TrimSpace(original.Mimetype); mt != "" && mt != "application/octet-stream" {
			resolved.Mimetype = mt
		}
	}
	if resolved.Size == 0 {
		resolved.Size = original.Size
	}
	if strings.TrimSpace(resolved.URLPrivate) == "" {
		resolved.URLPrivate = original.URLPrivate
	}
	if strings.TrimSpace(resolved.URLPrivateDownload) == "" {
		resolved.URLPrivateDownload = original.URLPrivateDownload
	}
	if strings.TrimSpace(resolved.FileAccess) == "" {
		resolved.FileAccess = original.FileAccess
	}
	if strings.TrimSpace(resolved.Thumb360) == "" {
		resolved.Thumb360 = original.Thumb360
	}
	if strings.TrimSpace(resolved.Thumb480) == "" {
		resolved.Thumb480 = original.Thumb480
	}
	if strings.TrimSpace(resolved.Thumb720) == "" {
		resolved.Thumb720 = original.Thumb720
	}
	if strings.TrimSpace(resolved.Thumb960) == "" {
		resolved.Thumb960 = original.Thumb960
	}
	if strings.TrimSpace(resolved.Thumb1024) == "" {
		resolved.Thumb1024 = original.Thumb1024
	}
	return resolved
}

func (s *SlackService) downloadSlackFileCandidate(ctx context.Context, botToken, tmpDir, fileName, mediaType string, f slackIncomingFile) (string, string, error) {
	candidates := slackFileDownloadURLs(f, mediaType)
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("file %q has no private download URL", slackFileDisplayName(f))
	}
	var lastErr error
	for i, candidateURL := range candidates {
		destPath := filepath.Join(tmpDir, uniqueSlackTempFilename(tmpDir, fileName))
		dest, err := os.Create(destPath)
		if err != nil {
			return "", "", fmt.Errorf("failed to create file: %w", err)
		}
		err = s.downloadSlackPrivateFile(ctx, botToken, candidateURL, dest)
		closeErr := dest.Close()
		if err != nil {
			_ = os.Remove(destPath)
			lastErr = fmt.Errorf("failed to download file %q: %w", fileName, err)
			continue
		}
		if closeErr != nil {
			_ = os.Remove(destPath)
			return "", "", fmt.Errorf("failed to save file %q: %w", fileName, closeErr)
		}
		normalizedMediaType, err := validateSlackDownloadedFile(destPath, fileName, mediaType)
		if err == nil {
			if i > 0 {
				applog.Infof("[slack] attachment file=%s downloaded from fallback URL after earlier candidate failed", fileName)
			}
			if normalizedMediaType != "" {
				mediaType = normalizedMediaType
			}
			return destPath, mediaType, nil
		}
		_ = os.Remove(destPath)
		lastErr = err
		if !isSlackImageFile(mediaType) {
			break
		}
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("failed to download file %q", fileName)
}

func slackFileDownloadURLs(f slackIncomingFile, mediaType string) []string {
	seen := make(map[string]bool)
	var urls []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		urls = append(urls, raw)
	}
	add(f.URLPrivateDownload)
	add(f.URLPrivate)
	if isSlackImageFile(mediaType) {
		add(f.Thumb1024)
		add(f.Thumb960)
		add(f.Thumb720)
		add(f.Thumb480)
		add(f.Thumb360)
	}
	return urls
}

func (s *SlackService) downloadSlackPrivateFile(ctx context.Context, botToken, downloadURL string, writer io.Writer) error {
	if strings.TrimSpace(downloadURL) == "" {
		return fmt.Errorf("received empty download URL")
	}
	if strings.TrimSpace(botToken) == "" {
		return fmt.Errorf("slack bot token is not configured")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	downloadClient := *client
	downloadClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	parsedDownloadURL, err := url.Parse(downloadURL)
	if err != nil {
		return err
	}
	if !slackTrustedOriginalFileDownloadHost(parsedDownloadURL.Hostname()) {
		return fmt.Errorf("slack file download URL host %q is not trusted", parsedDownloadURL.Host)
	}

	currentURL := downloadURL
	for redirects := 0; redirects <= 10; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+botToken)
		req.Header.Set("User-Agent", "OpenVibely Slack file downloader")

		resp, err := downloadClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			nextURL, err := slackRedirectLocation(currentURL, resp.Header.Get("Location"))
			_ = resp.Body.Close()
			if err != nil {
				return err
			}
			if !slackCanForwardFileAuth(downloadURL, nextURL) {
				return fmt.Errorf("slack file download redirected to untrusted host %q", nextURL.Host)
			}
			currentURL = nextURL.String()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("slack file download returned HTTP %d", resp.StatusCode)
		}
		written, err := io.Copy(writer, io.LimitReader(resp.Body, slackMaxFileSize))
		if err != nil {
			return err
		}
		if written == slackMaxFileSize {
			var extra [1]byte
			n, err := resp.Body.Read(extra[:])
			if err != nil && err != io.EOF {
				return err
			}
			if n > 0 {
				return fmt.Errorf("slack file download exceeded maximum size %d bytes", slackMaxFileSize)
			}
		}
		return nil
	}
	return fmt.Errorf("slack file download exceeded redirect limit")
}

func slackRedirectLocation(currentURL, location string) (*url.URL, error) {
	if strings.TrimSpace(location) == "" {
		return nil, fmt.Errorf("slack file download redirect missing Location header")
	}
	base, err := url.Parse(currentURL)
	if err != nil {
		return nil, err
	}
	next, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(next), nil
}

func slackCanForwardFileAuth(originalURL string, nextURL *url.URL) bool {
	if nextURL == nil {
		return false
	}
	original, err := url.Parse(originalURL)
	if err != nil {
		return false
	}
	return slackTrustedFileDownloadHost(original.Hostname(), nextURL.Hostname())
}

func slackIsTrustedFileURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return slackTrustedOriginalFileDownloadHost(parsed.Hostname())
}

func slackTrustedOriginalFileDownloadHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if host == "slack.com" || strings.HasSuffix(host, ".slack.com") {
		return true
	}
	if host == "slack-files.com" || strings.HasSuffix(host, ".slack-files.com") {
		return true
	}
	return false
}

func slackTrustedFileDownloadHost(originalHost, nextHost string) bool {
	originalHost = strings.ToLower(strings.TrimSpace(originalHost))
	nextHost = strings.ToLower(strings.TrimSpace(nextHost))
	if nextHost == "" {
		return false
	}
	if nextHost == originalHost {
		return true
	}
	if nextHost == "slack.com" || strings.HasSuffix(nextHost, ".slack.com") {
		return true
	}
	if nextHost == "slack-files.com" || strings.HasSuffix(nextHost, ".slack-files.com") {
		return true
	}
	return false
}

func (s *SlackService) slackClientForFiles() *slack.Client {
	s.mu.RLock()
	client := s.botClient
	s.mu.RUnlock()
	if client != nil {
		return client
	}
	botToken := strings.TrimSpace(s.resolveBotToken(context.Background()))
	if botToken == "" {
		return nil
	}
	return slack.New(botToken, slack.OptionHTTPClient(s.httpClient))
}

func (s *SlackService) saveChatAttachmentsToPendingSession(attachments []models.ChatAttachment) (string, error) {
	if len(attachments) == 0 {
		return "", nil
	}
	sessionID := generateSlackPendingSessionID()
	sessionDir := filepath.Join(s.uploadsDir, "chat", "pending", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return "", fmt.Errorf("creating pending upload directory: %w", err)
	}
	cleanupDirs := make(map[string]struct{})
	for _, att := range attachments {
		fileName := filepath.Base(att.FileName)
		if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
			fileName = "slack-attachment"
		}
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
		destPath := filepath.Join(sessionDir, fmt.Sprintf("%s_%s", generateSlackPendingSessionID()[:8], fileName))
		if err := moveOrCopyFile(att.FilePath, destPath); err != nil {
			_ = os.RemoveAll(sessionDir)
			for dir := range cleanupDirs {
				_ = os.RemoveAll(dir)
			}
			return "", fmt.Errorf("staging %s: %w", fileName, err)
		}
	}
	for dir := range cleanupDirs {
		_ = os.RemoveAll(dir)
	}
	return sessionID, nil
}

func (s *SlackService) linkAttachmentsToExecution(ctx context.Context, execID string, attachments []models.ChatAttachment) ([]models.ChatAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	if s.chatAttachmentRepo == nil {
		cleanupSlackAttachmentSourceDirs(attachments)
		return nil, fmt.Errorf("chat attachment repository is unavailable")
	}
	execDir := filepath.Join(s.uploadsDir, "chat", execID)
	if err := os.MkdirAll(execDir, 0755); err != nil {
		applog.Infof("[slack] error creating exec dir %s: %v", execDir, err)
		cleanupSlackAttachmentSourceDirs(attachments)
		return nil, fmt.Errorf("storing Slack attachment: %w", err)
	}
	cleanupDirs := make(map[string]struct{})
	linked := make([]models.ChatAttachment, 0, len(attachments))
	var linkErrs []string
	for i := range attachments {
		att := &attachments[i]
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
		destPath := filepath.Join(execDir, uniqueSlackTempFilename(execDir, filepath.Base(att.FileName)))
		if err := moveOrCopyFile(att.FilePath, destPath); err != nil {
			applog.Infof("[slack] error moving attachment file=%s: %v", att.FileName, err)
			linkErrs = append(linkErrs, fmt.Sprintf("%s: %v", att.FileName, err))
			continue
		}
		absPath, err := filepath.Abs(destPath)
		if err != nil {
			absPath = destPath
		}
		att.FilePath = absPath
		att.ExecutionID = execID
		if err := s.chatAttachmentRepo.Create(ctx, att); err != nil {
			applog.Infof("[slack] error creating chat attachment record: %v", err)
			_ = os.Remove(destPath)
			linkErrs = append(linkErrs, fmt.Sprintf("%s: %v", att.FileName, err))
		} else {
			linked = append(linked, *att)
			applog.Infof("[slack] linked attachment id=%s file=%s to exec=%s", att.ID, att.FileName, execID)
		}
	}
	for dir := range cleanupDirs {
		_ = os.RemoveAll(dir)
	}
	if len(linkErrs) > 0 {
		s.cleanupLinkedSlackAttachments(ctx, linked)
		return nil, fmt.Errorf("storing Slack attachment failed for %d of %d file(s): %s", len(linkErrs), len(attachments), strings.Join(linkErrs, "; "))
	}
	return linked, nil
}

func (s *SlackService) cleanupLinkedSlackAttachments(ctx context.Context, attachments []models.ChatAttachment) {
	for _, att := range attachments {
		if strings.TrimSpace(att.ID) != "" && s.chatAttachmentRepo != nil {
			if err := s.chatAttachmentRepo.Delete(ctx, att.ID); err != nil {
				applog.Infof("[slack] error deleting partial chat attachment record id=%s: %v", att.ID, err)
			}
		}
		if strings.TrimSpace(att.FilePath) != "" {
			if err := os.Remove(att.FilePath); err != nil && !os.IsNotExist(err) {
				applog.Infof("[slack] error deleting partial chat attachment file=%s: %v", att.FilePath, err)
			}
		}
	}
}

func cleanupSlackAttachmentSourceDirs(attachments []models.ChatAttachment) {
	cleanupDirs := make(map[string]struct{})
	for _, att := range attachments {
		if strings.TrimSpace(att.FilePath) == "" {
			continue
		}
		cleanupDirs[filepath.Dir(att.FilePath)] = struct{}{}
	}
	for dir := range cleanupDirs {
		_ = os.RemoveAll(dir)
	}
}

func generateSlackPendingSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func slackSafeFileName(f slackIncomingFile) string {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = strings.TrimSpace(f.Title)
	}
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) || name == "" {
		if strings.TrimSpace(f.ID) != "" {
			name = "slack-" + strings.TrimSpace(f.ID)
		} else {
			name = "slack-attachment"
		}
	}
	return name
}

func slackFileDisplayName(f slackIncomingFile) string {
	return slackSafeFileName(f)
}

func uniqueSlackTempFilename(dir, filename string) string {
	candidate := filename
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	if base == "" {
		base = "slack-attachment"
	}
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
}

func slackIncomingFileMediaType(f slackIncomingFile, fileName string) string {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(f.Mimetype, ";")[0]))
	filenameMediaType := mediaTypeFromSlackFilename(fileName)
	if mediaType == "" || mediaType == "application/octet-stream" {
		return filenameMediaType
	}
	return mediaType
}

func slackIncomingFilesContainImage(files []slackIncomingFile) bool {
	for _, f := range files {
		if isSlackImageFile(slackIncomingFileMediaType(f, slackSafeFileName(f))) {
			return true
		}
	}
	return false
}

func slackIncomingFilesRequireVision(files []slackIncomingFile) bool {
	for _, f := range files {
		mediaType := slackIncomingFileMediaType(f, slackSafeFileName(f))
		if isSlackImageFile(mediaType) {
			return true
		}
		if (mediaType == "" || mediaType == "application/octet-stream") && slackIncomingFileHasDownloadURL(f) {
			return true
		}
	}
	return false
}

func slackChatAttachmentsContainImage(chatAttachments []models.ChatAttachment) bool {
	for _, att := range chatAttachments {
		if isSlackImageFile(att.MediaType) {
			return true
		}
	}
	return false
}

func mediaTypeFromSlackFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".md", ".csv", ".json", ".log", ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".html", ".css", ".scss", ".sql", ".sh", ".yaml", ".yml", ".xml":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func validateSlackDownloadedFile(path, fileName, declaredMediaType string) (string, error) {
	declaredMediaType = strings.ToLower(strings.TrimSpace(strings.Split(declaredMediaType, ";")[0]))
	shouldValidateImage := isSlackImageFile(declaredMediaType)
	if !shouldValidateImage && declaredMediaType != "" && declaredMediaType != "application/octet-stream" {
		return declaredMediaType, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to validate image %q: %w", fileName, err)
	}
	defer file.Close()

	head := make([]byte, 512)
	n, readErr := io.ReadFull(file, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("failed to validate image %q: %w", fileName, readErr)
	}
	sniffedMediaType := strings.ToLower(strings.TrimSpace(http.DetectContentType(head[:n])))
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed to validate image %q: %w", fileName, err)
	}
	if slackLooksLikeWebP(head[:n]) {
		return "image/webp", nil
	}
	format, err := slackDecodeImageConfig(file)
	if err != nil {
		if !shouldValidateImage {
			return declaredMediaType, nil
		}
		return "", fmt.Errorf("downloaded Slack file %q was labeled %s but is not a valid supported image (detected %s)", fileName, declaredMediaType, sniffedMediaType)
	}
	detectedMediaType := slackImageFormatMediaType(format)
	if detectedMediaType == "" {
		return "", fmt.Errorf("downloaded Slack file %q uses unsupported image format %q", fileName, format)
	}
	if declaredMediaType != "" && declaredMediaType != "application/octet-stream" && declaredMediaType != detectedMediaType {
		applog.Infof("[slack] attachment file=%s declared mime=%s but detected mime=%s; using detected mime", fileName, declaredMediaType, detectedMediaType)
	}
	return detectedMediaType, nil
}

func slackDecodeImageConfig(r io.Reader) (string, error) {
	_, format, err := image.DecodeConfig(r)
	return format, err
}

func slackLooksLikeWebP(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

func slackImageFormatMediaType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func isSlackImageFile(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func filterSlackChatHistory(executions []models.Execution, currentExecID string) []models.Execution {
	if len(executions) == 0 {
		return []models.Execution{}
	}
	result := make([]models.Execution, 0, len(executions))
	for i := range executions {
		if executions[i].ID == currentExecID || executions[i].Status == models.ExecRunning {
			continue
		}
		result = append(result, executions[i])
	}
	return result
}

func (s *SlackService) sendSlackMessage(channelID, threadTS, text string) error {
	_, err := s.postSlackMessage(channelID, threadTS, text)
	return err
}

func (s *SlackService) openSlackDirectMessage(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("slack user id is required")
	}
	if s.openConversationFn != nil {
		return s.openConversationFn(userID)
	}
	s.mu.RLock()
	client := s.botClient
	s.mu.RUnlock()
	if client == nil {
		botToken := strings.TrimSpace(s.resolveBotToken(context.Background()))
		if botToken == "" {
			return "", fmt.Errorf("slack bot token is not configured")
		}
		client = slack.New(botToken)
	}
	channel, _, _, err := client.OpenConversation(&slack.OpenConversationParameters{ReturnIM: true, Users: []string{userID}})
	if err != nil {
		return "", fmt.Errorf("open slack direct message: %w", err)
	}
	if channel == nil || strings.TrimSpace(channel.ID) == "" {
		return "", fmt.Errorf("open slack direct message: missing channel id")
	}
	return strings.TrimSpace(channel.ID), nil
}

func (s *SlackService) postSlackMessage(channelID, threadTS, text string) (string, error) {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(text) == "" {
		return "", nil
	}
	if s.postMessageFn != nil {
		return s.postMessageFn(channelID, threadTS, text)
	}

	s.mu.RLock()
	client := s.botClient
	s.mu.RUnlock()
	if client == nil {
		botToken := strings.TrimSpace(s.resolveBotToken(context.Background()))
		if botToken == "" {
			return "", fmt.Errorf("slack bot token is not configured")
		}
		client = slack.New(botToken)
	}

	params := slack.PostMessageParameters{}
	if strings.TrimSpace(threadTS) != "" {
		params.ThreadTimestamp = threadTS
	}
	_, ts, err := client.PostMessage(channelID,
		slack.MsgOptionPostMessageParameters(params),
		slack.MsgOptionText(text, false),
	)
	if err != nil {
		return "", fmt.Errorf("post slack message: %w", err)
	}
	return ts, nil
}

func (s *SlackService) SendOutboundMessage(ctx context.Context, channelID, threadTS, text string) SendMessageResult {
	_ = ctx
	if strings.TrimSpace(channelID) == "" {
		return SendMessageResult{OK: false, Platform: "slack", Error: "slack channel id is required"}
	}
	if strings.TrimSpace(text) == "" {
		return SendMessageResult{OK: false, Platform: "slack", Target: formatResolvedMessageTarget("slack", channelID, threadTS), Error: "message is required"}
	}
	messageID, err := s.postSlackMessage(channelID, threadTS, text)
	if err != nil {
		return SendMessageResult{OK: false, Platform: "slack", Target: formatResolvedMessageTarget("slack", channelID, threadTS), Error: err.Error()}
	}
	return SendMessageResult{OK: true, Platform: "slack", Target: formatResolvedMessageTarget("slack", channelID, threadTS), MessageID: messageID}
}

func (s *SlackService) SendOutboundDirectMessage(ctx context.Context, userID, text string) SendMessageResult {
	_ = ctx
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return SendMessageResult{OK: false, Platform: "slack", Error: "slack user id is required"}
	}
	if strings.TrimSpace(text) == "" {
		return SendMessageResult{OK: false, Platform: "slack", Target: formatResolvedMessageTarget("slack", userID, ""), Error: "message is required"}
	}
	channelID, err := s.openSlackDirectMessage(userID)
	if err != nil {
		return SendMessageResult{OK: false, Platform: "slack", Target: formatResolvedMessageTarget("slack", userID, ""), Error: err.Error()}
	}
	messageID, err := s.postSlackMessage(channelID, "", text)
	if err != nil {
		return SendMessageResult{OK: false, Platform: "slack", Target: formatResolvedMessageTarget("slack", userID, ""), Error: err.Error()}
	}
	return SendMessageResult{OK: true, Platform: "slack", Target: formatResolvedMessageTarget("slack", userID, ""), MessageID: messageID}
}

func generateOAuthState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *SlackService) getSetting(ctx context.Context, key string) string {
	if s.settingsRepo == nil {
		return ""
	}
	val, _ := s.settingsRepo.Get(ctx, key)
	return val
}

func (s *SlackService) setSetting(ctx context.Context, key, value string) error {
	if s.settingsRepo == nil {
		return nil
	}
	return s.settingsRepo.Set(ctx, key, value)
}

func (s *SlackService) getBotTokenSource(ctx context.Context) string {
	source := strings.TrimSpace(strings.ToLower(s.getSetting(ctx, SlackSettingBotTokenSource)))
	switch source {
	case SlackBotTokenSourceManual:
		return SlackBotTokenSourceManual
	default:
		return SlackBotTokenSourceOAuth
	}
}

func (s *SlackService) resolveBotToken(ctx context.Context) string {
	source := s.getBotTokenSource(ctx)
	if source == SlackBotTokenSourceManual {
		overrideToken := strings.TrimSpace(s.getSetting(ctx, SlackSettingBotTokenOverride))
		if overrideToken != "" {
			return overrideToken
		}
	}
	return strings.TrimSpace(s.getSetting(ctx, SlackSettingBotToken))
}

// SendChatResponse sends a completed chat-orchestrator response back to the
// originating Slack thread. Unlike task completion notifications, this is only
// for chat-category tasks that were promoted from queued channel input.
func (s *SlackService) SendChatResponse(ctx context.Context, task models.Task, output string, errMsg string) {
	if task.CreatedVia != models.TaskOriginSlack || task.Category != models.CategoryChat || s.slackTaskContextRepo == nil {
		return
	}
	if !s.IsSendResponsesEnabled(ctx) {
		return
	}
	ctxRecord, err := s.slackTaskContextRepo.GetByTaskID(ctx, task.ID)
	if err != nil || ctxRecord == nil {
		return
	}
	var message string
	if errMsg != "" {
		message = fmt.Sprintf("❌ Error: %s", util.Truncate(errMsg, 220))
	} else {
		cleaned := llmoutput.CleanChatOutputForDisplay(output)
		if cleaned == "" {
			cleaned = "(No response)"
		}
		message = cleaned
	}
	if err := s.sendSlackMessage(ctxRecord.SlackChannelID, ctxRecord.SlackThreadTS, message); err != nil {
		applog.Infof("[slack] send chat response failed for task=%s: %v", task.ID, err)
	}
}

// executeChannelSetTaskGoal sets or replaces the goal for a task.
func (s *SlackService) executeChannelSetTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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

// executeChannelClearTaskGoal clears the stored goal for a task.
func (s *SlackService) executeChannelClearTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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

// executeChannelGetTaskGoal reads the current goal and status for a task.
func (s *SlackService) executeChannelGetTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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

// executeChannelPauseTaskGoal pauses automatic continuation for a task goal.
func (s *SlackService) executeChannelPauseTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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

// executeChannelResumeTaskGoal resumes automatic continuation for a paused task goal.
func (s *SlackService) executeChannelResumeTaskGoal(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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

// executeChannelMarkTaskGoalAchieved marks the current task goal as achieved.
// In channel contexts the orchestrating user is the principal, so no agent
// tool grant is required (consistent with web chat where the user drives actions).
func (s *SlackService) executeChannelMarkTaskGoalAchieved(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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

// executeChannelReportTaskGoalBlocked records a repeatable blocker for a task goal.
// In channel contexts the orchestrating user is the principal, so no agent
// tool grant is required (consistent with web chat where the user drives actions).
func (s *SlackService) executeChannelReportTaskGoalBlocked(ctx context.Context, projectID string, input json.RawMessage) (string, error) {
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
