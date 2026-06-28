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

type fakeSlackOutbound struct{ channelID, threadTS, userID, text string }

func (f *fakeSlackOutbound) SendOutboundMessage(_ context.Context, channelID, threadTS, text string) SendMessageResult {
	f.channelID, f.threadTS, f.text = channelID, threadTS, text
	return SendMessageResult{OK: true, Platform: "slack", Target: formatResolvedMessageTarget("slack", channelID, threadTS), MessageID: "171.1"}
}

func (f *fakeSlackOutbound) SendOutboundDirectMessage(_ context.Context, userID, text string) SendMessageResult {
	f.userID, f.text = userID, text
	return SendMessageResult{OK: true, Platform: "slack", Target: formatResolvedMessageTarget("slack", userID, ""), MessageID: "171.2"}
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

type fakeDiscordOutbound struct{ channelID, threadID, userID, text string }

func (f *fakeDiscordOutbound) SendOutboundMessage(_ context.Context, channelID, threadID, text string) SendMessageResult {
	f.channelID, f.threadID, f.text = channelID, threadID, text
	return SendMessageResult{OK: true, Platform: "discord", Target: formatResolvedMessageTarget("discord", channelID, threadID), MessageID: "discord-msg-1"}
}

func (f *fakeDiscordOutbound) SendOutboundDirectMessage(_ context.Context, userID, text string) SendMessageResult {
	f.userID, f.text = userID, text
	return SendMessageResult{OK: true, Platform: "discord", Target: formatResolvedMessageTarget("discord", userID, ""), MessageID: "discord-dm-1"}
}

func setupChannelMessageRouterTest(t *testing.T) (context.Context, *repository.ChannelTargetRepo, *repository.SettingsRepo, *repository.SlackAuthRepo, *repository.DiscordAuthRepo, *models.Project, *ChannelMessageRouter, *fakeSlackOutbound, *fakeTelegramOutbound, *fakeEmailOutbound, *fakeDiscordOutbound) {
	t.Helper()
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	project := &models.Project{Name: "Router Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	targetRepo := repository.NewChannelTargetRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackAuthRepo := repository.NewSlackAuthRepo(db)
	discordAuthRepo := repository.NewDiscordAuthRepo(db)
	router := NewChannelMessageRouter(targetRepo, settingsRepo)
	slack := &fakeSlackOutbound{}
	telegram := &fakeTelegramOutbound{}
	email := &fakeEmailOutbound{}
	discord := &fakeDiscordOutbound{}
	router.SetSlackService(slack)
	router.SetTelegramService(telegram)
	router.SetEmailService(email)
	router.SetDiscordService(discord)
	router.SetSlackAuthStore(slackAuthRepo)
	router.SetDiscordAuthStore(discordAuthRepo)
	auditSeq := 0
	router.newID = func() string {
		auditSeq++
		return fmt.Sprintf("send-audit-id-%d", auditSeq)
	}
	return ctx, targetRepo, settingsRepo, slackAuthRepo, discordAuthRepo, project, router, slack, telegram, email, discord
}

func TestChannelMessageRouter_ListTargets(t *testing.T) {
	ctx, targetRepo, _, _, _, project, router, _, _, _, _ := setupChannelMessageRouterTest(t)
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "t1", ProjectID: project.ID, Platform: "slack", Name: "ops", TargetID: "C123", Home: true}))
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "t2", ProjectID: project.ID, Platform: "slack", TargetKind: "user", TargetID: "U0AQYLJR14Y"}))
	out, err := ExecuteSendMessageTool(ctx, router, project.ID, json.RawMessage(`{"action":"list"}`))
	require.NoError(t, err)
	require.Contains(t, out, `"ok":true`)
	require.Contains(t, out, `"name":"ops"`)
	require.Contains(t, out, `"target_kind":"channel"`, "list output must include target_kind for channel targets")
	require.Contains(t, out, `"target_kind":"user"`, "list output must include target_kind for user DM targets")
}

func TestChannelMessageRouter_ResolvesHomeTargets(t *testing.T) {
	ctx, targetRepo, _, _, _, project, router, slack, telegram, email, discord := setupChannelMessageRouterTest(t)
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

	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "discord-home", ProjectID: project.ID, Platform: "discord", TargetID: "123456789", Home: true}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "discord", Message: "hello discord"}).OK)
	require.Equal(t, "123456789", discord.channelID)
	require.Equal(t, "hello discord", discord.text)
}

func TestChannelMessageRouter_ResolvesNamedAndThreadTargets(t *testing.T) {
	ctx, targetRepo, _, _, _, project, router, slack, telegram, email, discord := setupChannelMessageRouterTest(t)
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

	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "discord-thread", ProjectID: project.ID, Platform: "discord", Name: "ops", TargetID: "987654321", ThreadID: "111222333"}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:#ops", Message: "discord thread"}).OK)
	require.Equal(t, "987654321", discord.channelID)
	require.Equal(t, "111222333", discord.threadID)
}

