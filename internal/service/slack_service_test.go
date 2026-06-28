package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/require"
)

var slackTestPNGBytes = func() []byte {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC")
	if err != nil {
		panic(err)
	}
	return data
}()

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSlackService_GetConnectionStatus(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientID, "cid"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientSecret, "secret"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingAppToken, "xapp-1"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingBotToken, "xoxb-1"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingTeamName, "OpenVibely"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	status, err := svc.GetConnectionStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.True(t, status.Connected)
	require.Equal(t, "OpenVibely", status.TeamName)
}

func TestSlackService_GetConnectionStatus_ManualOverrideSource(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientID, "cid"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientSecret, "secret"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingAppToken, "xapp-1"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingBotTokenSource, SlackBotTokenSourceManual))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingBotTokenOverride, "xoxb-manual-1"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	status, err := svc.GetConnectionStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.True(t, status.Connected)
	require.Equal(t, SlackBotTokenSourceManual, status.BotTokenSource)
	require.True(t, status.HasBotTokenOverride)
}

func TestSlackService_ConnectURLStoresState(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientID, "cid"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientSecret, "secret"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	u, err := svc.ConnectURL(context.Background(), "http://localhost:8080/channels/slack/callback")
	require.NoError(t, err)
	require.Contains(t, u, "oauth/v2/authorize")

	state, err := settingsRepo.Get(context.Background(), SlackSettingOAuthState)
	require.NoError(t, err)
	require.NotEmpty(t, state)
	require.Contains(t, u, "state=")
}

func TestSlackService_HandleOAuthCallback(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientID, "cid"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientSecret, "secret"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingOAuthState, "state-123"))

	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth.v2.access", r.URL.Path)
		_ = r.ParseForm()
		require.Equal(t, "cid", r.FormValue("client_id"))
		require.Equal(t, "secret", r.FormValue("client_secret"))
		fmt.Fprint(w, `{"ok":true,"access_token":"xoxb-123","bot_user_id":"U123","team":{"id":"T123","name":"OpenVibely"}}`)
	}))
	defer oauthSrv.Close()

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.oauthBaseURL = oauthSrv.URL

	err := svc.HandleOAuthCallback(context.Background(), "code-1", "state-123", "http://localhost:8080/channels/slack/callback")
	require.NoError(t, err)

	botToken, _ := settingsRepo.Get(context.Background(), SlackSettingBotToken)
	teamID, _ := settingsRepo.Get(context.Background(), SlackSettingTeamID)
	teamName, _ := settingsRepo.Get(context.Background(), SlackSettingTeamName)
	require.Equal(t, "xoxb-123", botToken)
	require.Equal(t, "T123", teamID)
	require.Equal(t, "OpenVibely", teamName)
}

func TestSlackService_HandleOAuthCallbackInvalidState(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientID, "cid"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientSecret, "secret"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingOAuthState, "expected-state"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := svc.HandleOAuthCallback(context.Background(), "code-1", "wrong-state", "http://localhost:8080/channels/slack/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid oauth state")
}

func TestSlackService_EventFilteringAcceptsDMAndAppMentions(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingBotUserID, "UBOT"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var received []slackIncomingMessage
	svc.processIncomingMessageFn = func(msg slackIncomingMessage) {
		received = append(received, msg)
	}

	svc.handleAppMention(context.Background(), "T1", slackevents.AppMentionEvent{
		User:      "U1",
		Channel:   "C1",
		Text:      "<@UBOT> hello from mention",
		TimeStamp: "1710000000.100000",
	})
	svc.handleMessageEvent(context.Background(), "T1", slackevents.MessageEvent{
		ChannelType: "im",
		User:        "U2",
		Channel:     "D1",
		Text:        "hello from dm",
		TimeStamp:   "1710000001.100000",
	})

	require.Len(t, received, 2)
	require.Equal(t, "T1", received[0].TeamID)
	require.Equal(t, "C1", received[0].ChannelID)
	require.Equal(t, "hello from mention", received[0].Text)
	require.Equal(t, "D1", received[1].ChannelID)
	require.Equal(t, "hello from dm", received[1].Text)
}

func TestSlackService_EventFilteringIgnoresBotSelfAndNonDMMessages(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingBotUserID, "UBOT"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	called := false
	svc.processIncomingMessageFn = func(msg slackIncomingMessage) {
		called = true
	}

	svc.handleAppMention(context.Background(), "T1", slackevents.AppMentionEvent{
		User:      "UBOT",
		Channel:   "C1",
		Text:      "<@UBOT> should ignore",
		TimeStamp: "1710000000.100000",
	})
	svc.handleAppMention(context.Background(), "T1", slackevents.AppMentionEvent{
		User:      "U1",
		Channel:   "C1",
		BotID:     "B1",
		Text:      "<@UBOT> should ignore",
		TimeStamp: "1710000000.100000",
	})
	svc.handleMessageEvent(context.Background(), "T1", slackevents.MessageEvent{
		ChannelType: "channel",
		User:        "U1",
		Channel:     "C2",
		Text:        "public channel message",
	})
	svc.handleMessageEvent(context.Background(), "T1", slackevents.MessageEvent{
		ChannelType: "im",
		User:        "U1",
		Channel:     "D1",
		SubType:     "message_changed",
		Text:        "edited",
	})
	svc.handleMessageEvent(context.Background(), "T1", slackevents.MessageEvent{
		ChannelType: "im",
		User:        "UBOT",
		Channel:     "D2",
		Text:        "bot self message",
	})

	require.False(t, called)
}

