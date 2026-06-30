package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleTelegramTest_NotRunning tests the error feedback HTML
func TestHandleTelegramTest_NotRunning(t *testing.T) {
	e := echo.New()
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	// telegramService is nil by default (not running)

	req := httptest.NewRequest(http.MethodPost, "/channels/telegram/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.handleTelegramTest(c)
	require.NoError(t, err)

	// Verify handler returns 200 OK with error HTML
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Verify error feedback HTML contains:
	// - Error styling (text-error class)
	// - Error icon (X SVG path)
	// - Error message
	// - No auto-dismiss script (errors should persist)
	assert.Contains(t, body, "text-error")
	assert.Contains(t, body, "Connection failed")
	assert.Contains(t, body, "Bot is not running")
	assert.Contains(t, body, "M6 18L18 6M6 6l12 12") // X SVG path
	assert.NotContains(t, body, "setTimeout")        // should NOT auto-dismiss
}

func assertChannelsRefreshTrigger(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, channelsRefreshTrigger, rec.Header().Get("HX-Trigger"))
	assert.Empty(t, rec.Header().Get("HX-Refresh"))
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestHandleTelegramSaveHTMXTriggersChannelsRefresh(t *testing.T) {
	h, e, _ := setupTestHandler(t)

	h.telegramService = &service.TelegramService{}
	origUpdateTelegramServiceToken := updateTelegramServiceToken
	t.Cleanup(func() { updateTelegramServiceToken = origUpdateTelegramServiceToken })
	updateTelegramServiceToken = func(svc *service.TelegramService, token string) error {
		return nil
	}

	form := url.Values{}
	form.Set("token", "test-token")
	form.Set("telegram_rich_messages_v2", "true")

	rec := htmxPost(e, "/channels/telegram", form)
	assert.Equal(t, http.StatusOK, rec.Code)
	assertChannelsRefreshTrigger(t, rec)

	token, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingBotToken)
	require.NoError(t, err)
	assert.Equal(t, "test-token", token)
	richMessages, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingRichMessagesV2)
	require.NoError(t, err)
	assert.Equal(t, "true", richMessages)
}

func TestChannelCoreHTMXMutationsTriggerInPlaceChannelsRefresh(t *testing.T) {
	h, e, _ := setupTestHandler(t)

	h.telegramService = &service.TelegramService{}
	origUpdateTelegramServiceToken := updateTelegramServiceToken
	t.Cleanup(func() { updateTelegramServiceToken = origUpdateTelegramServiceToken })
	updateTelegramServiceToken = func(svc *service.TelegramService, token string) error { return nil }

	base := func() url.Values {
		return url.Values{}
	}
	tests := []struct {
		name string
		path string
		form func() url.Values
	}{
		{
			name: "telegram save",
			path: "/channels/telegram",
			form: func() url.Values {
				form := base()
				form.Set("token", "test-token")
				form.Set("telegram_rich_messages_v2", "true")
				return form
			},
		},
		{
			name: "telegram remove",
			path: "/channels/telegram/remove",
			form: base,
		},
		{
			name: "github configure",
			path: "/channels/github/configure",
			form: func() url.Values {
				form := base()
				form.Set("github_auth_mode", service.GitHubAuthModePAT)
				form.Set("github_pat", "ghp_test_token")
				return form
			},
		},
		{
			name: "github remove",
			path: "/channels/github/remove",
			form: base,
		},
		{
			name: "slack configure",
			path: "/channels/slack/configure",
			form: func() url.Values {
				form := base()
				form.Set("slack_client_id", "cid")
				form.Set("slack_client_secret", "secret")
				form.Set("slack_app_token", "xapp-token")
				form.Set("slack_bot_token_mode", service.SlackBotTokenSourceOAuth)
				return form
			},
		},
		{
			name: "slack remove",
			path: "/channels/slack/remove",
			form: base,
		},
		{
			name: "discord configure",
			path: "/channels/discord/configure",
			form: func() url.Values {
				form := base()
				form.Set("discord_bot_token", "discord-token")
				return form
			},
		},
		{
			name: "discord remove",
			path: "/channels/discord/remove",
			form: base,
		},
		{
			name: "email configure",
			path: "/channels/email/configure",
			form: func() url.Values {
				form := base()
				form.Set("email_provider", service.EmailProviderCustom)
				form.Set("email_address", "bot@example.com")
				form.Set("email_password", "app-password")
				form.Set("email_imap_host", "imap.example.com")
				form.Set("email_imap_port", "993")
				form.Set("email_smtp_host", "smtp.example.com")
				form.Set("email_smtp_port", "587")
				return form
			},
		},
		{
			name: "email remove",
			path: "/channels/email/remove",
			form: base,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := htmxPost(e, tc.path, tc.form())
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assertChannelsRefreshTrigger(t, rec)
		})
	}
}

func TestHandleTelegramSaveStoresRichMessagesFalseWhenUnchecked(t *testing.T) {
	h, e, _ := setupTestHandler(t)

	h.telegramService = &service.TelegramService{}
	origUpdateTelegramServiceToken := updateTelegramServiceToken
	t.Cleanup(func() { updateTelegramServiceToken = origUpdateTelegramServiceToken })
	updateTelegramServiceToken = func(svc *service.TelegramService, token string) error { return nil }

	form := url.Values{}
	form.Set("token", "test-token")

	rec := htmxPost(e, "/channels/telegram", form)
	assert.Equal(t, http.StatusOK, rec.Code)
	richMessages, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingRichMessagesV2)
	require.NoError(t, err)
	assert.Equal(t, "false", richMessages)
}

