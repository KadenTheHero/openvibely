package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func newDiscordServiceForTest(t *testing.T) (*DiscordService, *sql.DB, *repository.SettingsRepo, *repository.ProjectRepo, *repository.TaskRepo, *repository.DiscordAuthRepo, *repository.DiscordTaskContextRepo) {
	t.Helper()
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	discordAuthRepo := repository.NewDiscordAuthRepo(db)
	discordTaskContextRepo := repository.NewDiscordTaskContextRepo(db)
	svc := NewDiscordService(settingsRepo, projectRepo, nil, taskRepo, nil, nil, nil, nil, nil, discordAuthRepo, discordTaskContextRepo)
	return svc, db, settingsRepo, projectRepo, taskRepo, discordAuthRepo, discordTaskContextRepo
}

func TestDiscordService_SendOutboundMessageUsesChannelAndThread(t *testing.T) {
	svc, _, _, _, _, _, _ := newDiscordServiceForTest(t)
	var gotChannelID, gotMessageID, gotText string
	svc.sendMessageFunc = func(channelID, messageID, text string) (string, error) {
		gotChannelID, gotMessageID, gotText = channelID, messageID, text
		return "discord-msg-1", nil
	}

	res := svc.SendOutboundMessage(context.Background(), "chan-1", "thread-1", "hello discord")
	if !res.OK || res.Platform != "discord" || res.Target != "discord:chan-1:thread-1" || res.MessageID != "discord-msg-1" {
		t.Fatalf("unexpected outbound result: %#v", res)
	}
	if gotChannelID != "thread-1" || gotMessageID != "" || gotText != "hello discord" {
		t.Fatalf("unexpected outbound send channel=%q message=%q text=%q", gotChannelID, gotMessageID, gotText)
	}
}

func TestDiscordActionHandlersSendMessageUsesChannelRouter(t *testing.T) {
	svc, db, _, projectRepo, _, _, _ := newDiscordServiceForTest(t)
	ctx := context.Background()
	project := &models.Project{Name: "Discord Outbound"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	targetRepo := repository.NewChannelTargetRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	router := NewChannelMessageRouter(targetRepo, settingsRepo)
	router.SetDiscordService(svc)
	svc.SetChannelMessageRouter(router)
	if err := targetRepo.Upsert(ctx, models.ChannelTarget{ID: "discord-target", ProjectID: project.ID, Platform: "discord", Name: "ops", TargetID: "chan-1", ThreadID: "thread-1"}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	var gotChannelID, gotMessageID, gotText string
	svc.sendMessageFunc = func(channelID, messageID, text string) (string, error) {
		gotChannelID, gotMessageID, gotText = channelID, messageID, text
		return "discord-msg-2", nil
	}

	handlers := svc.discordActionHandlers(project.ID, discordMarkerContext{UserID: "discord-user"}, nil)
	out, err := handlers["send_message"](ctx, []byte(`{"target":"discord:#ops","message":"hello ops"}`))
	if err != nil {
		t.Fatalf("send_message handler failed: %v", err)
	}
	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `discord-msg-2`) {
		t.Fatalf("unexpected send_message output: %s", out)
	}
	if gotChannelID != "thread-1" || gotMessageID != "" || gotText != "hello ops" {
		t.Fatalf("unexpected routed send channel=%q message=%q text=%q", gotChannelID, gotMessageID, gotText)
	}
}

func TestDiscordHandleMessageCreateIgnoresSelfAndBotMessages(t *testing.T) {
	svc, _, settingsRepo, _, _, _, _ := newDiscordServiceForTest(t)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, DiscordSettingBotUserID, "bot-1"); err != nil {
		t.Fatalf("set bot id: %v", err)
	}
	var processed int
	svc.processIncomingMessageFn = func(msg discordIncomingMessage) { processed++ }

	svc.handleMessageCreate(ctx, nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m1", ChannelID: "c1", Content: "hello", Author: &discordgo.User{ID: "bot-1"}}})
	svc.handleMessageCreate(ctx, nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m2", ChannelID: "c1", Content: "hello", Author: &discordgo.User{ID: "other-bot", Bot: true}}})

	if processed != 0 {
		t.Fatalf("expected self/bot messages ignored, processed=%d", processed)
	}
}

func TestDiscordHandleMessageCreateRequiresMentionInGuildAndStripsMention(t *testing.T) {
	svc, _, settingsRepo, _, _, _, _ := newDiscordServiceForTest(t)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, DiscordSettingBotUserID, "bot-1"); err != nil {
		t.Fatalf("set bot id: %v", err)
	}
	if err := settingsRepo.Set(ctx, DiscordSettingRequireMention, "true"); err != nil {
		t.Fatalf("set require mention: %v", err)
	}
	var got []discordIncomingMessage
	svc.processIncomingMessageFn = func(msg discordIncomingMessage) { got = append(got, msg) }

	svc.handleMessageCreate(ctx, nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m1", ChannelID: "c1", GuildID: "g1", Content: "hello", Author: &discordgo.User{ID: "u1"}}})
	if len(got) != 0 {
		t.Fatalf("expected unmentioned guild message ignored, got %#v", got)
	}

	svc.handleMessageCreate(ctx, nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m2", ChannelID: "c1", GuildID: "g1", Content: "<@bot-1> please help", Author: &discordgo.User{ID: "u1"}, Mentions: []*discordgo.User{{ID: "bot-1"}}}})
	if len(got) != 1 {
		t.Fatalf("expected mentioned guild message processed, got %#v", got)
	}
	if got[0].Text != "please help" {
		t.Fatalf("expected mention stripped, got %q", got[0].Text)
	}
}

func TestDiscordHandleMessageCreateAllowsFreeResponseChannelWithoutMention(t *testing.T) {
	svc, _, settingsRepo, _, _, _, _ := newDiscordServiceForTest(t)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, DiscordSettingBotUserID, "bot-1"); err != nil {
		t.Fatalf("set bot id: %v", err)
	}
	if err := settingsRepo.Set(ctx, DiscordSettingRequireMention, "true"); err != nil {
		t.Fatalf("set require mention: %v", err)
	}
	if err := settingsRepo.Set(ctx, DiscordSettingFreeResponseChannels, "c-free, c-other"); err != nil {
		t.Fatalf("set free channels: %v", err)
	}
	var got []discordIncomingMessage
	svc.processIncomingMessageFn = func(msg discordIncomingMessage) { got = append(got, msg) }

	svc.handleMessageCreate(ctx, nil, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "m1", ChannelID: "c-free", GuildID: "g1", Content: "no mention", Author: &discordgo.User{ID: "u1"}}})
	if len(got) != 1 || got[0].Text != "no mention" {
		t.Fatalf("expected free-response guild message processed, got %#v", got)
	}
}