func TestSlackService_RuntimeCreateTaskTool_CreatedTasksGetSlackOriginAndContext(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)

	project := &models.Project{Name: "Slack Tool Project"}
	require.NoError(t, projectRepo.Create(ctx, project))

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	agentRepo := repository.NewAgentRepo(db)
	taskSvc.SetAgentRepo(agentRepo)
	agent := &models.Agent{Name: "Reviewer", Key: "reviewer", Enabled: true, SelectableAsPrimary: true}
	require.NoError(t, agentRepo.Create(ctx, agent))

	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetAgentRepo(agentRepo)

	collector := newChannelActionSummaryCollector()
	rt := svc.buildSlackActionToolRuntime(project.ID, slackMarkerContext{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U1",
	}, collector)
	require.NotNil(t, rt)

	output, handled, isErr, err := rt.Executor(ctx, "create_task", json.RawMessage(`{"title":"Slack Tool Created","prompt":"Do it","agent":"Reviewer"}`))
	require.True(t, handled)
	require.False(t, isErr)
	require.NoError(t, err)
	require.Contains(t, output, "Created 1 task(s):")

	tasks, err := taskRepo.ListByProject(ctx, project.ID, "")
	require.NoError(t, err)

	var created *models.Task
	for i := range tasks {
		if tasks[i].Title == "Slack Tool Created" {
			created = &tasks[i]
			break
		}
	}
	require.NotNil(t, created)
	require.Equal(t, models.TaskOriginSlack, created.CreatedVia)
	require.NotNil(t, created.AgentDefinitionID)
	require.Equal(t, agent.ID, *created.AgentDefinitionID)

	stc, err := slackTaskContextRepo.GetByTaskID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, stc)
	require.Equal(t, "C1", stc.SlackChannelID)
	require.Equal(t, "1710000000.100000", stc.SlackThreadTS)

	finalOutput := collector.appendToOutput("Done.")
	require.Contains(t, finalOutput, "[TASK_ID:")
}

func TestSlackService_RuntimeListAlertsTool_Handled(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	alertRepo := repository.NewAlertRepo(db)

	project := &models.Project{Name: "Slack Alerts Runtime"}
	require.NoError(t, projectRepo.Create(ctx, project))

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)

	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetAlertService(NewAlertService(alertRepo, nil))

	rt := svc.buildSlackActionToolRuntime(project.ID, slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}, nil)
	require.NotNil(t, rt)

	output, handled, isErr, err := rt.Executor(ctx, "list_alerts", json.RawMessage(`{}`))
	require.True(t, handled)
	require.False(t, isErr)
	require.NoError(t, err)
	require.Contains(t, output, "No alerts found")
}

func TestSlackService_RuntimeExecutorHandlesAllDefinedTools(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	alertRepo := repository.NewAlertRepo(db)

	project := &models.Project{Name: "Slack Full Runtime"}
	require.NoError(t, projectRepo.Create(ctx, project))

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)

	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetAlertService(NewAlertService(alertRepo, nil))

	rt := svc.buildSlackActionToolRuntime(project.ID, slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}, nil)
	require.NotNil(t, rt)

	defs := chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceSlack, true)
	require.NotEmpty(t, defs)

	for _, d := range defs {
		_, handled, _, _ := rt.Executor(ctx, d.Name, json.RawMessage(`{}`))
		require.Truef(t, handled, "tool should be handled by slack runtime executor: %s", d.Name)
	}

	handlers := svc.slackActionHandlers(project.ID, slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}, nil)
	require.NoError(t, chatcontrol.ValidateHandlerCoverage(models.ChatModeOrchestrate, chatcontrol.SurfaceSlack, true, handlers))
}

func TestSlackService_ProcessIncomingMessage_AuthorizationEnforced(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	slackAuthRepo := repository.NewSlackAuthRepo(db)

	project := &models.Project{Name: "Slack Auth Enforce"}
	require.NoError(t, projectRepo.Create(ctx, project))

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)

	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, slackAuthRepo)
	svc.setActiveProject(ctx, "T1", "U2", project.ID)

	require.NoError(t, slackAuthRepo.Create(ctx, &models.SlackAuthorizedUser{
		ProjectID:   project.ID,
		SlackUserID: "U1",
		DisplayName: "Allowed",
		AddedBy:     "test",
	}))

	var responses []string
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) {
		responses = append(responses, text)
		return "", nil
	}

	svc.processIncomingMessage(slackIncomingMessage{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U2",
		Text:      "hello",
		Source:    "slack",
	})
	require.NotEmpty(t, responses)
	require.Contains(t, responses[0], "not authorized")

	tasks, err := taskRepo.ListByProject(ctx, project.ID, "")
	require.NoError(t, err)
	require.Len(t, tasks, 0)
}

func TestSlackService_CheckAuthorization_FallsBackToAnyProject(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackAuthRepo := repository.NewSlackAuthRepo(db)

	projectA := &models.Project{Name: "Project A"}
	projectB := &models.Project{Name: "Project B"}
	require.NoError(t, projectRepo.Create(ctx, projectA))
	require.NoError(t, projectRepo.Create(ctx, projectB))

	require.NoError(t, slackAuthRepo.Create(ctx, &models.SlackAuthorizedUser{
		ProjectID:   projectA.ID,
		SlackUserID: "U_ALLOWED",
		DisplayName: "Allowed",
		AddedBy:     "test",
	}))

	svc := NewSlackService(settingsRepo, projectRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, slackAuthRepo)

	require.True(t, svc.checkAuthorization(ctx, projectB.ID, "U_ALLOWED"))
	require.False(t, svc.checkAuthorization(ctx, projectB.ID, "U_BLOCKED"))
}