func TestChannelMessageRouter_SendDirectTargetDoesNotRequireSavedOrExplicitPolicy(t *testing.T) {
	ctx, targetRepo, _, _, _, project, router, _, _, email, discord := setupChannelMessageRouterTest(t)
	res := router.SendDirectTarget(ctx, project.ID, ChannelTarget{Platform: "email", TargetID: "Draft@Example.com", DefaultSubject: "Draft Subject"}, SendMessageRequest{Message: "draft test"})
	require.True(t, res.OK)
	require.Equal(t, "draft@example.com", email.to)
	require.Equal(t, "Draft Subject", email.subject)

	res = router.SendDirectTarget(ctx, project.ID, ChannelTarget{Platform: "discord", TargetID: "CDRAFT", ThreadID: "TDRAFT"}, SendMessageRequest{Message: "discord draft"})
	require.True(t, res.OK)
	require.Equal(t, "CDRAFT", discord.channelID)
	require.Equal(t, "TDRAFT", discord.threadID)

	targets, err := targetRepo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Empty(t, targets, "draft test must not save outbound targets")
	sends, err := targetRepo.ListSendsByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, sends, 2)
	require.True(t, sends[0].Success)
	require.True(t, sends[1].Success)
}

func TestChannelMessageRouter_ExplicitTargetsRequireSetting(t *testing.T) {
	ctx, targetRepo, settingsRepo, _, _, project, router, _, _, email, _ := setupChannelMessageRouterTest(t)
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

func TestChannelMessageRouter_ExplicitSlackTelegramAndDiscordChannelTargets(t *testing.T) {
	ctx, _, settingsRepo, _, _, project, router, slack, telegram, _, discord := setupChannelMessageRouterTest(t)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting, "true"))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:C123:169.1", Message: "slack"}).OK)
	require.Equal(t, "C123", slack.channelID)
	require.Equal(t, "169.1", slack.threadTS)
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "telegram:-100123:7", Message: "telegram"}).OK)
	require.Equal(t, int64(-100123), telegram.chatID)
	require.Equal(t, 7, telegram.threadID)

	bareDiscord := router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:123456789:987654321", Message: "discord"})
	require.False(t, bareDiscord.OK)
	require.Contains(t, bareDiscord.Error, "ambiguous")
	require.Empty(t, discord.channelID, "bare Discord snowflakes must not be sent as raw channel IDs")

	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:channel:123456789:987654321", Message: "discord"}).OK)
	require.Equal(t, "123456789", discord.channelID)
	require.Equal(t, "987654321", discord.threadID)
}

func TestChannelMessageRouter_AuthorizedUserIDsResolveToDirectMessages(t *testing.T) {
	ctx, _, _, slackAuthRepo, discordAuthRepo, project, router, slack, _, _, discord := setupChannelMessageRouterTest(t)

	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:U0AQYLJR14Y", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "Invalid slack target")

	require.NoError(t, slackAuthRepo.Create(ctx, &models.SlackAuthorizedUser{ProjectID: project.ID, SlackUserID: "U0AQYLJR14Y", DisplayName: "Slack User", AddedBy: "test"}))
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:U0AQYLJR14Y", Message: "hi"})
	require.True(t, res.OK, "authorized Slack user IDs should send as DMs: %#v", res)
	require.Equal(t, "U0AQYLJR14Y", slack.userID)
	require.Equal(t, "hi", slack.text)
	require.Empty(t, slack.channelID, "authorized user DM should not be treated as a Slack channel ID")

	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:1518288288572641398", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "not authorized")

	require.NoError(t, discordAuthRepo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: project.ID, DiscordUserID: "1518288288572641398", DisplayName: "Discord User", AddedBy: "test"}))
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:1518288288572641398", Message: "hi"})
	require.True(t, res.OK, "authorized Discord user IDs should send as DMs: %#v", res)
	require.Equal(t, "1518288288572641398", discord.userID)
	require.Equal(t, "hi", discord.text)
	require.Empty(t, discord.channelID, "authorized Discord user ID should not be treated as a channel ID")
}

