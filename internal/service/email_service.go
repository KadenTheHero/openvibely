package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	netmail "net/mail"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	messagemail "github.com/emersion/go-message/mail"
	"github.com/openvibely/openvibely/internal/applog"
	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/events"
	llmoutput "github.com/openvibely/openvibely/internal/llm/output"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/util"
)

const (
	EmailSettingProvider                = "email_provider"
	EmailSettingAddress                 = "email_address"
	EmailSettingPassword                = "email_password"
	EmailSettingIMAPHost                = "email_imap_host"
	EmailSettingIMAPPort                = "email_imap_port"
	EmailSettingSMTPHost                = "email_smtp_host"
	EmailSettingSMTPPort                = "email_smtp_port"
	EmailSettingPollIntervalSeconds     = "email_poll_interval_seconds"
	EmailSettingSendResponses           = "email_send_responses"
	EmailSettingSkipAttachments         = "email_skip_attachments"
	EmailSettingMarkExistingSeenOnStart = "email_mark_existing_seen_on_start"

	EmailProviderCustom   = "custom"
	EmailProviderGmail    = "gmail"
	EmailProviderOutlook  = "outlook"
	EmailProviderYahoo    = "yahoo"
	EmailProviderFastmail = "fastmail"
	EmailProviderICloud   = "icloud"

	emailProcessTimeout   = 5 * time.Minute
	emailChatHistoryLimit = 50
)

type EmailProviderPreset struct {
	Key      string
	Label    string
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
	HelpText string
}

var emailProviderPresets = map[string]EmailProviderPreset{
	EmailProviderGmail: {
		Key: EmailProviderGmail, Label: "Gmail", IMAPHost: "imap.gmail.com", IMAPPort: 993, SMTPHost: "smtp.gmail.com", SMTPPort: 587,
		HelpText: "Use a Google app password; IMAP must be enabled in Gmail settings.",
	},
	EmailProviderOutlook: {
		Key: EmailProviderOutlook, Label: "Outlook / Microsoft 365", IMAPHost: "outlook.office365.com", IMAPPort: 993, SMTPHost: "smtp.office365.com", SMTPPort: 587,
		HelpText: "Use an app password or tenant-approved SMTP auth.",
	},
	EmailProviderYahoo: {
		Key: EmailProviderYahoo, Label: "Yahoo Mail", IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 587,
		HelpText: "Use an app password.",
	},
	EmailProviderFastmail: {
		Key: EmailProviderFastmail, Label: "Fastmail", IMAPHost: "imap.fastmail.com", IMAPPort: 993, SMTPHost: "smtp.fastmail.com", SMTPPort: 587,
		HelpText: "Use an app password/API password.",
	},
	EmailProviderICloud: {
		Key: EmailProviderICloud, Label: "iCloud Mail", IMAPHost: "imap.mail.me.com", IMAPPort: 993, SMTPHost: "smtp.mail.me.com", SMTPPort: 587,
		HelpText: "Use an app-specific password.",
	},
	EmailProviderCustom: {
		Key: EmailProviderCustom, Label: "Custom IMAP/SMTP", IMAPPort: 993, SMTPPort: 587,
		HelpText: "Enter your IMAP and SMTP host details.",
	},
}

func EmailProviderPresets() []EmailProviderPreset {
	return []EmailProviderPreset{
		emailProviderPresets[EmailProviderGmail],
		emailProviderPresets[EmailProviderOutlook],
		emailProviderPresets[EmailProviderYahoo],
		emailProviderPresets[EmailProviderFastmail],
		emailProviderPresets[EmailProviderICloud],
		emailProviderPresets[EmailProviderCustom],
	}
}

func NormalizeEmailProvider(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	if _, ok := emailProviderPresets[key]; ok {
		return key
	}
	return EmailProviderCustom
}

func NormalizeEmailPasswordForProvider(provider, password string) string {
	password = strings.TrimSpace(password)
	if NormalizeEmailProvider(provider) == EmailProviderCustom {
		return password
	}
	return regexp.MustCompile(`\s+`).ReplaceAllString(password, "")
}