func TestSlackService_CheckAuthorization_NoUsersConfiguredDenyAll(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackAuthRepo := repository.NewSlackAuthRepo(db)

	project := &models.Project{Name: "Project Empty Auth"}
	require.NoError(t, projectRepo.Create(ctx, project))

	svc := NewSlackService(settingsRepo, projectRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, slackAuthRepo)

	require.False(t, svc.checkAuthorization(ctx, project.ID, "U_ANY"))
	require.False(t, svc.checkAuthorization(ctx, "", "U_ANY"))
}

func TestSlackService_SendTaskCompletionNotification(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	settingsRepo := repository.NewSettingsRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)

	project := &models.Project{Name: "Slack Notify Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	task := &models.Task{
		ProjectID:  project.ID,
		Title:      "Slack Notify",
		Category:   models.CategoryActive,
		Status:     models.StatusCompleted,
		Prompt:     "done",
		CreatedVia: models.TaskOriginSlack,
	}
	require.NoError(t, taskRepo.Create(ctx, task))
	require.NoError(t, slackTaskContextRepo.Upsert(ctx, &models.SlackTaskContext{
		TaskID:         task.ID,
		SlackTeamID:    "T1",
		SlackChannelID: "C1",
		SlackThreadTS:  "1710000000.100000",
		SlackUserID:    "U1",
	}))

	svc := NewSlackService(settingsRepo, projectRepo, nil, taskRepo, nil, nil, nil, nil, nil, nil, slackTaskContextRepo, nil)
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingSendResponses, "true"))

	called := false
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) {
		called = true
		require.Equal(t, "C1", channelID)
		require.Equal(t, "1710000000.100000", threadTS)
		require.True(t, strings.Contains(text, "Task completed") || strings.Contains(text, "Task failed"))
		return "", nil
	}

	svc.SendTaskCompletionNotification(ctx, *task, "completed output", "")
	require.True(t, called)

	require.NoError(t, settingsRepo.Set(ctx, SlackSettingSendResponses, "false"))
	called = false
	svc.SendTaskCompletionNotification(ctx, *task, "completed output", "")
	require.False(t, called)
}

