package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestRunChannelTaskThreadSendStartsDirectFollowupWithReplyContext(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	project := &models.Project{Name: "Task Thread Channel"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent := &models.LLMConfig{Name: "Task Agent", Provider: models.ProviderTest, Model: "test", IsDefault: true}
	require.NoError(t, llmConfigRepo.Create(ctx, agent))
	task := &models.Task{ProjectID: project.ID, Title: "Follow target", Prompt: "prompt", Category: models.CategoryBacklog, Status: models.StatusPending, Priority: 2, AgentID: &agent.ID}
	require.NoError(t, taskRepo.Create(ctx, task))
	var runReq ChannelTaskRunRequest
	result := runChannelTaskThreadSend(ctx, task, channelTaskThreadSendOptions{
		Platform:          "test",
		ProjectID:         project.ID,
		Message:           "continue",
		Source:            models.TaskOriginTelegram,
		Surface:           "test-surface",
		ReplyContext:      ChannelReplyContext{Source: models.TaskOriginTelegram, TelegramChatID: 123},
		TaskRepo:          taskRepo,
		ExecRepo:          execRepo,
		LLMConfigRepo:     llmConfigRepo,
		ChannelTaskRunner: func(_ context.Context, req ChannelTaskRunRequest) { runReq = req },
		CompleteExecution: func(context.Context, string, string, string, string, int, int64) {},
	})
	require.Contains(t, result, "Sent message to task")
	require.Equal(t, task.ID, runReq.TaskID)
	require.Equal(t, "continue", runReq.Message)
	require.Equal(t, int64(123), runReq.ReplyContext.TelegramChatID)
	updated, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusQueued, updated.Status)
	require.Equal(t, models.CategoryActive, updated.Category)
}

func TestBuildChannelGoalActionHandlersSetGoalUsesSharedTaskResolution(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	project := &models.Project{Name: "Goal Actions"}
	require.NoError(t, projectRepo.Create(ctx, project))
	task := &models.Task{ProjectID: project.ID, Title: "Goal target", Prompt: "prompt", Category: models.CategoryBacklog, Status: models.StatusPending, Priority: 2}
	require.NoError(t, taskRepo.Create(ctx, task))
	handlers := buildChannelGoalActionHandlers(channelGoalActionHandlerOptions{ProjectID: project.ID, TaskRepo: taskRepo, TaskGoalSvc: NewTaskGoalService(repository.NewTaskGoalRepo(db), taskRepo, nil)})
	payload, err := json.Marshal(channelGoalToolInput{Title: "Goal target", Goal: "Finish the shared refactor"})
	require.NoError(t, err)

	result, err := handlers["set_task_goal"](ctx, payload)
	require.NoError(t, err)
	require.Contains(t, result, "Finish the shared refactor")
	require.Contains(t, result, task.ID)
}

func TestBuildChannelUtilityActionHandlersScheduleTaskAndModifyUseSharedLogic(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	project := &models.Project{Name: "Utility Actions"}
	require.NoError(t, projectRepo.Create(ctx, project))
	task := &models.Task{ProjectID: project.ID, Title: "Scheduled target", Prompt: "prompt", Category: models.CategoryBacklog, Status: models.StatusCompleted, Priority: 2}
	require.NoError(t, taskRepo.Create(ctx, task))

	handlers := buildChannelUtilityActionHandlers(channelUtilityActionHandlerOptions{ProjectID: project.ID, TaskRepo: taskRepo, ScheduleRepo: scheduleRepo})
	scheduleOut, err := handlers["schedule_task"](ctx, json.RawMessage(`{"title":"Scheduled target","time":"09:30","repeat":"weekly","days":["mon"],"interval":2}`))
	require.NoError(t, err)
	require.Contains(t, scheduleOut, "Scheduled task")
	require.Contains(t, scheduleOut, "every 2 weeks on mon")
	updatedTask, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, models.CategoryScheduled, updatedTask.Category)
	require.Equal(t, models.StatusPending, updatedTask.Status)
	schedules, err := scheduleRepo.ListByTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, schedules, 1)

	modifyOut, err := handlers["modify_schedule"](ctx, json.RawMessage(`{"schedule_id":"`+schedules[0].ID+`","time":"10:45","enabled":false}`))
	require.NoError(t, err)
	require.Contains(t, modifyOut, "Updated schedule")
	require.Contains(t, modifyOut, "time→10:45")
	require.Contains(t, modifyOut, "enabled→false")
}