func ResolveEmailProviderSettings(provider, imapHost, imapPort, smtpHost, smtpPort string) (string, string, int, string, int, error) {
	key := NormalizeEmailProvider(provider)
	preset := emailProviderPresets[key]
	if key != EmailProviderCustom {
		return key, preset.IMAPHost, preset.IMAPPort, preset.SMTPHost, preset.SMTPPort, nil
	}
	imapHost = strings.TrimSpace(imapHost)
	smtpHost = strings.TrimSpace(smtpHost)
	if imapHost == "" || smtpHost == "" {
		return key, imapHost, 0, smtpHost, 0, fmt.Errorf("custom email provider requires IMAP and SMTP hosts")
	}
	imapPortInt, err := parseEmailPort(imapPort, preset.IMAPPort)
	if err != nil {
		return key, imapHost, 0, smtpHost, 0, fmt.Errorf("invalid IMAP port")
	}
	smtpPortInt, err := parseEmailPort(smtpPort, preset.SMTPPort)
	if err != nil {
		return key, imapHost, 0, smtpHost, 0, fmt.Errorf("invalid SMTP port")
	}
	return key, imapHost, imapPortInt, smtpHost, smtpPortInt, nil
}

func parseEmailPort(value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port")
	}
	return port, nil
}

type EmailConnectionStatus struct {
	Configured bool
	Running    bool
	Address    string
	Provider   string
	IMAPHost   string
	IMAPPort   int
	SMTPHost   string
	SMTPPort   int
}

type emailIMAPClient interface {
	Login(username, password string) error
	Select(name string, readOnly bool) (*imap.MailboxStatus, error)
	Search(criteria *imap.SearchCriteria) ([]uint32, error)
	Fetch(seqset *imap.SeqSet, items []imap.FetchItem, ch chan *imap.Message) error
	Store(seqset *imap.SeqSet, item imap.StoreItem, flags interface{}, ch chan *imap.Message) error
	Logout() error
}

type EmailService struct {
	settingsRepo          *repository.SettingsRepo
	projectRepo           *repository.ProjectRepo
	llmConfigRepo         *repository.LLMConfigRepo
	taskRepo              *repository.TaskRepo
	execRepo              *repository.ExecutionRepo
	scheduleRepo          *repository.ScheduleRepo
	taskSvc               *TaskService
	llmSvc                *LLMService
	workerSvc             *WorkerService
	emailAuthRepo         *repository.EmailAuthRepo
	emailTaskContextRepo  *repository.EmailTaskContextRepo
	threadInputRepo       *repository.ThreadInputRepo
	customPersonalityRepo *repository.CustomPersonalityRepo
	agentRepo             *repository.AgentRepo
	chatBroadcaster       *events.ChatBroadcaster
	queuedTurnPromoter    func(projectID string)
	channelChatRunner     ChannelChatRunner

	mu                       sync.RWMutex
	running                  bool
	ctx                      context.Context
	cancel                   context.CancelFunc
	connectIMAP              func(ctx context.Context, cfg EmailRuntimeConfig) (emailIMAPClient, error)
	sendMail                 func(ctx context.Context, cfg EmailRuntimeConfig, to, subject, body, inReplyTo, references string) error
	processIncomingMessageFn func(context.Context, EmailInboundMessage)
}

type EmailRuntimeConfig struct {
	Provider                string
	Address                 string
	Password                string
	IMAPHost                string
	IMAPPort                int
	SMTPHost                string
	SMTPPort                int
	PollInterval            time.Duration
	SendResponses           bool
	SkipAttachments         bool
	MarkExistingSeenOnStart bool
}

type EmailInboundMessage struct {
	FromName      string
	FromAddress   string
	Subject       string
	Body          string
	MessageID     string
	References    string
	AutoSubmitted string
	Precedence    string
	ListUnsub     string
}

func NewEmailService(settingsRepo *repository.SettingsRepo, projectRepo *repository.ProjectRepo, llmConfigRepo *repository.LLMConfigRepo, taskRepo *repository.TaskRepo, execRepo *repository.ExecutionRepo, scheduleRepo *repository.ScheduleRepo, taskSvc *TaskService, llmSvc *LLMService, workerSvc *WorkerService, emailAuthRepo *repository.EmailAuthRepo, emailTaskContextRepo *repository.EmailTaskContextRepo) *EmailService {
	s := &EmailService{
		settingsRepo:         settingsRepo,
		projectRepo:          projectRepo,
		llmConfigRepo:        llmConfigRepo,
		taskRepo:             taskRepo,
		execRepo:             execRepo,
		scheduleRepo:         scheduleRepo,
		taskSvc:              taskSvc,
		llmSvc:               llmSvc,
		workerSvc:            workerSvc,
		emailAuthRepo:        emailAuthRepo,
		emailTaskContextRepo: emailTaskContextRepo,
	}
	s.connectIMAP = defaultEmailIMAPConnect
	s.sendMail = defaultEmailSendMail
	return s
}