func TestSlackService_ProcessIncomingMessage_QueuesWhenChatActive(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	threadInputRepo := repository.NewThreadInputRepo(db)

	project := &models.Project{Name: "Slack Queue Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	activeTask := &models.Task{ProjectID: project.ID, Title: "active", Category: models.CategoryChat, Status: models.StatusRunning, Prompt: "active", AgentID: &agent.ID, CreatedVia: models.TaskOriginSlack}
	require.NoError(t, taskRepo.Create(ctx, activeTask))
	activeExec := &models.Execution{TaskID: activeTask.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "active"}
	require.NoError(t, execRepo.Create(ctx, activeExec))

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetThreadInputRepo(threadInputRepo)
	svc.setActiveProject(ctx, "T1", "U1", project.ID)

	var sent []string
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) {
		sent = append(sent, text)
		return "", nil
	}

	svc.processIncomingMessage(slackIncomingMessage{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1", Text: "follow up from slack", Source: "slack"})

	inputs, err := threadInputRepo.ListPendingForChat(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.Equal(t, models.ThreadInputModeQueued, inputs[0].InputMode)
	require.Equal(t, activeExec.ID, inputs[0].RunExecutionID)
	require.Equal(t, models.TaskOriginSlack, inputs[0].Source)
	require.Equal(t, "C1", inputs[0].SlackChannelID)
	require.Equal(t, "1710000000.100000", inputs[0].SlackThreadTS)
	require.Contains(t, strings.Join(sent, "\n"), "Queued")

	tasks, err := taskRepo.ListByProject(ctx, project.ID, "")
	require.NoError(t, err)
	require.Len(t, tasks, 1, "queued channel follow-up must not create a second chat task immediately")
}

func TestSlackService_ProcessIncomingMessage_UsesSharedChannelChatRunner(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)

	project := &models.Project{Name: "Slack Shared Runner Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.setActiveProject(ctx, "T1", "U1", project.ID)

	var got *ChannelChatRunRequest
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) {
		got = &req
	})

	svc.processIncomingMessage(slackIncomingMessage{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1", Text: "start from slack", Source: "slack"})

	require.NotNil(t, got, "Slack chat should use the shared steering-aware runner when wired")
	require.NotEmpty(t, got.ExecID)
	require.NotEmpty(t, got.TaskID)
	require.Equal(t, project.ID, got.ProjectID)
	require.Equal(t, "start from slack", got.Message)
	require.Equal(t, agent.ID, got.Agent.ID)
	require.Equal(t, chatcontrol.SurfaceSlack, got.Surface)
	createdExec, err := execRepo.GetByID(ctx, got.ExecID)
	require.NoError(t, err)
	require.NotNil(t, createdExec)
	require.Equal(t, models.ExecRunning, createdExec.Status)
	stc, err := slackTaskContextRepo.GetByTaskID(ctx, got.TaskID)
	require.NoError(t, err)
	require.NotNil(t, stc, "Slack shared-runner chat needs reply context for final response delivery")
	require.Equal(t, "T1", stc.SlackTeamID)
	require.Equal(t, "C1", stc.SlackChannelID)
	require.Equal(t, "1710000000.100000", stc.SlackThreadTS)
	require.Equal(t, "U1", stc.SlackUserID)
}

func TestSlackService_SendToTaskUsesSharedRunnerAndQueuesActiveTask(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	threadInputRepo := repository.NewThreadInputRepo(db)
	project := &models.Project{Name: "Slack Task Runner Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)
	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetThreadInputRepo(threadInputRepo)

	task := &models.Task{Title: "Slack task", Prompt: "work", Category: models.CategoryCompleted, Status: models.StatusCompleted, ProjectID: project.ID, AgentID: &agent.ID}
	require.NoError(t, taskRepo.Create(ctx, task))
	var runnerReq ChannelTaskRunRequest
	runnerCalled := false
	svc.SetChannelTaskRunner(func(_ context.Context, req ChannelTaskRunRequest) {
		runnerCalled = true
		runnerReq = req
	})
	payload := []byte(fmt.Sprintf(`{"task_id":"%s","message":"do more"}`, task.ID))
	markerCtx := slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}
	result := svc.slackSendToTask(ctx, project.ID, payload, markerCtx)
	require.Contains(t, result, "Sent message to task")
	require.True(t, runnerCalled, "Slack task follow-ups should use shared runner when wired")
	require.Equal(t, task.ID, runnerReq.TaskID)
	require.Equal(t, chatcontrol.SurfaceSlack, runnerReq.Surface)
	updatedTask, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.NotEqual(t, models.TaskOriginSlack, updatedTask.CreatedVia, "channel send_to_task must not rewrite target task origin")
	require.Equal(t, models.TaskOriginSlack, runnerReq.ReplyContext.Source)
	require.Equal(t, "C1", runnerReq.ReplyContext.SlackChannelID)
	require.Equal(t, "1710000000.100000", runnerReq.ReplyContext.SlackThreadTS)

	active := &models.Execution{TaskID: task.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "active", IsFollowup: true}
	require.NoError(t, execRepo.Create(ctx, active))
	result = svc.slackSendToTask(ctx, project.ID, payload, markerCtx)
	require.Contains(t, result, "Queued message to task")
	inputs, err := threadInputRepo.ListPendingForTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.Equal(t, "do more", inputs[0].Content)
	require.Equal(t, active.ID, inputs[0].RunExecutionID)
	require.Equal(t, models.TaskOriginSlack, inputs[0].Source)
	require.Equal(t, "C1", inputs[0].SlackChannelID)
	require.Equal(t, "1710000000.100000", inputs[0].SlackThreadTS)
	updatedTask, err = taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.NotEqual(t, models.TaskOriginSlack, updatedTask.CreatedVia, "queued channel follow-up must not hijack active task reply origin")
}

func TestSlackService_SendToTask_QueuesDuringStartingFirstTurnBeforeExecutionExists(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	threadInputRepo := repository.NewThreadInputRepo(db)
	project := &models.Project{Name: "Slack Starting First Turn Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)
	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetThreadInputRepo(threadInputRepo)
	runnerCalled := false
	svc.SetChannelTaskRunner(func(context.Context, ChannelTaskRunRequest) {
		runnerCalled = true
	})
	task := &models.Task{Title: "Slack starting task", Prompt: "tell me a story about a duck", Category: models.CategoryActive, Status: models.StatusPending, ProjectID: project.ID, AgentID: &agent.ID}
	require.NoError(t, taskRepo.Create(ctx, task))

	payload := []byte(fmt.Sprintf(`{"task_id":"%s","message":"1+1=?"}`, task.ID))
	markerCtx := slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}
	result := svc.slackSendToTask(ctx, project.ID, payload, markerCtx)
	require.Contains(t, result, "Queued message to task")
	require.False(t, runnerCalled, "pre-execution first-turn send must not start a follow-up runner")
	execs, err := execRepo.ListByTaskChronological(ctx, task.ID)
	require.NoError(t, err)
	require.Empty(t, execs)
	inputs, err := threadInputRepo.ListPendingForTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.Equal(t, "1+1=?", inputs[0].Content)
	require.Empty(t, inputs[0].RunExecutionID)
	require.Equal(t, models.TaskOriginSlack, inputs[0].Source)
	require.Equal(t, "C1", inputs[0].SlackChannelID)
}

func TestSlackService_CompleteExecution_FailurePromotesQueuedChat(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	project := &models.Project{Name: "Slack Failed Promotion Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agents, err := llmConfigRepo.List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, agents)
	task := &models.Task{ProjectID: project.ID, Title: "chat", Category: models.CategoryChat, Status: models.StatusRunning, Prompt: "prompt", CreatedVia: models.TaskOriginSlack}
	require.NoError(t, taskRepo.Create(ctx, task))
	exec := &models.Execution{TaskID: task.ID, AgentConfigID: agents[0].ID, Status: models.ExecRunning, PromptSent: "prompt"}
	require.NoError(t, execRepo.Create(ctx, exec))

	promotedProject := ""
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, nil, nil, nil, nil, nil, nil, nil)
	svc.queuedTurnPromoter = func(projectID string) { promotedProject = projectID }

	svc.completeExecution(ctx, exec.ID, task.ID, "", "boom", 0, 10)

	updatedExec, err := execRepo.GetByID(ctx, exec.ID)
	require.NoError(t, err)
	require.Equal(t, models.ExecFailed, updatedExec.Status)
	updatedTask, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusFailed, updatedTask.Status)
	require.Equal(t, project.ID, promotedProject, "failed chat executions should still promote queued follow-ups")
}

func TestSlackService_SendChatResponse_SendsChatTaskOutput(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	settingsRepo := repository.NewSettingsRepo(db)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(db)
	project := &models.Project{Name: "Slack Chat Response Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	task := &models.Task{ProjectID: project.ID, Title: "chat", Category: models.CategoryChat, Status: models.StatusCompleted, Prompt: "prompt", CreatedVia: models.TaskOriginSlack}
	require.NoError(t, taskRepo.Create(ctx, task))
	require.NoError(t, slackTaskContextRepo.Upsert(ctx, &models.SlackTaskContext{TaskID: task.ID, SlackTeamID: "T1", SlackChannelID: "C1", SlackThreadTS: "1710000000.100000", SlackUserID: "U1"}))

	svc := NewSlackService(settingsRepo, projectRepo, nil, taskRepo, nil, nil, nil, nil, nil, nil, slackTaskContextRepo, nil)
	var sent []string
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) {
		sent = append(sent, channelID+"|"+threadTS+"|"+text)
		return "", nil
	}

	svc.SendChatResponse(ctx, *task, "hello from queued slack", "")

	require.Equal(t, []string{"C1|1710000000.100000|hello from queued slack"}, sent)
}

// TestSlackService_GoalTools_SetGetClearPauseResume verifies that the Slack
// goal tool handlers call TaskGoalService and return JSON results, not errors.
// This is a regression test: before the fix, all goal tool handlers were stubs
// that always returned "task goal tools are unavailable on Slack".
func TestSlackService_GoalTools_SetGetClearPauseResume(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	attachmentRepo := repository.NewAttachmentRepo(db)
	taskGoalRepo := repository.NewTaskGoalRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	workerSvc := NewWorkerService(nil, 0, projectRepo)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	goalSvc := NewTaskGoalService(taskGoalRepo, taskRepo, nil)

	project := &models.Project{Name: "Slack Goal Test", RepoPath: "/tmp/test", IsDefault: true}
	require.NoError(t, projectRepo.Create(ctx, project))

	task := &models.Task{
		ProjectID: project.ID,
		Title:     "slack-goal-task",
		Prompt:    "do something",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	require.NoError(t, taskRepo.Create(ctx, task))

	svc := NewSlackService(settingsRepo, projectRepo, nil, taskRepo, nil, nil, taskSvc, nil, workerSvc, nil, nil, nil)
	svc.taskGoalSvc = goalSvc

	markerCtx := slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}
	handlers := svc.slackActionHandlers(project.ID, markerCtx, nil)

	taskIDJSON, err := json.Marshal(map[string]string{"task_id": task.ID, "goal": "All tests pass"})
	require.NoError(t, err)

	// set_task_goal
	out, err := handlers["set_task_goal"](ctx, taskIDJSON)
	require.NoError(t, err, "set_task_goal must not return an error on Slack")
	require.Contains(t, out, "ok")
	require.Contains(t, out, "All tests pass")

	// get_task_goal
	getInput, _ := json.Marshal(map[string]string{"task_id": task.ID})
	out, err = handlers["get_task_goal"](ctx, getInput)
	require.NoError(t, err, "get_task_goal must not return an error on Slack")
	require.Contains(t, out, "All tests pass")

	// pause_task_goal
	out, err = handlers["pause_task_goal"](ctx, getInput)
	require.NoError(t, err, "pause_task_goal must not return an error on Slack")
	require.Contains(t, out, "paused")

	// resume_task_goal
	out, err = handlers["resume_task_goal"](ctx, getInput)
	require.NoError(t, err, "resume_task_goal must not return an error on Slack")
	require.Contains(t, out, "active")

	// clear_task_goal
	out, err = handlers["clear_task_goal"](ctx, getInput)
	require.NoError(t, err, "clear_task_goal must not return an error on Slack")
	require.Contains(t, out, "ok")
}

// TestSlackService_GoalTools_UnavailableWithoutService verifies that when
// taskGoalSvc is nil (not yet wired), goal tools return a descriptive error
// rather than panicking.
func TestSlackService_GoalTools_UnavailableWithoutService(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	settingsRepo := repository.NewSettingsRepo(db)

	project := &models.Project{Name: "Slack Goal Nil Svc", RepoPath: "/tmp/test"}
	require.NoError(t, projectRepo.Create(ctx, project))

	svc := NewSlackService(settingsRepo, projectRepo, nil, taskRepo, nil, nil, nil, nil, nil, nil, nil, nil)
	// taskGoalSvc intentionally not set

	markerCtx := slackMarkerContext{TeamID: "T1", ChannelID: "C1"}
	handlers := svc.slackActionHandlers(project.ID, markerCtx, nil)

	input, _ := json.Marshal(map[string]string{"task_id": "any"})
	for _, name := range []string{"set_task_goal", "clear_task_goal", "get_task_goal", "pause_task_goal", "resume_task_goal", "mark_task_goal_achieved", "report_task_goal_blocked"} {
		_, err := handlers[name](ctx, input)
		require.Error(t, err, "expected error when taskGoalSvc is nil for handler %s", name)
		require.Contains(t, err.Error(), "task goal service unavailable", "handler %s should report service unavailable", name)
	}
}

// TestSlackService_GoalTools_MarkAchievedReportBlocked verifies that
// mark_task_goal_achieved and report_task_goal_blocked are callable from Slack
// (previously blocked by the unavailable stub).
func TestSlackService_GoalTools_MarkAchievedReportBlocked(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	attachmentRepo := repository.NewAttachmentRepo(db)
	taskGoalRepo := repository.NewTaskGoalRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	workerSvc := NewWorkerService(nil, 0, projectRepo)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	goalSvc := NewTaskGoalService(taskGoalRepo, taskRepo, nil)

	project := &models.Project{Name: "Slack Goal Achieved", RepoPath: "/tmp/test", IsDefault: true}
	require.NoError(t, projectRepo.Create(ctx, project))

	task := &models.Task{
		ProjectID: project.ID,
		Title:     "slack-mark-achieved-task",
		Prompt:    "do something",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
	}
	require.NoError(t, taskRepo.Create(ctx, task))

	goal, err := goalSvc.SetGoal(ctx, task.ID, "ship it", GoalOptions{Actor: "test"})
	require.NoError(t, err)

	svc := NewSlackService(settingsRepo, projectRepo, nil, taskRepo, nil, nil, taskSvc, nil, workerSvc, nil, nil, nil)
	svc.taskGoalSvc = goalSvc

	markerCtx := slackMarkerContext{TeamID: "T1", ChannelID: "C1", ThreadTS: "1710000000.100000", UserID: "U1"}
	handlers := svc.slackActionHandlers(project.ID, markerCtx, nil)

	achievedInput, _ := json.Marshal(map[string]string{
		"task_id": task.ID,
		"goal_id": goal.GoalID,
		"reason":  "done",
	})
	out, err := handlers["mark_task_goal_achieved"](ctx, achievedInput)
	require.NoError(t, err, "mark_task_goal_achieved must work on Slack when goal service is wired")
	require.Contains(t, out, "achieved")
}

func TestSlackService_SendOutboundMessage_PostsChannelAndThread(t *testing.T) {
	svc := NewSlackService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var gotChannel, gotThread, gotText string
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) {
		gotChannel, gotThread, gotText = channelID, threadTS, text
		return "1710000000.000001", nil
	}
	res := svc.SendOutboundMessage(context.Background(), "C123", "1690000000.000000", "hello")
	require.True(t, res.OK)
	require.Equal(t, "C123", gotChannel)
	require.Equal(t, "1690000000.000000", gotThread)
	require.Equal(t, "hello", gotText)
	require.Equal(t, "1710000000.000001", res.MessageID)
}

func TestSlackService_SendOutboundMessage_MissingTokenReturnsCleanError(t *testing.T) {
	svc := NewSlackService(repository.NewSettingsRepo(testutil.NewTestDB(t)), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	res := svc.SendOutboundMessage(context.Background(), "C123", "", "hello")
	require.False(t, res.OK)
	require.Contains(t, res.Error, "slack bot token is not configured")
}

func TestSlackService_ConnectURLIncludesFilesReadScope(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientID, "cid"))
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingClientSecret, "secret"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	authURL, err := svc.ConnectURL(context.Background(), "http://localhost/callback")
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	scopes := strings.Split(parsed.Query().Get("scope"), ",")
	require.Contains(t, scopes, "files:read")
}

