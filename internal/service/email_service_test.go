package service

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEmailPasswordForProvider(t *testing.T) {
	assert.Equal(t, "abcdefghijklmnop", NormalizeEmailPasswordForProvider(EmailProviderGmail, " abcd efgh ijkl mnop "))
	assert.Equal(t, "abc def", NormalizeEmailPasswordForProvider(EmailProviderCustom, " abc def "))
}

func TestEmailService_LoadConfigNormalizesSavedProviderAppPassword(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingProvider, EmailProviderGmail))
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingAddress, "bot@example.com"))
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingPassword, "abcd efgh ijkl mnop"))
	svc := NewEmailService(settingsRepo, repository.NewProjectRepo(db), repository.NewLLMConfigRepo(db), repository.NewTaskRepo(db, nil), repository.NewExecutionRepo(db), repository.NewScheduleRepo(db), nil, nil, nil, repository.NewEmailAuthRepo(db), repository.NewEmailTaskContextRepo(db))

	cfg, err := svc.loadConfig(ctx)
	require.NoError(t, err)
	assert.Equal(t, "abcdefghijklmnop", cfg.Password)
}

func TestEmailService_IgnoresUnauthorizedAutomatedAndSelfSentMessages(t *testing.T) {
	assert.True(t, isIgnoredEmail(EmailInboundMessage{FromAddress: "bot@example.com", Subject: "self", Body: "hello"}, "bot@example.com"))
	assert.True(t, isIgnoredEmail(EmailInboundMessage{FromAddress: "noreply@example.com", Subject: "auto", Body: "hello"}, "bot@example.com"))
	assert.True(t, isIgnoredEmail(EmailInboundMessage{FromAddress: "user@example.com", Subject: "list", Body: "hello", ListUnsub: "<mailto:unsubscribe@example.com>"}, "bot@example.com"))
	assert.True(t, isIgnoredEmail(EmailInboundMessage{FromAddress: "user@example.com", Subject: "bulk", Body: "hello", Precedence: "bulk"}, "bot@example.com"))
	assert.False(t, isIgnoredEmail(EmailInboundMessage{FromAddress: "alice@example.com", Subject: "ok", Body: "hello"}, "bot@example.com"))
}