func (s *EmailService) SetChatBroadcaster(cb *events.ChatBroadcaster)       { s.chatBroadcaster = cb }
func (s *EmailService) SetThreadInputRepo(repo *repository.ThreadInputRepo) { s.threadInputRepo = repo }
func (s *EmailService) SetCustomPersonalityRepo(repo *repository.CustomPersonalityRepo) {
	s.customPersonalityRepo = repo
}
func (s *EmailService) SetAgentRepo(repo *repository.AgentRepo) { s.agentRepo = repo }
func (s *EmailService) SetQueuedTurnPromoter(promoter func(projectID string)) {
	s.queuedTurnPromoter = promoter
}
func (s *EmailService) SetChannelChatRunner(runner ChannelChatRunner) { s.channelChatRunner = runner }

func (s *EmailService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *EmailService) Start() error {
	cfg, err := s.loadConfig(context.Background())
	if err != nil || !cfg.Configured() {
		return err
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()
	go s.pollLoop(ctx, cfg)
	applog.Infof("[email] polling started for %s", cfg.Address)
	return nil
}

func (s *EmailService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
	applog.Infof("[email] polling stopped")
}

func (s *EmailService) ReloadFromSettings(ctx context.Context) error {
	s.Stop()
	return s.Start()
}

func (s *EmailService) TestConnection(ctx context.Context) error {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Configured() {
		return fmt.Errorf("email channel is not fully configured")
	}
	client, err := s.connectIMAP(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Logout()
	return nil
}

func (s *EmailService) GetConnectionStatus(ctx context.Context) EmailConnectionStatus {
	cfg, _ := s.loadConfig(ctx)
	return EmailConnectionStatus{
		Configured: cfg.Configured(), Running: s.IsRunning(), Address: cfg.Address, Provider: cfg.Provider,
		IMAPHost: cfg.IMAPHost, IMAPPort: cfg.IMAPPort, SMTPHost: cfg.SMTPHost, SMTPPort: cfg.SMTPPort,
	}
}

func (cfg EmailRuntimeConfig) Configured() bool {
	return strings.TrimSpace(cfg.Address) != "" && strings.TrimSpace(cfg.Password) != "" && strings.TrimSpace(cfg.IMAPHost) != "" && strings.TrimSpace(cfg.SMTPHost) != ""
}

func (s *EmailService) loadConfig(ctx context.Context) (EmailRuntimeConfig, error) {
	if s.settingsRepo == nil {
		return EmailRuntimeConfig{}, fmt.Errorf("settings repository not configured")
	}
	get := func(key string) string { val, _ := s.settingsRepo.Get(ctx, key); return strings.TrimSpace(val) }
	provider := NormalizeEmailProvider(get(EmailSettingProvider))
	if provider == "" {
		provider = EmailProviderCustom
	}
	imapPort, _ := parseEmailPort(get(EmailSettingIMAPPort), 993)
	smtpPort, _ := parseEmailPort(get(EmailSettingSMTPPort), 587)
	pollSeconds, _ := strconv.Atoi(get(EmailSettingPollIntervalSeconds))
	if pollSeconds <= 0 {
		pollSeconds = 15
	}
	cfg := EmailRuntimeConfig{
		Provider:                provider,
		Address:                 repository.NormalizeEmailAddress(get(EmailSettingAddress)),
		Password:                NormalizeEmailPasswordForProvider(provider, get(EmailSettingPassword)),
		IMAPHost:                get(EmailSettingIMAPHost),
		IMAPPort:                imapPort,
		SMTPHost:                get(EmailSettingSMTPHost),
		SMTPPort:                smtpPort,
		PollInterval:            time.Duration(pollSeconds) * time.Second,
		SendResponses:           strings.ToLower(get(EmailSettingSendResponses)) != "false",
		SkipAttachments:         strings.ToLower(get(EmailSettingSkipAttachments)) == "true",
		MarkExistingSeenOnStart: strings.ToLower(get(EmailSettingMarkExistingSeenOnStart)) != "false",
	}
	if cfg.IMAPHost == "" && provider != EmailProviderCustom {
		preset := emailProviderPresets[provider]
		cfg.IMAPHost, cfg.IMAPPort, cfg.SMTPHost, cfg.SMTPPort = preset.IMAPHost, preset.IMAPPort, preset.SMTPHost, preset.SMTPPort
	}
	return cfg, nil
}

func (s *EmailService) pollLoop(ctx context.Context, cfg EmailRuntimeConfig) {
	if cfg.MarkExistingSeenOnStart {
		if err := s.markUnreadSeen(ctx, cfg); err != nil {
			applog.Infof("[email] mark existing seen failed: %v", err)
		}
	}
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			s.pollOnce(ctx, cfg)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *EmailService) markUnreadSeen(ctx context.Context, cfg EmailRuntimeConfig) error {
	client, err := s.connectIMAP(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Logout()
	if _, err := client.Select("INBOX", false); err != nil {
		return err
	}
	ids, err := client.Search(unseenCriteria())
	if err != nil || len(ids) == 0 {
		return err
	}
	return storeSeen(client, ids)
}

func (s *EmailService) pollOnce(ctx context.Context, cfg EmailRuntimeConfig) {
	client, err := s.connectIMAP(ctx, cfg)
	if err != nil {
		applog.Infof("[email] IMAP connection failed: %v", err)
		return
	}
	defer client.Logout()
	if _, err := client.Select("INBOX", false); err != nil {
		applog.Infof("[email] select inbox failed: %v", err)
		return
	}
	ids, err := client.Search(unseenCriteria())
	if err != nil || len(ids) == 0 {
		if err != nil {
			applog.Infof("[email] search unread failed: %v", err)
		}
		return
	}
	messages, err := fetchEmailMessages(client, ids)
	if err != nil {
		applog.Infof("[email] fetch messages failed: %v", err)
		return
	}
	for _, msg := range messages {
		s.ProcessIncoming(ctx, msg)
	}
	if err := storeSeen(client, ids); err != nil {
		applog.Infof("[email] mark seen failed: %v", err)
	}
}

func unseenCriteria() *imap.SearchCriteria {
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	return criteria
}

func storeSeen(client emailIMAPClient, ids []uint32) error {
	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)
	return client.Store(seqset, imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.SeenFlag}, nil)
}

func defaultEmailIMAPConnect(ctx context.Context, cfg EmailRuntimeConfig) (emailIMAPClient, error) {
	addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)
	client, err := imapclient.DialTLS(addr, &tls.Config{ServerName: cfg.IMAPHost})
	if err != nil {
		return nil, fmt.Errorf("connect IMAP: %w", err)
	}
	if err := client.Login(cfg.Address, cfg.Password); err != nil {
		_ = client.Logout()
		return nil, fmt.Errorf("login IMAP: %w", err)
	}
	return client, nil
}