func TestSlackService_EventFilteringPreservesFileMetadata(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	require.NoError(t, settingsRepo.Set(context.Background(), SlackSettingBotUserID, "UBOT"))

	svc := NewSlackService(settingsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var received []slackIncomingMessage
	svc.processIncomingMessageFn = func(msg slackIncomingMessage) { received = append(received, msg) }

	svc.handleAppMention(context.Background(), "T1", slackevents.AppMentionEvent{
		User:      "U1",
		Channel:   "C1",
		Text:      "<@UBOT> what is this?",
		TimeStamp: "1710000000.100000",
		Files: []slack.File{{
			ID:                 "F1",
			Name:               "screenshot.png",
			Mimetype:           "image/png",
			Size:               7,
			URLPrivateDownload: "https://files.slack.test/screenshot.png",
		}},
	})

	svc.handleMessageEvent(context.Background(), "T1", slackevents.MessageEvent{
		ChannelType: "im",
		SubType:     "file_share",
		User:        "U2",
		Channel:     "D1",
		Text:        "what is this?",
		TimeStamp:   "1710000001.100000",
		Message: &slack.Msg{Files: []slack.File{{
			ID:                 "F2",
			Name:               "dm-shot.png",
			Mimetype:           "image/png",
			Size:               8,
			URLPrivateDownload: "https://files.slack.test/dm-shot.png",
		}}},
	})

	require.Len(t, received, 2)
	require.Len(t, received[0].Files, 1)
	require.Equal(t, "F1", received[0].Files[0].ID)
	require.Equal(t, "screenshot.png", received[0].Files[0].Name)
	require.Len(t, received[1].Files, 1)
	require.Equal(t, "F2", received[1].Files[0].ID)
	require.Equal(t, "dm-shot.png", received[1].Files[0].Name)
}

func TestSlackService_ProcessIncomingMessage_DownloadsPersistsAndPassesImageAttachment(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo, taskRepo, llmConfigRepo, execRepo, scheduleRepo, attachmentRepo, chatAttachmentRepo, settingsRepo, slackUserProjectRepo, slackTaskContextRepo := newSlackAttachmentTestRepos(t, db)
	project := &models.Project{Name: "Slack Attachment Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenOverride, "xoxb-test"))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceManual))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(slackTestPNGBytes)
	}))
	defer fileServer.Close()

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	uploadRoot := useTempSlackUploadsDir(t, svc)
	svc.SetChatAttachmentRepo(chatAttachmentRepo)
	svc.setActiveProject(ctx, "T1", "U1", project.ID)

	var got *ChannelChatRunRequest
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) { got = &req })
	svc.processIncomingMessage(slackIncomingMessage{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U1",
		Text:      "what is this screenshot?",
		Source:    "slack",
		Files: []slackIncomingFile{{
			ID:                 "F1",
			Name:               "screenshot.png",
			Mimetype:           "image/png",
			Size:               7,
			URLPrivateDownload: fileServer.URL + "/screenshot.png",
		}},
	})

	require.NotNil(t, got)
	require.Len(t, got.ImageAttachments, 1)
	require.Equal(t, "screenshot.png", got.ImageAttachments[0].FileName)
	require.Equal(t, "image/png", got.ImageAttachments[0].MediaType)
	require.FileExists(t, got.ImageAttachments[0].FilePath)
	require.Contains(t, got.ImageAttachments[0].FilePath, filepath.Join(uploadRoot, "chat", got.ExecID))
	savedImage, err := os.ReadFile(got.ImageAttachments[0].FilePath)
	require.NoError(t, err)
	require.Equal(t, slackTestPNGBytes, savedImage)
	persisted, err := chatAttachmentRepo.ListByExecution(ctx, got.ExecID)
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	require.Equal(t, got.ImageAttachments[0].FilePath, persisted[0].FilePath)
}