func TestEmailService_AuthorizationRequiresConfiguredSender(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	settingsRepo := repository.NewSettingsRepo(db)
	projectRepo := repository.NewProjectRepo(db)
	emailAuthRepo := repository.NewEmailAuthRepo(db)
	project := &models.Project{Name: "Email Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	svc := NewEmailService(settingsRepo, projectRepo, repository.NewLLMConfigRepo(db), repository.NewTaskRepo(db, nil), repository.NewExecutionRepo(db), repository.NewScheduleRepo(db), nil, nil, nil, emailAuthRepo, repository.NewEmailTaskContextRepo(db))

	assert.Empty(t, svc.resolveAuthorizedProject(ctx, "alice@example.com"))
	require.NoError(t, emailAuthRepo.Create(ctx, &models.EmailAuthorizedSender{ProjectID: project.ID, EmailAddress: "Alice@Example.com", AddedBy: "test"}))
	assert.Equal(t, project.ID, svc.resolveAuthorizedProject(ctx, "alice@example.com"))
	assert.Empty(t, svc.resolveAuthorizedProject(ctx, "bob@example.com"))
}

func TestEmailThreadingHelpers(t *testing.T) {
	msg := EmailInboundMessage{FromName: "Alice", FromAddress: "alice@example.com", Subject: "Deploy question", Body: "What now?", MessageID: "<m2@example.com>", References: "<root@example.com> <m1@example.com>"}
	assert.Equal(t, "[Email from: Alice <alice@example.com>]\n[Subject: Deploy question]\n\nWhat now?", BuildEmailPrompt(msg))
	assert.Equal(t, "email:alice@example.com:<root@example.com>", EmailSessionKey("Alice@Example.com", msg.MessageID, msg.References, msg.Subject))
	assert.Equal(t, "Re: Deploy question", replySubject("Deploy question"))
	assert.Equal(t, "Re: Deploy question", replySubject("Re: Deploy question"))
	assert.Equal(t, "<root@example.com> <m1@example.com> <m2@example.com>", appendEmailReference(msg.References, msg.MessageID))
	assert.NotEqual(t, EmailSessionKey("alice@example.com", "", "", "Subject A"), EmailSessionKey("alice@example.com", "", "", "Subject B"))
}

func TestEmailService_UsesThreadScopedSessionForActiveChatAndHistory(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	settingsRepo := repository.NewSettingsRepo(db)
	projectRepo := repository.NewProjectRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	emailAuthRepo := repository.NewEmailAuthRepo(db)
	emailTaskContextRepo := repository.NewEmailTaskContextRepo(db)
	project := &models.Project{Name: "Email Session Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, emailAuthRepo.Create(ctx, &models.EmailAuthorizedSender{ProjectID: project.ID, EmailAddress: "alice@example.com", AddedBy: "test"}))
	agent := &models.LLMConfig{Name: "Email Agent", Provider: models.ProviderTest, Model: "test", IsDefault: true}
	require.NoError(t, llmConfigRepo.Create(ctx, agent))
	activeTask := &models.Task{ProjectID: project.ID, Title: "Root Thread", Prompt: "root", Category: models.CategoryChat, Status: models.StatusRunning, CreatedVia: models.TaskOriginEmail, AgentID: &agent.ID}
	require.NoError(t, taskRepo.Create(ctx, activeTask))
	require.NoError(t, emailTaskContextRepo.Upsert(ctx, &models.EmailTaskContext{TaskID: activeTask.ID, EmailFrom: "alice@example.com", EmailMessageID: "<root@example.com>", EmailSubject: "Root", EmailSessionKey: "email:alice@example.com:<root@example.com>"}))
	activeExec := &models.Execution{TaskID: activeTask.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "root active"}
	require.NoError(t, execRepo.Create(ctx, activeExec))
	completedTask := &models.Task{ProjectID: project.ID, Title: "Other Completed", Prompt: "other", Category: models.CategoryChat, Status: models.StatusCompleted, CreatedVia: models.TaskOriginEmail, AgentID: &agent.ID}
	require.NoError(t, taskRepo.Create(ctx, completedTask))
	require.NoError(t, emailTaskContextRepo.Upsert(ctx, &models.EmailTaskContext{TaskID: completedTask.ID, EmailFrom: "alice@example.com", EmailMessageID: "<other@example.com>", EmailSubject: "Other", EmailSessionKey: "email:alice@example.com:<other@example.com>"}))
	completedExec := &models.Execution{TaskID: completedTask.ID, AgentConfigID: agent.ID, Status: models.ExecCompleted, PromptSent: "other prior"}
	require.NoError(t, execRepo.Create(ctx, completedExec))

	attachmentRepo := repository.NewAttachmentRepo(db)
	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, repository.NewScheduleRepo(db), attachmentRepo)
	llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewEmailService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, repository.NewScheduleRepo(db), taskSvc, llmSvc, workerSvc, emailAuthRepo, emailTaskContextRepo)
	svc.SetThreadInputRepo(repository.NewThreadInputRepo(db))
	var runReq ChannelChatRunRequest
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) { runReq = req })
	svc.ProcessIncoming(ctx, EmailInboundMessage{FromAddress: "alice@example.com", Subject: "Other", Body: "continue other", MessageID: "<other-2@example.com>", References: "<other@example.com>"})

	require.NotEmpty(t, runReq.ExecID)
	require.Equal(t, "email:alice@example.com:<other@example.com>", runReq.ReplyContext.EmailSessionKey)
	require.Len(t, runReq.ChatHistory, 1)
	require.Equal(t, completedExec.ID, runReq.ChatHistory[0].ID)
	pending, err := repository.NewThreadInputRepo(db).ListPendingForChat(ctx, project.ID)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestEmailService_SendResponsesDisabledSkipsReplies(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingAddress, "bot@example.com"))
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingPassword, "secret"))
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingIMAPHost, "imap.example.com"))
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingSMTPHost, "smtp.example.com"))
	require.NoError(t, settingsRepo.Set(ctx, EmailSettingSendResponses, "false"))
	svc := NewEmailService(settingsRepo, repository.NewProjectRepo(db), repository.NewLLMConfigRepo(db), repository.NewTaskRepo(db, nil), repository.NewExecutionRepo(db), repository.NewScheduleRepo(db), nil, nil, nil, repository.NewEmailAuthRepo(db), repository.NewEmailTaskContextRepo(db))
	called := false
	svc.sendMail = func(context.Context, EmailRuntimeConfig, string, string, string, string, string) error {
		called = true
		return nil
	}
	svc.SendTaskCompletionToThread(ctx, "alice@example.com", "<m@example.com>", "", "Question", "Task", "ok", "")
	assert.False(t, called)
}

func TestEmailService_SendTaskCompletionPreservesThreading(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	settingsRepo := repository.NewSettingsRepo(db)
	for k, v := range map[string]string{EmailSettingAddress: "bot@example.com", EmailSettingPassword: "secret", EmailSettingIMAPHost: "imap.example.com", EmailSettingSMTPHost: "smtp.example.com", EmailSettingSendResponses: "true"} {
		require.NoError(t, settingsRepo.Set(ctx, k, v))
	}
	svc := NewEmailService(settingsRepo, repository.NewProjectRepo(db), repository.NewLLMConfigRepo(db), repository.NewTaskRepo(db, nil), repository.NewExecutionRepo(db), repository.NewScheduleRepo(db), nil, nil, nil, repository.NewEmailAuthRepo(db), repository.NewEmailTaskContextRepo(db))
	var gotTo, gotSubject, gotInReplyTo, gotRefs string
	svc.sendMail = func(_ context.Context, _ EmailRuntimeConfig, to, subject, body, inReplyTo, references string) error {
		gotTo, gotSubject, gotInReplyTo, gotRefs = to, subject, inReplyTo, references
		assert.Contains(t, body, "Task completed")
		return nil
	}
	svc.SendTaskCompletionToThread(ctx, "alice@example.com", "<m@example.com>", "<root@example.com>", "Question", "Task", "done", "")
	assert.Equal(t, "alice@example.com", gotTo)
	assert.Equal(t, "Re: Question", gotSubject)
	assert.Equal(t, "<m@example.com>", gotInReplyTo)
	assert.Equal(t, "<root@example.com> <m@example.com>", gotRefs)
}