func fetchEmailMessages(client emailIMAPClient, ids []uint32) ([]EmailInboundMessage, error) {
	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}
	ch := make(chan *imap.Message, len(ids))
	if err := client.Fetch(seqset, items, ch); err != nil {
		return nil, err
	}
	var out []EmailInboundMessage
	for msg := range ch {
		if msg == nil {
			continue
		}
		inbound, err := parseIMAPMessage(msg, section)
		if err != nil {
			applog.Infof("[email] parse message failed: %v", err)
			continue
		}
		out = append(out, inbound)
	}
	return out, nil
}

func parseIMAPMessage(msg *imap.Message, section *imap.BodySectionName) (EmailInboundMessage, error) {
	var inbound EmailInboundMessage
	if msg.Envelope != nil {
		inbound.Subject = strings.TrimSpace(msg.Envelope.Subject)
		inbound.MessageID = strings.TrimSpace(msg.Envelope.MessageId)
		if len(msg.Envelope.From) > 0 {
			from := msg.Envelope.From[0]
			inbound.FromName = strings.TrimSpace(from.PersonalName)
			inbound.FromAddress = repository.NormalizeEmailAddress(from.MailboxName + "@" + from.HostName)
		}
	}
	body := msg.GetBody(section)
	if body != nil {
		mr, err := messagemail.CreateReader(body)
		if err == nil {
			h := mr.Header
			if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
				inbound.FromName = from[0].Name
				inbound.FromAddress = repository.NormalizeEmailAddress(from[0].Address)
			}
			if subj, err := h.Subject(); err == nil && subj != "" {
				inbound.Subject = subj
			}
			inbound.MessageID = firstNonEmpty(h.Get("Message-ID"), inbound.MessageID)
			inbound.References = firstNonEmpty(h.Get("References"), inbound.References)
			inbound.AutoSubmitted = h.Get("Auto-Submitted")
			inbound.Precedence = h.Get("Precedence")
			inbound.ListUnsub = h.Get("List-Unsubscribe")
			inbound.Body = readPlainTextBody(mr)
		} else {
			raw, _ := io.ReadAll(body)
			inbound.Body = string(raw)
		}
	}
	if inbound.FromAddress == "" {
		return inbound, fmt.Errorf("missing sender")
	}
	return inbound, nil
}