func TestSlackService_ProcessIncomingMessage_FallsBackToSlackImageThumbnailWhenDownloadURLReturnsHTML(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo, taskRepo, llmConfigRepo, execRepo, scheduleRepo, attachmentRepo, chatAttachmentRepo, settingsRepo, slackUserProjectRepo, slackTaskContextRepo := newSlackAttachmentTestRepos(t, db)
	project := &models.Project{Name: "Slack Thumbnail Attachment Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenOverride, "xoxb-test"))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceManual))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/download.png":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>Slack file viewer</html>"))
		case "/thumb.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(slackTestPNGBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fileServer.Close()

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetUploadsDir(t.TempDir())
	svc.SetChatAttachmentRepo(chatAttachmentRepo)
	svc.setActiveProject(ctx, "T1", "U1", project.ID)

	var got *ChannelChatRunRequest
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) { got = &req })
	svc.processIncomingMessage(slackIncomingMessage{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U1",
		Text:      "what is this screenshot?",
		Source:    "slack",
		Files: []slackIncomingFile{{
			ID:                 "F1",
			Name:               "screenshot.png",
			Mimetype:           "image/png",
			Size:               len(slackTestPNGBytes),
			URLPrivateDownload: fileServer.URL + "/download.png",
			Thumb1024:          fileServer.URL + "/thumb.png",
		}},
	})

	require.NotNil(t, got)
	require.Len(t, got.ImageAttachments, 1)
	savedImage, err := os.ReadFile(got.ImageAttachments[0].FilePath)
	require.NoError(t, err)
	require.Equal(t, slackTestPNGBytes, savedImage)
}