func TestHandleTelegramSaveErrorDoesNotRefreshOrRedirect(t *testing.T) {
	h, e, _ := setupTestHandler(t)

	h.telegramService = &service.TelegramService{}
	origUpdateTelegramServiceToken := updateTelegramServiceToken
	t.Cleanup(func() { updateTelegramServiceToken = origUpdateTelegramServiceToken })
	updateTelegramServiceToken = func(svc *service.TelegramService, token string) error {
		return assert.AnError
	}

	form := url.Values{}
	form.Set("token", "test-token")

	rec := htmxPost(e, "/channels/telegram", form)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Empty(t, rec.Header().Get("HX-Refresh"))
	assert.Empty(t, rec.Header().Get("Location"))

	token, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingBotToken)
	require.NoError(t, err)
	assert.Equal(t, "test-token", token)
}

func TestHandleTelegramSaveNewServiceWiresSharedRunner(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	h.SetAgentRepo(repository.NewAgentRepo(db))

	createdSvc := &service.TelegramService{}
	origNewTelegramService := newTelegramService
	t.Cleanup(func() { newTelegramService = origNewTelegramService })
	newTelegramService = func(
		token string,
		taskSvc *service.TaskService,
		projectRepo *repository.ProjectRepo,
		llmConfigRepo *repository.LLMConfigRepo,
		taskRepo *repository.TaskRepo,
		execRepo *repository.ExecutionRepo,
		scheduleRepo *repository.ScheduleRepo,
		chatAttachmentRepo *repository.ChatAttachmentRepo,
		llmSvc *service.LLMService,
		workerSvc *service.WorkerService,
	) (*service.TelegramService, error) {
		return createdSvc, nil
	}

	form := url.Values{}
	form.Set("token", "test-token")

	rec := htmxPost(e, "/channels/telegram", form)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Same(t, createdSvc, h.telegramService)
	assert.True(t, createdSvc.HasChannelChatRunner(), "settings-created Telegram service must use shared steering-aware runner")
	assert.True(t, createdSvc.HasAgentRepo(), "settings-created Telegram service must expose agent definitions in chat context")
}

func TestHandleTelegramSaveNonHTMXRedirectsToChannels(t *testing.T) {
	h, e, _ := setupTestHandler(t)

	h.telegramService = &service.TelegramService{}
	origUpdateTelegramServiceToken := updateTelegramServiceToken
	t.Cleanup(func() { updateTelegramServiceToken = origUpdateTelegramServiceToken })
	updateTelegramServiceToken = func(svc *service.TelegramService, token string) error {
		return nil
	}

	form := url.Values{}
	form.Set("token", "")

	rec := postForm(e, "/channels/telegram", form)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/channels", rec.Header().Get("Location"))

	token, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingBotToken)
	require.NoError(t, err)
	assert.Equal(t, "", token)
}

func TestHandleTelegramRemoveHTMXTriggersChannelsRefreshAndClearsSettings(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	require.NoError(t, h.settingsRepo.Set(context.Background(), service.TelegramSettingBotToken, "test-token"))
	require.NoError(t, h.settingsRepo.Set(context.Background(), service.TelegramSettingSendResponses, "true"))
	require.NoError(t, h.settingsRepo.Set(context.Background(), service.TelegramSettingRichMessagesV2, "false"))

	h.telegramService = &service.TelegramService{}

	req := httptest.NewRequest(http.MethodPost, "/channels/telegram/remove", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assertChannelsRefreshTrigger(t, rec)

	token, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingBotToken)
	require.NoError(t, err)
	assert.Equal(t, "", token)
	sendResponses, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingSendResponses)
	require.NoError(t, err)
	assert.Equal(t, "", sendResponses)
	richMessages, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingRichMessagesV2)
	require.NoError(t, err)
	assert.Equal(t, "", richMessages)
}

func TestHandleTelegramRemoveNonHTMXRedirectsToChannelsAndClearsSettings(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	require.NoError(t, h.settingsRepo.Set(context.Background(), service.TelegramSettingBotToken, "test-token"))
	require.NoError(t, h.settingsRepo.Set(context.Background(), service.TelegramSettingSendResponses, "true"))
	require.NoError(t, h.settingsRepo.Set(context.Background(), service.TelegramSettingRichMessagesV2, "false"))

	h.telegramService = &service.TelegramService{}

	req := httptest.NewRequest(http.MethodPost, "/channels/telegram/remove", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/channels", rec.Header().Get("Location"))

	token, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingBotToken)
	require.NoError(t, err)
	assert.Equal(t, "", token)
	sendResponses, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingSendResponses)
	require.NoError(t, err)
	assert.Equal(t, "", sendResponses)
	richMessages, err := h.settingsRepo.Get(context.Background(), service.TelegramSettingRichMessagesV2)
	require.NoError(t, err)
	assert.Equal(t, "", richMessages)
}

func TestHandleTelegramRemoveMissingSettingsRepoReturnsError(t *testing.T) {
	e := echo.New()
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/channels/telegram/remove", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.handleTelegramRemove(c)
	require.Error(t, err)

	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
}