func readPlainTextBody(mr *messagemail.Reader) string {
	var fallback string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *messagemail.InlineHeader:
			contentType, _, _ := h.ContentType()
			b, _ := io.ReadAll(part.Body)
			if strings.HasPrefix(contentType, "text/plain") {
				return strings.TrimSpace(string(b))
			}
			if fallback == "" && strings.HasPrefix(contentType, "text/html") {
				fallback = stripHTML(string(b))
			}
		}
	}
	return strings.TrimSpace(fallback)
}

var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string { return strings.TrimSpace(htmlTagRE.ReplaceAllString(s, " ")) }
func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func (s *EmailService) ProcessIncoming(ctx context.Context, msg EmailInboundMessage) {
	if s.processIncomingMessageFn != nil {
		s.processIncomingMessageFn(ctx, msg)
		return
	}
	s.processIncomingMessage(ctx, msg)
}

func (s *EmailService) processIncomingMessage(ctx context.Context, msg EmailInboundMessage) {
	if isIgnoredEmail(msg, s.getConfiguredAddress(ctx)) || strings.TrimSpace(msg.Body) == "" {
		return
	}
	if s.taskRepo == nil || s.execRepo == nil || s.llmConfigRepo == nil || s.llmSvc == nil || s.taskSvc == nil || s.projectRepo == nil {
		applog.Infof("[email] incoming message ignored: service dependencies are not fully configured")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), emailProcessTimeout)
	defer cancel()
	projectID := s.resolveAuthorizedProject(ctx, msg.FromAddress)
	if projectID == "" {
		applog.Infof("[email] unauthorized or no project for sender=%s", redactEmail(msg.FromAddress))
		return
	}
	agent, err := s.autoSelectAgent(ctx, msg.Body)
	if err != nil {
		applog.Infof("[email] model selection failed: %v", err)
		return
	}
	prompt := BuildEmailPrompt(msg)
	sessionKey := EmailSessionKey(msg.FromAddress, msg.MessageID, msg.References, msg.Subject)
	if activeChatExec, activeErr := s.execRepo.FindLatestActiveEmailChatExecution(ctx, projectID, sessionKey); activeErr != nil {
		applog.Infof("[email] active chat check failed: %v", activeErr)
		return
	} else if activeChatExec != nil {
		if s.queueChatInput(ctx, projectID, activeChatExec.ID, agent.ID, prompt, msg, sessionKey) {
			return
		}
	}
	selectedAgentID := agent.ID
	task := &models.Task{ProjectID: projectID, Title: fmt.Sprintf("Email %s: %s", time.Now().Format("15:04:05.000"), util.Truncate(msg.Subject, 47)), Prompt: prompt, Status: models.StatusPending, Category: models.CategoryChat, AgentID: &selectedAgentID, CreatedVia: models.TaskOriginEmail}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		applog.Infof("[email] create chat task failed: %v", err)
		return
	}
	if s.emailTaskContextRepo != nil {
		if err := s.emailTaskContextRepo.Upsert(ctx, &models.EmailTaskContext{TaskID: task.ID, EmailFrom: msg.FromAddress, EmailMessageID: msg.MessageID, EmailReferences: msg.References, EmailSubject: msg.Subject, EmailSessionKey: sessionKey}); err != nil {
			applog.Infof("[email] create task context failed task=%s: %v", task.ID, err)
			_ = s.taskRepo.Delete(ctx, task.ID)
			return
		}
	}
	exec := &models.Execution{TaskID: task.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: prompt}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		applog.Infof("[email] create execution failed: %v", err)
		_ = s.taskRepo.Delete(ctx, task.ID)
		return
	}
	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, ExecID: exec.ID, TaskID: task.ID, Message: prompt, Source: models.TaskOriginEmail, AgentName: agent.Name})
	}
	history, err := s.execRepo.ListEmailChatHistory(ctx, projectID, sessionKey, emailChatHistoryLimit)
	if err != nil {
		history = []models.Execution{}
	}
	systemContext := s.buildChatContext(ctx, projectID)
	if personalityPrompt := s.getPersonalityContext(ctx, projectID); personalityPrompt != "" {
		systemContext += personalityPrompt
	}
	if s.channelChatRunner == nil {
		s.completeExecution(ctx, exec.ID, task.ID, "", "Email chat runner is unavailable", 0, 0)
		return
	}
	s.channelChatRunner(context.Background(), ChannelChatRunRequest{
		ExecID:        exec.ID,
		TaskID:        task.ID,
		ProjectID:     projectID,
		Message:       prompt,
		Agent:         *agent,
		ChatHistory:   filterEmailChatHistory(history, exec.ID),
		SystemContext: systemContext,
		WorkDir:       s.resolveWorkDir(ctx, projectID),
		Surface:       chatcontrol.SurfaceEmail,
		ReplyContext: ChannelReplyContext{
			Source:          models.TaskOriginEmail,
			EmailFrom:       msg.FromAddress,
			EmailMessageID:  msg.MessageID,
			EmailReferences: msg.References,
			EmailSubject:    msg.Subject,
			EmailSessionKey: sessionKey,
		},
	})
}