func TestChannelMessageRouter_SavedDiscordTargetTakesPrecedenceOverAuthorizedUserID(t *testing.T) {
	ctx, targetRepo, _, _, discordAuthRepo, project, router, _, _, _, discord := setupChannelMessageRouterTest(t)
	require.NoError(t, discordAuthRepo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: project.ID, DiscordUserID: "1518288288572641398", DisplayName: "Discord User", AddedBy: "test"}))
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "discord-channel", ProjectID: project.ID, Platform: "discord", TargetID: "1518288288572641398"}))

	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:1518288288572641398", Message: "hi channel"})
	require.True(t, res.OK, "saved Discord channel targets should still send as channels: %#v", res)
	require.Equal(t, "1518288288572641398", discord.channelID)
	require.Equal(t, "hi channel", discord.text)
	require.Empty(t, discord.userID)
}

func TestChannelMessageRouter_DiscordUserAuthorizedInOtherProjectDoesNotFallThroughToChannel(t *testing.T) {
	ctx, _, settingsRepo, _, discordAuthRepo, project, router, _, _, _, discord := setupChannelMessageRouterTest(t)
	requestProjectID := "other-project"
	require.NoError(t, discordAuthRepo.Create(ctx, &models.DiscordAuthorizedUser{ProjectID: project.ID, DiscordUserID: "1518288288572641398", DisplayName: "Discord User", AddedBy: "test"}))
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+requestProjectID, "true"))

	res := router.Send(ctx, requestProjectID, SendMessageRequest{Target: "discord:1518288288572641398", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "not authorized")
	require.Empty(t, discord.channelID, "known Discord user IDs must not be sent as raw channel IDs when project authorization misses")
	require.Empty(t, discord.userID)
}

func TestChannelMessageRouter_BareDiscordSnowflakeDoesNotFallThroughToChannel(t *testing.T) {
	ctx, _, settingsRepo, _, _, project, router, _, _, _, discord := setupChannelMessageRouterTest(t)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+project.ID, "true"))

	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:1518288288572641398", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "not authorized")
	require.Empty(t, discord.channelID, "bare Discord user-shaped targets must not be sent as raw channel IDs")
	require.Empty(t, discord.userID)
}

func TestChannelMessageRouter_ValidationFailures(t *testing.T) {
	ctx, _, settingsRepo, _, _, project, router, _, _, _, _ := setupChannelMessageRouterTest(t)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "", Message: "x"}).OK)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:C123", Message: ""}).OK)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "unknown:123", Message: "x"}).OK)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting, "true"))
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "email:not-an-email", Message: "x"}).OK)
	require.False(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "telegram:-100:abc", Message: "x"}).OK)
}

// TestChannelMessageRouter_UserDMSyntaxDoesNotRequireAuthorizedUsers verifies that
// platform:user:<id> sends DMs without requiring the user to be in authorized users.
func TestChannelMessageRouter_UserDMSyntaxDoesNotRequireAuthorizedUsers(t *testing.T) {
	ctx, targetRepo, settingsRepo, _, _, project, router, slack, _, _, discord := setupChannelMessageRouterTest(t)

	// slack:user:U... is rejected when the target is not saved and explicit targets are off.
	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:user:U0AQYLJR14Y", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "No saved slack user DM target")
	require.Empty(t, slack.userID, "slack:user:<id> must not dispatch until saved or explicit targets are enabled")

	// Enabling explicit targets allows unsaved slack:user:<id> sends without authorized users.
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+project.ID, "true"))
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:user:U0AQYLJR14Y", Message: "hi"})
	require.True(t, res.OK, "slack:user:<id> with explicit targets enabled must send as DM: %#v", res)
	require.Equal(t, "U0AQYLJR14Y", slack.userID)
	require.Empty(t, slack.channelID, "slack:user:<id> must not be treated as a channel ID")

	// Reset explicit targets for discord test.
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+project.ID, "false"))

	// discord:user:<id> is rejected when the target is not saved and explicit targets are off.
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:user:1518288288572641398", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "No saved discord user DM target")
	require.Empty(t, discord.userID)

	// Enabling explicit targets allows unsaved discord:user:<id> sends without authorized users.
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+project.ID, "true"))
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:user:1518288288572641398", Message: "hi"})
	require.True(t, res.OK, "discord:user:<id> with explicit targets enabled must send as DM: %#v", res)
	require.Equal(t, "1518288288572641398", discord.userID)
	require.Empty(t, discord.channelID, "discord:user:<id> must not be treated as a channel ID")

	// Neither user appeared in inbound authorized users; verify no targets were saved.
	targets, err := targetRepo.ListByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Empty(t, targets, "explicit DM sends must not create saved targets")
}