func TestBuildChannelUtilityActionHandlersPersonalityModelAndProjectInfo(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	settingsRepo := repository.NewSettingsRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	project := &models.Project{Name: "Info Project", Description: "Details"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent := &models.LLMConfig{Name: "Default Model", Provider: models.ProviderTest, Model: "test", IsDefault: true}
	require.NoError(t, llmConfigRepo.Create(ctx, agent))
	require.NoError(t, taskRepo.Create(ctx, &models.Task{ProjectID: project.ID, Title: "Info task", Prompt: "prompt", Category: models.CategoryBacklog, Status: models.StatusPending, Priority: 2}))

	handlers := buildChannelUtilityActionHandlers(channelUtilityActionHandlerOptions{ProjectID: project.ID, TaskRepo: taskRepo, ProjectRepo: projectRepo, SettingsRepo: settingsRepo, LLMConfigRepo: llmConfigRepo})
	setOut, err := handlers["set_personality"](ctx, json.RawMessage(`{"personality":"no_nonsense_pro"}`))
	require.NoError(t, err)
	require.Contains(t, setOut, "Personality changed")
	getOut, err := handlers["get_personality"](ctx, nil)
	require.NoError(t, err)
	require.Contains(t, getOut, "no_nonsense_pro")
	modelsOut, err := handlers["list_models"](ctx, nil)
	require.NoError(t, err)
	require.Contains(t, modelsOut, "Default Model")
	projectOut, err := handlers["project_info"](ctx, nil)
	require.NoError(t, err)
	require.Contains(t, projectOut, "Info Project")
	require.Contains(t, projectOut, "Total tasks: 1")
}

func TestBuildChannelTaskActionHandlersCreateTaskUsesSharedLogicAndOriginCallback(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	projectRepo := repository.NewProjectRepo(db)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	project := &models.Project{Name: "Channel Actions"}
	require.NoError(t, projectRepo.Create(ctx, project))
	agent := &models.LLMConfig{Name: "Default", Provider: models.ProviderTest, Model: "test", IsDefault: true}
	require.NoError(t, llmConfigRepo.Create(ctx, agent))
	collector := newChannelActionSummaryCollector()
	var callbackTaskIDs []string
	handlers := buildChannelTaskActionHandlers(channelTaskActionHandlerOptions{
		ProjectID:     project.ID,
		TaskSvc:       NewTaskService(taskRepo, nil, nil),
		LLMConfigRepo: llmConfigRepo,
		Collector:     collector,
		OnTasksCreated: func(_ context.Context, tasks []models.Task) {
			for _, task := range tasks {
				callbackTaskIDs = append(callbackTaskIDs, task.ID)
			}
		},
	})
	payload, err := json.Marshal(TaskCreationRequest{Title: "Shared action task", Prompt: "Do shared work"})
	require.NoError(t, err)

	summary, err := handlers["create_task"](ctx, payload)
	require.NoError(t, err)
	require.Contains(t, summary, "Shared action task")
	require.Contains(t, summary, "[TASK_ID:")
	require.Len(t, callbackTaskIDs, 1)
	created, err := taskRepo.GetByID(ctx, callbackTaskIDs[0])
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, project.ID, created.ProjectID)
	require.Equal(t, 2, created.Priority)
	require.Contains(t, strings.Join(collector.createdLines, "\n"), callbackTaskIDs[0])
}