func (s *EmailService) queueChatInput(ctx context.Context, projectID, activeExecID, agentID, prompt string, msg EmailInboundMessage, sessionKey string) bool {
	if s.threadInputRepo == nil {
		return false
	}
	queued := &models.ThreadInput{Scope: models.ThreadInputScopeChat, ProjectID: projectID, RunExecutionID: activeExecID, AgentConfigID: agentID, InputMode: models.ThreadInputModeQueued, InputStatus: models.ThreadInputPending, Content: prompt, ChatMode: models.ChatModeOrchestrate, Source: models.TaskOriginEmail, EmailFrom: msg.FromAddress, EmailMessageID: msg.MessageID, EmailReferences: msg.References, EmailSubject: msg.Subject, EmailSessionKey: sessionKey}
	if err := s.threadInputRepo.CreateQueued(ctx, queued); err != nil {
		applog.Infof("[email] queue chat input failed: %v", err)
		return true
	}
	if s.chatBroadcaster != nil {
		s.chatBroadcaster.Publish(events.ChatEvent{Type: events.ChatNewMessage, ProjectID: projectID, ExecID: queued.ID, Message: prompt, Source: models.TaskOriginEmail, Queued: true})
	}
	return true
}

func (s *EmailService) resolveAuthorizedProject(ctx context.Context, sender string) string {
	if s.emailAuthRepo == nil || sender == "" {
		return ""
	}
	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return ""
	}
	for _, p := range projects {
		hasAny, err := s.emailAuthRepo.HasAnyAuthorizedUsers(ctx, p.ID)
		if err != nil || !hasAny {
			continue
		}
		ok, err := s.emailAuthRepo.IsAuthorized(ctx, p.ID, sender)
		if err == nil && ok {
			return p.ID
		}
	}
	return ""
}

func (s *EmailService) getConfiguredAddress(ctx context.Context) string {
	if s.settingsRepo == nil {
		return ""
	}
	v, _ := s.settingsRepo.Get(ctx, EmailSettingAddress)
	return repository.NormalizeEmailAddress(v)
}

func isIgnoredEmail(msg EmailInboundMessage, selfAddress string) bool {
	from := repository.NormalizeEmailAddress(msg.FromAddress)
	if from == "" || (selfAddress != "" && from == repository.NormalizeEmailAddress(selfAddress)) {
		return true
	}
	if auto := strings.TrimSpace(strings.ToLower(msg.AutoSubmitted)); auto != "" && auto != "no" {
		return true
	}
	precedence := strings.TrimSpace(strings.ToLower(msg.Precedence))
	if precedence == "bulk" || precedence == "list" || precedence == "junk" {
		return true
	}
	if strings.TrimSpace(msg.ListUnsub) != "" {
		return true
	}
	local := from
	if idx := strings.Index(local, "@"); idx >= 0 {
		local = local[:idx]
	}
	for _, token := range []string{"no-reply", "noreply", "do-not-reply", "donotreply", "mailer-daemon"} {
		if strings.Contains(local, token) {
			return true
		}
	}
	return false
}

