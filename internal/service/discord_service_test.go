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
