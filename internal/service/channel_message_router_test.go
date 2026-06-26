package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/require"
)

type fakeSlackOutbound struct{ channelID, threadTS, text string }

func (f *fakeSlackOutbound) SendOutboundMessage(_ context.Context, channelID, threadTS, text string) SendMessageResult {
	f.channelID, f.threadTS, f.text = channelID, threadTS, text
	return SendMessageResult{OK: true, Platform: "slack", Target: formatResolvedMessageTarget("slack", channelID, threadTS), MessageID: "171.1"}
}

type fakeTelegramOutbound struct {
	chatID   int64
	threadID int
	text     string
}

func (f *fakeTelegramOutbound) SendOutboundMessage(_ context.Context, chatID int64, threadID int, text string) SendMessageResult {
	f.chatID, f.threadID, f.text = chatID, threadID, text
	return SendMessageResult{OK: true, Platform: "telegram", Target: formatResolvedMessageTarget("telegram", "-100123", threadIDString(threadID))}
}

type fakeEmailOutbound struct{ to, subject, body string }

func (f *fakeEmailOutbound) SendOutboundMessage(_ context.Context, to, subject, body string) SendMessageResult {
	f.to, f.subject, f.body = to, subject, body
	return SendMessageResult{OK: true, Platform: "email", Target: "email:" + to}
}

func setupChannelMessageRouterTest(t *testing.T) (context.Context, *repository.ChannelTargetRepo, *repository.SettingsRepo, *models.Project, *ChannelMessageRouter, *fakeSlackOutbound, *fakeTelegramOutbound, *fakeEmailOutbound) {
	t.Helper()
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "Router Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	targetRepo := repository.NewChannelTargetRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	router := NewChannelMessageRouter(targetRepo, settingsRepo)
	slack := &fakeSlackOutbound{}
	telegram := &fakeTelegramOutbound{}
	email := &fakeEmailOutbound{}
	router.SetSlackService(slack)
	router.SetTelegramService(telegram)
	router.SetEmailService(email)
	auditSeq := 0
	router.newID = func() string {
		auditSeq++
		return fmt.Sprintf("send-audit-id-%d", auditSeq)
	}
	return ctx, targetRepo, settingsRepo, project, router, slack, telegram, email
}

func TestChannelMessageRouter_ListTargets(t *testing.T) {
	ctx, targetRepo, _, project, router, _, _, _ := setupChannelMessageRouterTest(t)
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "t1", ProjectID: project.ID, Platform: "slack", Name: "ops", TargetID: "C123", Home: true}))
	out, err := ExecuteSendMessageTool(ctx, router, project.ID, json.RawMessage(`{"action":"list"}`))
	require.NoError(t, err)
	require.Contains(t, out, `"ok":true`)
	require.Contains(t, out, `"name":"ops"`)
}

func TestChannelMessageRouter_ResolvesHomeTargets(t *testing.T) {
	ctx, targetRepo, _, project, router, slack, telegram, email := setupChannelMessageRouterTest(t)
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "slack-home", ProjectID: project.ID, Platform: "slack", TargetID: "C123", Home: true}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack", Message: "hello slack"}).OK)
	require.Equal(t, "C123", slack.channelID)

	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "telegram-home", ProjectID: project.ID, Platform: "telegram", TargetID: "-100123", Home: true}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "telegram", Message: "hello telegram"}).OK)
	require.Equal(t, int64(-100123), telegram.chatID)

	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "email-home", ProjectID: project.ID, Platform: "email", TargetID: "person@example.com", Home: true, DefaultSubject: "Default Subject"}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "email", Message: "hello email"}).OK)
	require.Equal(t, "person@example.com", email.to)
	require.Equal(t, "Default Subject", email.subject)
}

func TestChannelMessageRouter_ResolvesNamedAndThreadTargets(t *testing.T) {
	ctx, targetRepo, _, project, router, slack, telegram, email := setupChannelMessageRouterTest(t)
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "slack-ops", ProjectID: project.ID, Platform: "slack", Name: "ops", TargetID: "COPS", ThreadID: "1690000000.000000"}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:#ops", Message: "thread msg"}).OK)
	require.Equal(t, "COPS", slack.channelID)
	require.Equal(t, "1690000000.000000", slack.threadTS)

	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "tg-topic", ProjectID: project.ID, Platform: "telegram", Name: "topic", TargetID: "-100123", ThreadID: "42"}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "telegram:#topic", Message: "topic msg"}).OK)
	require.Equal(t, 42, telegram.threadID)

	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "email-ops", ProjectID: project.ID, Platform: "email", Name: "ops", TargetID: "ops@example.com"}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "email:#ops", Message: "body", Subject: "Subject"}).OK)
	require.Equal(t, "ops@example.com", email.to)
	require.Equal(t, "Subject", email.subject)
}

func TestChannelMessageRouter_ExplicitTargetsRequireSetting(t *testing.T) {
	ctx, targetRepo, settingsRepo, project, router, _, _, email := setupChannelMessageRouterTest(t)
	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "email:Person@Example.com", Message: "body"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "not saved")

	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting, "true"))
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "email:Person@Example.com", Message: "body"})
	require.True(t, res.OK)
	require.Equal(t, "person@example.com", email.to)

	sends, err := targetRepo.ListSendsByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, sends, 2)
	require.True(t, sends[0].Success)
	require.False(t, sends[1].Success)
}

func TestChannelMessageRouter_ExplicitSlackAndTelegramTargets(t *testing.T) {
	ctx, _, settingsRepo, project, router, slack, telegram, _ := setupChannelMessageRouterTest(t)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting, "true"))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:C123:169.1", Message: "slack"}).OK)
	require.Equal(t, "C123", slack.channelID)
	require.Equal(t, "169.1", slack.threadTS)
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "telegram:-100123:7", Message: "telegram"}).OK)
	require.Equal(t, int64(-100123), telegram.chatID)
	require.Equal(t, 7, telegram.threadID)
}

func TestChannelMessageRouter_ValidationFailures(t *testing.T) {
	ctx, _, settingsRepo, project, router, _, _, _ := setupChannelMessageRouterTest(t)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "", Message: "x"}).OK)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:C123", Message: ""}).OK)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:123", Message: "x"}).OK)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting, "true"))
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "email:not-an-email", Message: "x"}).OK)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "telegram:-100:abc", Message: "x"}).OK)
}