func BuildEmailPrompt(msg EmailInboundMessage) string {
	name := strings.TrimSpace(msg.FromName)
	from := msg.FromAddress
	if name != "" {
		from = fmt.Sprintf("%s <%s>", name, msg.FromAddress)
	}
	return fmt.Sprintf("[Email from: %s]\n[Subject: %s]\n\n%s", from, strings.TrimSpace(msg.Subject), strings.TrimSpace(msg.Body))
}

func EmailSessionKey(sender, messageID, references, subject string) string {
	sender = repository.NormalizeEmailAddress(sender)
	root := strings.TrimSpace(references)
	if root != "" {
		parts := strings.Fields(root)
		root = parts[0]
	}
	if root == "" {
		root = strings.TrimSpace(messageID)
	}
	if root != "" {
		return "email:" + sender + ":" + root
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(subject))))
	return "email:" + sender + ":" + hex.EncodeToString(h[:8])
}

func (s *EmailService) autoSelectAgent(ctx context.Context, message string) (*models.LLMConfig, error) {
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

func (s *EmailService) buildChatContext(ctx context.Context, projectID string) string {
	var tasks []models.Task
	if s.taskSvc != nil {
		tasks, _ = s.taskSvc.ListByProject(ctx, projectID, "")
	}
	modelsList, _ := s.llmConfigRepo.List(ctx)
	var schedules []models.Schedule
	if s.scheduleRepo != nil {
		schedules, _ = s.scheduleRepo.ListByProject(ctx, projectID)
	}
	return BuildChatContextWithAgentDefinitions(tasks, modelsList, s.listChatAssignableAgentDefinitions(ctx), schedules, time.Now())
}

func (s *EmailService) listChatAssignableAgentDefinitions(ctx context.Context) []models.Agent {
	if s.agentRepo == nil {
		return nil
	}
	agents, err := s.agentRepo.List(ctx)
	if err != nil {
		return nil
	}
	return UniqueChatAssignableAgentDefinitions(agents)
}

func (s *EmailService) resolveWorkDir(ctx context.Context, projectID string) string {
	if s.projectRepo == nil {
		return ""
	}
	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil || project == nil {
		return ""
	}
	return project.RepoPath
}

func (s *EmailService) getPersonalityContext(ctx context.Context, projectID string) string {
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

func filterEmailChatHistory(history []models.Execution, currentExecID string) []models.Execution {
	filtered := make([]models.Execution, 0, len(history))
	for _, exec := range history {
		if exec.ID != currentExecID {
			filtered = append(filtered, exec)
		}
	}
	return filtered
}

func (s *EmailService) completeExecution(ctx context.Context, execID, taskID, output, errorMessage string, tokensUsed int, durationMs int64) {
	if errorMessage != "" {
		_ = s.execRepo.Complete(ctx, execID, models.ExecFailed, "", errorMessage, 0, durationMs)
		_ = s.taskRepo.UpdateStatus(ctx, taskID, models.StatusFailed)
	} else {
		_ = s.execRepo.Complete(ctx, execID, models.ExecCompleted, output, "", tokensUsed, durationMs)
		_ = s.taskRepo.UpdateStatus(ctx, taskID, models.StatusCompleted)
	}
	if s.queuedTurnPromoter != nil {
		if task, err := s.taskRepo.GetByID(ctx, taskID); err == nil && task != nil {
			s.queuedTurnPromoter(task.ProjectID)
		}
	}
}

func (s *EmailService) IsSendResponsesEnabled(ctx context.Context) bool {
	cfg, _ := s.loadConfig(ctx)
	return cfg.SendResponses
}

func (s *EmailService) SendTaskCompletionToThread(ctx context.Context, to, inboundMessageID, references, subject, taskTitle, output, errMsg string) {
	if !s.IsSendResponsesEnabled(ctx) || strings.TrimSpace(to) == "" {
		return
	}
	body := buildEmailCompletionBody(taskTitle, output, errMsg)
	if err := s.sendReply(ctx, to, subject, body, inboundMessageID, appendEmailReference(references, inboundMessageID)); err != nil {
		applog.Infof("[email] send thread reply failed: %v", err)
	}
}

func (s *EmailService) SendChatResponse(ctx context.Context, task models.Task, output, errMsg string) {
	if s.emailTaskContextRepo == nil || !s.IsSendResponsesEnabled(ctx) {
		return
	}
	etc, err := s.emailTaskContextRepo.GetByTaskID(ctx, task.ID)
	if err != nil || etc == nil {
		return
	}
	body := buildEmailChatBody(output, errMsg)
	if err := s.sendReply(ctx, etc.EmailFrom, etc.EmailSubject, body, etc.EmailMessageID, appendEmailReference(etc.EmailReferences, etc.EmailMessageID)); err != nil {
		applog.Infof("[email] send chat response failed task=%s: %v", task.ID, err)
	}
}

func (s *EmailService) SendTaskCompletionNotification(ctx context.Context, task models.Task, output, errMsg string) {
	if task.CreatedVia != models.TaskOriginEmail && task.ID != "" && s.taskRepo != nil {
		if loaded, err := s.taskRepo.GetByID(ctx, task.ID); err == nil && loaded != nil {
			task = *loaded
		}
	}
	if task.CreatedVia != models.TaskOriginEmail || task.Category == models.CategoryChat || s.emailTaskContextRepo == nil || !s.IsSendResponsesEnabled(ctx) {
		return
	}
	etc, err := s.emailTaskContextRepo.GetByTaskID(ctx, task.ID)
	if err != nil || etc == nil {
		return
	}
	body := buildEmailCompletionBody(task.Title, output, errMsg)
	if err := s.sendReply(ctx, etc.EmailFrom, etc.EmailSubject, body, etc.EmailMessageID, appendEmailReference(etc.EmailReferences, etc.EmailMessageID)); err != nil {
		applog.Infof("[email] send task notification failed task=%s: %v", task.ID, err)
	}
}

func (s *EmailService) sendReply(ctx context.Context, to, subject, body, inReplyTo, references string) error {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Configured() {
		return fmt.Errorf("email channel is not fully configured")
	}
	return s.sendMail(ctx, cfg, to, replySubject(subject), body, inReplyTo, references)
}

func buildEmailCompletionBody(taskTitle, output, errMsg string) string {
	if errMsg != "" {
		return fmt.Sprintf("Task failed: %s\n\n%s", taskTitle, util.Truncate(errMsg, 1000))
	}
	cleaned := llmoutput.CleanChatOutputForDisplay(output)
	if cleaned == "" {
		cleaned = "(No output)"
	}
	return fmt.Sprintf("Task completed: %s\n\n%s", taskTitle, util.Truncate(cleaned, 8000))
}

func buildEmailChatBody(output, errMsg string) string {
	if errMsg != "" {
		return "Error: " + util.Truncate(errMsg, 1000)
	}
	cleaned := llmoutput.CleanChatOutputForDisplay(output)
	if cleaned == "" {
		return "(No output)"
	}
	return util.Truncate(cleaned, 8000)
}

func replySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "OpenVibely response"
	}
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func appendEmailReference(references, messageID string) string {
	refs := strings.TrimSpace(references)
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return refs
	}
	if refs == "" {
		return messageID
	}
	if strings.Contains(refs, messageID) {
		return refs
	}
	return refs + " " + messageID
}