// TestChannelMessageRouter_SavedUserKindTargetRoutesAsDM verifies that saved targets with
// target_kind='user' dispatch as direct messages without requiring the auth store.
func TestChannelMessageRouter_SavedUserKindTargetRoutesAsDM(t *testing.T) {
	ctx, targetRepo, _, _, _, project, router, slack, _, _, discord := setupChannelMessageRouterTest(t)

	// Save a Slack user-kind target (no auth store entry needed).
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "slack-user-dm", ProjectID: project.ID, Platform: "slack", TargetKind: "user", TargetID: "U0AQYLJR14Y"}))

	// Bare slack:U... reference finds saved user-kind target and dispatches as DM.
	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:U0AQYLJR14Y", Message: "hi"})
	require.True(t, res.OK, "saved user-kind Slack target must send as DM via bare reference: %#v", res)
	require.Equal(t, "U0AQYLJR14Y", slack.userID)
	require.Empty(t, slack.channelID)

	// Explicit slack:user:<id> reference also finds the saved user-kind target.
	slack.userID = ""
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:user:U0AQYLJR14Y", Message: "hi again"})
	require.True(t, res.OK, "saved user-kind Slack target must send as DM via user syntax: %#v", res)
	require.Equal(t, "U0AQYLJR14Y", slack.userID)

	// Save a Discord user-kind target (no auth store entry needed).
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "discord-user-dm", ProjectID: project.ID, Platform: "discord", TargetKind: "user", TargetID: "1518288288572641398"}))

	// Explicit discord:user:<id> reference finds saved user-kind target and dispatches as DM.
	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:user:1518288288572641398", Message: "hi"})
	require.True(t, res.OK, "saved user-kind Discord target must send as DM via user syntax: %#v", res)
	require.Equal(t, "1518288288572641398", discord.userID)
	require.Empty(t, discord.channelID)
}

// TestChannelMessageRouter_DiscordChannelSyntaxSendsToChannel verifies that
// discord:channel:<id> routes as a channel send, not a DM.
func TestChannelMessageRouter_DiscordChannelSyntaxSendsToChannel(t *testing.T) {
	ctx, _, settingsRepo, _, _, project, router, _, _, _, discord := setupChannelMessageRouterTest(t)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+project.ID, "true"))

	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:channel:123456789012345678", Message: "hello"})
	require.True(t, res.OK, "discord:channel:<id> must route as channel send: %#v", res)
	require.Equal(t, "123456789012345678", discord.channelID)
	require.Empty(t, discord.userID, "discord:channel:<id> must not dispatch as a DM")
}

// TestChannelMessageRouter_UserDMSyntaxInvalidIDRejected verifies that malformed user IDs
// with the platform:user:<id> syntax are rejected cleanly.
func TestChannelMessageRouter_UserDMSyntaxInvalidIDRejected(t *testing.T) {
	ctx, _, settingsRepo, _, _, project, router, slack, _, _, discord := setupChannelMessageRouterTest(t)
	require.NoError(t, settingsRepo.Set(ctx, SendMessageAllowExplicitTargetsSetting+":"+project.ID, "true"))

	res := router.Send(ctx, project.ID, SendMessageRequest{Target: "slack:user:", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "slack:user:<user_id>")
	require.Empty(t, slack.userID)

	res = router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:user:", Message: "hi"})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "discord:user:<user_id>")
	require.Empty(t, discord.userID)
}

// TestChannelMessageRouter_AuditRowIncludesTargetKind verifies that send audit rows
// record target_kind for both channel and user DM sends.
func TestChannelMessageRouter_AuditRowIncludesTargetKind(t *testing.T) {
	ctx, targetRepo, _, _, _, project, router, slack, _, _, discord := setupChannelMessageRouterTest(t)

	// Saved channel target send — audit row should record target_kind 'channel'.
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "slack-ch", ProjectID: project.ID, Platform: "slack", TargetKind: "channel", TargetID: "C123", Home: true}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "slack", Message: "channel msg"}).OK)
	require.Equal(t, "C123", slack.channelID)

	// Saved user-kind DM target send — audit row should record target_kind 'user'.
	require.NoError(t, targetRepo.Upsert(ctx, models.ChannelTarget{ID: "discord-dm", ProjectID: project.ID, Platform: "discord", TargetKind: "user", TargetID: "1518288288572641398"}))
	require.True(t, router.Send(ctx, project.ID, SendMessageRequest{Target: "discord:user:1518288288572641398", Message: "dm msg"}).OK)
	require.Equal(t, "1518288288572641398", discord.userID)

	sends, err := targetRepo.ListSendsByProject(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, sends, 2)
	var sawChannel, sawUser bool
	for _, s := range sends {
		if s.TargetID == "C123" {
			sawChannel = true
			require.Equal(t, "channel", s.TargetKind, "channel send audit must record target_kind=channel")
		}
		if s.TargetID == "1518288288572641398" {
			sawUser = true
			require.Equal(t, "user", s.TargetKind, "user DM send audit must record target_kind=user")
		}
	}
	require.True(t, sawChannel, "expected channel send audit row")
	require.True(t, sawUser, "expected user DM send audit row")
}