func TestSlackService_ProcessIncomingMessage_PreservesBotTokenAcrossSlackFileRedirect(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo, taskRepo, llmConfigRepo, execRepo, scheduleRepo, attachmentRepo, chatAttachmentRepo, settingsRepo, slackUserProjectRepo, slackTaskContextRepo := newSlackAttachmentTestRepos(t, db)
	project := &models.Project{Name: "Slack Redirect Attachment Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenOverride, "xoxb-test"))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceManual))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	var cdnSawAuth string
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnSawAuth = r.Header.Get("Authorization")
		if cdnSawAuth != "Bearer xoxb-test" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>sign in to Slack</html>"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(slackTestPNGBytes)
	}))
	defer cdnServer.Close()
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		http.Redirect(w, r, cdnServer.URL+"/screenshot.png", http.StatusFound)
	}))
	defer fileServer.Close()

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetUploadsDir(t.TempDir())
	svc.SetChatAttachmentRepo(chatAttachmentRepo)
	svc.setActiveProject(ctx, "T1", "U1", project.ID)

	var got *ChannelChatRunRequest
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) { got = &req })
	svc.processIncomingMessage(slackIncomingMessage{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U1",
		Text:      "what is this screenshot?",
		Source:    "slack",
		Files: []slackIncomingFile{{
			ID:                 "F1",
			Name:               "screenshot.png",
			Mimetype:           "image/png",
			Size:               len(slackTestPNGBytes),
			URLPrivateDownload: fileServer.URL + "/screenshot.png",
		}},
	})

	require.Equal(t, "Bearer xoxb-test", cdnSawAuth)
	require.NotNil(t, got)
	require.Len(t, got.ImageAttachments, 1)
	savedImage, err := os.ReadFile(got.ImageAttachments[0].FilePath)
	require.NoError(t, err)
	require.Equal(t, slackTestPNGBytes, savedImage)
}

func TestSlackService_DownloadSlackPrivateFileRejectsUntrustedRedirect(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("untrusted redirect target should not be requested")
	}))
	defer targetServer.Close()
	targetURL, err := url.Parse(targetServer.URL)
	require.NoError(t, err)

	redirectingTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{targetServer.URL + "/steal"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	svc := NewSlackService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.httpClient = &http.Client{Transport: redirectingTransport}

	err = svc.downloadSlackPrivateFile(context.Background(), "xoxb-secret", "https://files.slack.com/files-pri/T-F/download.png", io.Discard)
	require.Error(t, err)
	require.Contains(t, err.Error(), "untrusted host")
	require.Contains(t, err.Error(), targetURL.Host)
}

func TestSlackService_ProcessIncomingMessage_RejectsInvalidSlackImageBytes(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo, taskRepo, llmConfigRepo, execRepo, scheduleRepo, attachmentRepo, _, settingsRepo, slackUserProjectRepo, slackTaskContextRepo := newSlackAttachmentTestRepos(t, db)
	project := &models.Project{Name: "Slack Invalid Attachment Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenOverride, "xoxb-test"))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceManual))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer fileServer.Close()

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	svc.SetUploadsDir(t.TempDir())
	svc.setActiveProject(ctx, "T1", "U1", project.ID)
	var slackReply string
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) {
		slackReply = text
		return "", nil
	}
	runnerCalled := false
	svc.SetChannelChatRunner(func(_ context.Context, req ChannelChatRunRequest) { runnerCalled = true })

	svc.processIncomingMessage(slackIncomingMessage{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U1",
		Text:      "what is this screenshot?",
		Source:    "slack",
		Files: []slackIncomingFile{{
			ID:                 "F1",
			Name:               "screenshot.png",
			Mimetype:           "image/png",
			Size:               24,
			URLPrivateDownload: fileServer.URL + "/screenshot.png",
		}},
	})

	require.False(t, runnerCalled)
	require.Contains(t, slackReply, "Failed to process attachment")
	require.Contains(t, slackReply, "not a valid supported image")
}