func defaultEmailSendMail(ctx context.Context, cfg EmailRuntimeConfig, to, subject, body, inReplyTo, references string) error {
	var buf bytes.Buffer
	from := mailAddress(cfg.Address)
	toAddr := mailAddress(to)
	headers := map[string]string{"From": from.String(), "To": toAddr.String(), "Subject": mime.QEncoding.Encode("utf-8", subject), "MIME-Version": "1.0", "Content-Type": `text/plain; charset="utf-8"`, "Content-Transfer-Encoding": "8bit"}
	if inReplyTo != "" {
		headers["In-Reply-To"] = inReplyTo
	}
	if references != "" {
		headers["References"] = references
	}
	for _, key := range []string{"From", "To", "Subject", "MIME-Version", "Content-Type", "Content-Transfer-Encoding", "In-Reply-To", "References"} {
		if v := headers[key]; v != "" {
			fmt.Fprintf(&buf, "%s: %s\r\n", key, v)
		}
	}
	buf.WriteString("\r\n")
	buf.WriteString(body)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	auth := smtp.PlainAuth("", cfg.Address, cfg.Password, cfg.SMTPHost)
	return smtp.SendMail(addr, auth, cfg.Address, []string{to}, buf.Bytes())
}

func mailAddress(address string) *netmail.Address {
	return &netmail.Address{Address: strings.TrimSpace(address)}
}

func redactEmail(email string) string {
	email = repository.NormalizeEmailAddress(email)
	at := strings.Index(email, "@")
	if at <= 1 {
		return "[redacted]"
	}
	return email[:1] + "***" + email[at:]
}