func TestEmailService_SendOutboundMessage_NewEmailUsesSMTPWithoutReplyHeaders(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	settingsRepo := repository.NewSettingsRepo(db)
	for k, v := range map[string]string{EmailSettingAddress: "bot@example.com", EmailSettingPassword: "secret", EmailSettingIMAPHost: "imap.example.com", EmailSettingSMTPHost: "smtp.example.com"} {
		require.NoError(t, settingsRepo.Set(ctx, k, v))
	}
	svc := NewEmailService(settingsRepo, repository.NewProjectRepo(db), repository.NewLLMConfigRepo(db), repository.NewTaskRepo(db, nil), repository.NewExecutionRepo(db), repository.NewScheduleRepo(db), nil, nil, nil, repository.NewEmailAuthRepo(db), repository.NewEmailTaskContextRepo(db))
	var gotTo, gotSubject, gotBody, gotInReplyTo, gotRefs string
	svc.sendMail = func(_ context.Context, _ EmailRuntimeConfig, to, subject, body, inReplyTo, references string) error {
		gotTo, gotSubject, gotBody, gotInReplyTo, gotRefs = to, subject, body, inReplyTo, references
		return nil
	}
	res := svc.SendOutboundMessage(ctx, "Person <Person@Example.com>", "", "hello")
	require.True(t, res.OK)
	require.Equal(t, "person@example.com", gotTo)
	require.Equal(t, "OpenVibely", gotSubject)
	require.Equal(t, "hello", gotBody)
	require.Empty(t, gotInReplyTo)
	require.Empty(t, gotRefs)
}

func TestEmailService_SendOutboundMessage_ValidationAndMissingConfig(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := NewEmailService(repository.NewSettingsRepo(db), repository.NewProjectRepo(db), repository.NewLLMConfigRepo(db), repository.NewTaskRepo(db, nil), repository.NewExecutionRepo(db), repository.NewScheduleRepo(db), nil, nil, nil, repository.NewEmailAuthRepo(db), repository.NewEmailTaskContextRepo(db))
	invalid := svc.SendOutboundMessage(context.Background(), "not-an-email", "Subject", "body")
	require.False(t, invalid.OK)
	require.Contains(t, invalid.Error, "invalid email recipient")
	missing := svc.SendOutboundMessage(context.Background(), "person@example.com", "Subject", "body")
	require.False(t, missing.OK)
	require.Contains(t, missing.Error, "email channel is not fully configured")
}

func TestEmailService_CompleteExecutionUsesSharedChatPromotion(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	project := &models.Project{Name: "Email Promotion Project"}
	require.NoError(t, repository.NewProjectRepo(db).Create(ctx, project))
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	agent := &models.LLMConfig{Name: "Email Promotion Agent", Provider: models.ProviderTest, Model: "test", IsDefault: true}
	require.NoError(t, llmConfigRepo.Create(ctx, agent))
	agentID := agent.ID

	chatTask := &models.Task{ProjectID: project.ID, Title: "Email Chat", Prompt: "chat", Category: models.CategoryChat, Status: models.StatusRunning, CreatedVia: models.TaskOriginEmail, AgentID: &agentID}
	require.NoError(t, taskRepo.Create(ctx, chatTask))
	chatExec := &models.Execution{TaskID: chatTask.ID, AgentConfigID: agentID, Status: models.ExecRunning, PromptSent: "chat"}
	require.NoError(t, execRepo.Create(ctx, chatExec))

	nonChatTask := &models.Task{ProjectID: project.ID, Title: "Email Task", Prompt: "task", Category: models.CategoryActive, Status: models.StatusRunning, CreatedVia: models.TaskOriginEmail, AgentID: &agentID}
	require.NoError(t, taskRepo.Create(ctx, nonChatTask))
	nonChatExec := &models.Execution{TaskID: nonChatTask.ID, AgentConfigID: agentID, Status: models.ExecRunning, PromptSent: "task"}
	require.NoError(t, execRepo.Create(ctx, nonChatExec))

	svc := NewEmailService(repository.NewSettingsRepo(db), repository.NewProjectRepo(db), repository.NewLLMConfigRepo(db), taskRepo, execRepo, repository.NewScheduleRepo(db), nil, nil, nil, repository.NewEmailAuthRepo(db), repository.NewEmailTaskContextRepo(db))
	var promoted []string
	svc.queuedTurnPromoter = func(projectID string) { promoted = append(promoted, projectID) }

	svc.completeExecution(ctx, nonChatExec.ID, nonChatTask.ID, "done", "", 1, 10)
	require.Empty(t, promoted)

	svc.completeExecution(ctx, chatExec.ID, chatTask.ID, "done", "", 1, 10)
	require.Equal(t, []string{project.ID}, promoted)
}