func TestSlackService_SetUploadsDir_NormalizesConfiguredRoot(t *testing.T) {
	svc := NewSlackService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	relativeRoot := filepath.Join(".", "configured-slack-uploads")
	expected, err := filepath.Abs(relativeRoot)
	require.NoError(t, err)
	svc.SetUploadsDir(relativeRoot)
	require.Equal(t, expected, svc.uploadsDir)
}

func TestSlackService_ProcessIncomingMessage_QueuesSlackFilesInAttachmentSession(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo, taskRepo, llmConfigRepo, execRepo, scheduleRepo, attachmentRepo, _, settingsRepo, slackUserProjectRepo, slackTaskContextRepo := newSlackAttachmentTestRepos(t, db)
	threadInputRepo := repository.NewThreadInputRepo(db)
	project := &models.Project{Name: "Slack Queued Attachment Project"}
	require.NoError(t, projectRepo.Create(ctx, project))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenOverride, "xoxb-test"))
	require.NoError(t, settingsRepo.Set(ctx, SlackSettingBotTokenSource, SlackBotTokenSourceManual))
	agent, err := llmConfigRepo.GetDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)
	activeTask := &models.Task{ProjectID: project.ID, Title: "active", Category: models.CategoryChat, Status: models.StatusRunning, Prompt: "active", AgentID: &agent.ID, CreatedVia: models.TaskOriginSlack}
	require.NoError(t, taskRepo.Create(ctx, activeTask))
	activeExec := &models.Execution{TaskID: activeTask.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "active"}
	require.NoError(t, execRepo.Create(ctx, activeExec))

	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(slackTestPNGBytes)
	}))
	defer fileServer.Close()

	llmSvc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	workerSvc := NewWorkerService(llmSvc, 0, nil)
	taskSvc := NewTaskService(taskRepo, attachmentRepo, workerSvc)
	svc := NewSlackService(settingsRepo, projectRepo, llmConfigRepo, taskRepo, execRepo, scheduleRepo, taskSvc, llmSvc, workerSvc, slackUserProjectRepo, slackTaskContextRepo, nil)
	uploadRoot := useTempSlackUploadsDir(t, svc)
	svc.SetThreadInputRepo(threadInputRepo)
	svc.setActiveProject(ctx, "T1", "U1", project.ID)
	svc.postMessageFn = func(channelID, threadTS, text string) (string, error) { return "", nil }

	svc.processIncomingMessage(slackIncomingMessage{
		TeamID:    "T1",
		ChannelID: "C1",
		ThreadTS:  "1710000000.100000",
		UserID:    "U1",
		Text:      "what is this screenshot?",
		Source:    "slack",
		Files: []slackIncomingFile{{
			ID:                 "F1",
			Name:               "queued.png",
			Mimetype:           "image/png",
			Size:               10,
			URLPrivateDownload: fileServer.URL + "/queued.png",
		}},
	})

	inputs, err := threadInputRepo.ListPendingForChat(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, inputs, 1)
	require.NotEmpty(t, inputs[0].AttachmentSessionID)
	require.Equal(t, models.TaskOriginSlack, inputs[0].Source)
	entries, err := os.ReadDir(filepath.Join(uploadRoot, "chat", "pending", inputs[0].AttachmentSessionID))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	staged, err := os.ReadFile(filepath.Join(uploadRoot, "chat", "pending", inputs[0].AttachmentSessionID, entries[0].Name()))
	require.NoError(t, err)
	require.Equal(t, slackTestPNGBytes, staged)
}

func newSlackAttachmentTestRepos(t *testing.T, db interface{}) (*repository.ProjectRepo, *repository.TaskRepo, *repository.LLMConfigRepo, *repository.ExecutionRepo, *repository.ScheduleRepo, *repository.AttachmentRepo, *repository.ChatAttachmentRepo, *repository.SettingsRepo, *repository.SlackUserProjectRepo, *repository.SlackTaskContextRepo) {
	t.Helper()
	sqlDB, ok := db.(*sql.DB)
	require.True(t, ok)
	projectRepo := repository.NewProjectRepo(sqlDB)
	taskRepo := repository.NewTaskRepo(sqlDB, nil)
	llmConfigRepo := repository.NewLLMConfigRepo(sqlDB)
	execRepo := repository.NewExecutionRepo(sqlDB)
	scheduleRepo := repository.NewScheduleRepo(sqlDB)
	attachmentRepo := repository.NewAttachmentRepo(sqlDB)
	chatAttachmentRepo := repository.NewChatAttachmentRepo(sqlDB)
	settingsRepo := repository.NewSettingsRepo(sqlDB)
	slackUserProjectRepo := repository.NewSlackUserProjectRepo(sqlDB)
	slackTaskContextRepo := repository.NewSlackTaskContextRepo(sqlDB)
	return projectRepo, taskRepo, llmConfigRepo, execRepo, scheduleRepo, attachmentRepo, chatAttachmentRepo, settingsRepo, slackUserProjectRepo, slackTaskContextRepo
}

func useTempSlackUploadsDir(t *testing.T, svc *SlackService) string {
	t.Helper()
	dir := t.TempDir()
	svc.SetUploadsDir(dir)
	return dir
}