func TestDiscordSendToTaskUsesConfiguredChannelTaskRunner(t *testing.T) {
	svc, db, _, projectRepo, taskRepo, _, _ := newDiscordServiceForTest(t)
	ctx := context.Background()
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	svc.llmConfigRepo = llmConfigRepo
	svc.execRepo = execRepo

	project := &models.Project{Name: "Discord Runner"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	agent := &models.LLMConfig{Name: "Test Agent", Provider: models.ProviderAnthropic, Model: "claude-test", AuthMethod: models.AuthMethodCLI, IsDefault: true}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create model config: %v", err)
	}
	task := models.Task{ProjectID: project.ID, Title: "Discord Followup", Category: models.CategoryBacklog, Status: models.StatusPending, Prompt: "start", AgentID: &agent.ID}
	if err := taskRepo.Create(ctx, &task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	var gotReq ChannelTaskRunRequest
	var called int
	svc.SetChannelTaskRunner(func(_ context.Context, req ChannelTaskRunRequest) {
		called++
		gotReq = req
	})

	result := svc.discordSendToTask(ctx, project.ID, []byte(`{"task_id":"`+task.ID+`","message":"continue from discord"}`), discordMarkerContext{ChannelID: "chan-1", ThreadID: "thread-1", MessageID: "msg-1", UserID: "user-1"})
	if called != 1 {
		t.Fatalf("expected channel task runner called once, got %d; result=%q", called, result)
	}
	if !strings.Contains(result, "started processing") {
		t.Fatalf("expected started response, got %q", result)
	}
	if gotReq.TaskID != task.ID || gotReq.ProjectID != project.ID || gotReq.Message != "continue from discord" || gotReq.Surface != chatcontrol.SurfaceDiscord {
		t.Fatalf("unexpected runner request: %#v", gotReq)
	}
	if gotReq.ReplyContext.Source != models.TaskOriginDiscord || gotReq.ReplyContext.DiscordChannelID != "chan-1" || gotReq.ReplyContext.DiscordThreadID != "thread-1" || gotReq.ReplyContext.DiscordMessageID != "msg-1" || gotReq.ReplyContext.DiscordUserID != "user-1" {
		t.Fatalf("unexpected reply context: %#v", gotReq.ReplyContext)
	}
}

func TestDiscordProcessIncomingMessagePassesReplyContextToSharedChatRunner(t *testing.T) {
	svc, db, settingsRepo, projectRepo, taskRepo, authRepo, discordTaskContextRepo := newDiscordServiceForTest(t)
	ctx := context.Background()
	project := &models.Project{Name: "Discord Chat", IsDefault: true}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := authRepo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: project.ID, DiscordUserID: "user-1", DisplayName: "User", AddedBy: "test"}); err != nil {
		t.Fatalf("authorize user: %v", err)
	}
	if err := settingsRepo.Set(ctx, DiscordSettingSendResponses, "true"); err != nil {
		t.Fatalf("set responses: %v", err)
	}
	agentRepo := repository.NewLLMConfigRepo(db)
	agent := &models.LLMConfig{Name: "test", Provider: models.ProviderOpenAI, Model: "gpt-4o", APIKey: "key", IsDefault: true}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	svc.llmConfigRepo = agentRepo
	svc.execRepo = execRepo
	svc.scheduleRepo = scheduleRepo
	svc.taskSvc = NewTaskService(taskRepo, repository.NewAttachmentRepo(db), nil)
	svc.llmSvc = NewLLMService(agentRepo, execRepo, taskRepo, projectRepo, scheduleRepo, repository.NewAttachmentRepo(db))
	svc.sendMessageFunc = func(channelID, messageID, text string) (string, error) { return "ack-1", nil }
	var got ChannelChatRunRequest
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) {
		got = req
	})

	svc.processIncomingMessage(discordIncomingMessage{ChannelID: "chan-1", ThreadID: "thread-1", MessageID: "msg-1", UserID: "user-1", Username: "User", Text: "hello", Source: "discord"})

	if got.Surface != chatcontrol.SurfaceDiscord {
		t.Fatalf("expected Discord surface, got %q", got.Surface)
	}
	if got.ReplyContext.Source != models.TaskOriginDiscord {
		t.Fatalf("expected Discord reply source, got %#v", got.ReplyContext)
	}
	if got.ReplyContext.DiscordChannelID != "thread-1" || got.ReplyContext.DiscordThreadID != "thread-1" || got.ReplyContext.DiscordMessageID != "msg-1" || got.ReplyContext.DiscordUserID != "user-1" {
		t.Fatalf("unexpected Discord reply context: %#v", got.ReplyContext)
	}
	stored, err := discordTaskContextRepo.GetByTaskID(ctx, got.TaskID)
	if err != nil {
		t.Fatalf("load stored context: %v", err)
	}
	if stored == nil || stored.DiscordChannelID != "thread-1" || stored.DiscordMessageID != "ack-1" {
		t.Fatalf("unexpected stored context: %#v", stored)
	}
	if task, err := taskRepo.GetByID(ctx, got.TaskID); err != nil || task == nil || task.CreatedVia != models.TaskOriginDiscord {
		t.Fatalf("expected Discord chat task, task=%#v err=%v", task, err)
	}
}

func TestDiscordRequiresMentionAllowsParentFreeResponseThread(t *testing.T) {
	svc, _, settingsRepo, _, _, _, _ := newDiscordServiceForTest(t)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, DiscordSettingRequireMention, "true"); err != nil {
		t.Fatalf("set require mention: %v", err)
	}
	if err := settingsRepo.Set(ctx, DiscordSettingFreeResponseChannels, "parent-1"); err != nil {
		t.Fatalf("set free channels: %v", err)
	}

	if svc.requiresMentionForMessage(ctx, discordIncomingMessage{ChannelID: "thread-1", ParentChannelID: "parent-1"}) {
		t.Fatalf("expected parent free-response thread to not require a mention")
	}
}

func TestDiscordSendTaskCompletionNotificationRoutesToContext(t *testing.T) {
	svc, _, _, projectRepo, taskRepo, _, discordTaskContextRepo := newDiscordServiceForTest(t)
	ctx := context.Background()
	project := &models.Project{Name: "Discord Completion"}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := models.Task{ProjectID: project.ID, Title: "Discord Task", Category: models.CategoryActive, Status: models.StatusCompleted, CreatedVia: models.TaskOriginDiscord, Prompt: "do it"}
	if err := taskRepo.Create(ctx, &task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := discordTaskContextRepo.Upsert(ctx, &models.DiscordTaskContext{TaskID: task.ID, DiscordChannelID: "chan-1", DiscordThreadID: "thread-1", DiscordMessageID: "msg-1", DiscordUserID: "user-1"}); err != nil {
		t.Fatalf("upsert context: %v", err)
	}
	var channelID, messageID, text string
	svc.sendMessageFunc = func(ch, msg, body string) (string, error) {
		channelID, messageID, text = ch, msg, body
		return "reply-1", nil
	}

	svc.SendTaskCompletionNotification(ctx, task, "finished", "")
	time.Sleep(10 * time.Millisecond)

	if channelID != "chan-1" || messageID != "msg-1" || !strings.Contains(text, "Discord Task") || !strings.Contains(text, "finished") {
		t.Fatalf("completion routed incorrectly channel=%q message=%q text=%q", channelID, messageID, text)
	}
}
