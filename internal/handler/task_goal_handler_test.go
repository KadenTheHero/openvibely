package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/lifecycle"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
)

func TestTaskGoalRoutes_HTMXEditPauseResumeClear(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Goal UI", Category: models.CategoryBacklog, Status: models.StatusPending, Prompt: "prompt", Priority: 2}
	if err := tc.taskRepo.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := tc.HTMX().Post("/tasks/" + task.ID + "/goal").WithForm(url.Values{"goal": {"All checks pass"}}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("set goal status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "All checks pass") || !strings.Contains(rec.Body.String(), `>active</span>`) {
		t.Fatalf("set goal body missing panel content: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Active: true") || strings.Contains(rec.Body.String(), "Active: false") {
		t.Fatalf("goal panel should use the status pill instead of redundant boolean active text: %s", rec.Body.String())
	}

	for _, path := range []string{"/pause", "/resume", "/clear"} {
		rec = tc.HTMX().Post("/tasks/" + task.ID + "/goal" + path).Execute()
		if rec.Code != http.StatusOK {
			t.Fatalf("post %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	goal, err := tc.handler.taskGoalSvc.GetGoal(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if goal.Status != models.TaskGoalStatusCleared {
		t.Fatalf("goal status = %s", goal.Status)
	}
}

func TestUpdateTask_EditFormSavesGoalAndRefreshesReadOnlySummary(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Edit Goal", Category: models.CategoryBacklog, Status: models.StatusPending, Prompt: "prompt", Priority: 2}
	if err := tc.taskRepo.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	form := url.Values{
		"title":              {task.Title},
		"category":           {string(task.Category)},
		"priority":           {"2"},
		"prompt":             {task.Prompt},
		"tag":                {""},
		"agent_id":           {""},
		"goal_present":       {"1"},
		"goal":               {"Ship a clearer details UX"},
		"auto_merge_present": {"1"},
	}
	rec := tc.HTMX().Put("/tasks/" + task.ID).WithForm(form).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("update task status=%d body=%s", rec.Code, rec.Body.String())
	}
	goal, err := tc.handler.taskGoalSvc.GetGoal(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if goal == nil || goal.Objective != "Ship a clearer details UX" || goal.Status != models.TaskGoalStatusActive {
		t.Fatalf("unexpected saved goal: %#v", goal)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Ship a clearer details UX") || !strings.Contains(body, `>active</span>`) {
		t.Fatalf("expected refreshed read-only goal summary, got: %s", body)
	}
	if strings.Contains(body, "Active: true") || strings.Contains(body, "Active: false") {
		t.Fatalf("read-only goal summary should use the status pill instead of redundant boolean active text: %s", body)
	}
	viewStart := strings.Index(body, `id="task-detail-view"`)
	editStart := strings.Index(body, `id="task-detail-edit"`)
	if viewStart == -1 || editStart == -1 || editStart <= viewStart {
		t.Fatal("expected task detail view before edit form")
	}
	viewOnly := body[viewStart:editStart]
	for _, forbidden := range []string{"Add goal", "Pause", "Resume", "Clear"} {
		if strings.Contains(viewOnly, forbidden) {
			t.Fatalf("read-only goal summary should not include %q", forbidden)
		}
	}
}

func TestTaskGoalPanelLabelsUserStoppedPause(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	project := tc.CreateProject().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Stopped Goal UI", Category: models.CategoryBacklog, Status: models.StatusCancelled, Prompt: "prompt", Priority: 2}
	if err := tc.taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := tc.handler.taskGoalSvc.SetGoal(ctx, task.ID, "Keep going", service.GoalOptions{}); err != nil {
		t.Fatalf("set goal: %v", err)
	}
	if err := tc.handler.taskGoalSvc.PauseActiveGoalStoppedByUser(ctx, task.ID); err != nil {
		t.Fatalf("pause after user stop: %v", err)
	}

	rec := tc.HTMX().Get("/tasks/" + task.ID + "/goal").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("goal panel status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Paused (stopped by user)") {
		t.Fatalf("goal panel missing user-stopped paused label: %s", body)
	}
	if strings.Contains(body, ">paused</span>") {
		t.Fatalf("goal panel should not show bare paused status for user-stopped goal: %s", body)
	}
}

func TestTaskGoalContext_StatusToolGuidanceMatchesAgentGrants(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Goal prompt", Category: models.CategoryActive, Status: models.StatusRunning, Prompt: "prompt", Priority: 2}
	if err := tc.taskRepo.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := tc.handler.taskGoalSvc.SetGoal(context.Background(), task.ID, "Keep tests green", service.GoalOptions{}); err != nil {
		t.Fatalf("set goal: %v", err)
	}

	ungranted := tc.handler.taskGoalContext(context.Background(), task.ID, &models.Agent{Tools: []string{"send_to_task"}})
	if !strings.Contains(ungranted, "protected Goal Agent") {
		t.Fatalf("ungranted guidance should defer status evaluation to Goal Agent, got:\n%s", ungranted)
	}
	if strings.Contains(ungranted, "This assigned agent is explicitly granted goal status tools") {
		t.Fatalf("ungranted guidance advertised status tools, got:\n%s", ungranted)
	}

	granted := tc.handler.taskGoalContext(context.Background(), task.ID, &models.Agent{Tools: []string{"mark_task_goal_achieved"}})
	if !strings.Contains(granted, "explicitly granted these goal status tools: mark_task_goal_achieved") {
		t.Fatalf("granted guidance missing exact status-tool instruction, got:\n%s", granted)
	}
	if strings.Contains(granted, "report_task_goal_blocked") {
		t.Fatalf("single-tool guidance advertised ungranted blocker tool, got:\n%s", granted)
	}
	if !strings.Contains(granted, "goal_id") || !strings.Contains(granted, "still active") {
		t.Fatalf("granted guidance missing stale guard instruction, got:\n%s", granted)
	}
	if strings.Contains(granted, "handled by the protected Goal Agent") {
		t.Fatalf("granted guidance still says protected Goal Agent handles status, got:\n%s", granted)
	}
}

func TestGoalAgentSendToTaskSkipsContinuationWhenGoalPausedByUserStop(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	project := tc.CreateProject().Build()
	agent := tc.CreateLLMConfig().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Paused goal continuation", Category: models.CategoryActive, Status: models.StatusCompleted, Prompt: "prompt", Priority: 2, AgentID: &agent.ID}
	if err := tc.taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := tc.handler.taskGoalSvc.SetGoal(ctx, task.ID, "Keep going", service.GoalOptions{}); err != nil {
		t.Fatalf("set goal: %v", err)
	}
	if err := tc.handler.taskGoalSvc.PauseActiveGoalStoppedByUser(ctx, task.ID); err != nil {
		t.Fatalf("pause after user stop: %v", err)
	}

	params := streamingResponseParams{TaskID: task.ID, ProjectID: project.ID, IsTaskFollowup: true, AgentDefinition: &models.Agent{Tools: []string{"send_to_task"}}}
	handlers := tc.handler.chatActionHandlers(params, nil, models.ChatModeOrchestrate, "web")
	goalCtx := lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "goal-hook", SystemKind: models.AgentSystemKindGoal})
	out, err := handlers["send_to_task"](goalCtx, []byte(`{"task_id":"current","message":"Continue from stale evaluator"}`))
	if err == nil || !strings.Contains(err.Error(), "task goal is not active") {
		t.Fatalf("expected paused goal continuation to be rejected, out=%q err=%v", out, err)
	}
	pending, err := tc.handler.threadInputRepo.ListPendingForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("paused goal continuation queued pending inputs: %+v", pending)
	}
}

func TestGoalAgentSendToTaskQueuesWhenGoalStillActive(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	project := tc.CreateProject().Build()
	agent := tc.CreateLLMConfig().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Active goal continuation", Category: models.CategoryActive, Status: models.StatusCompleted, Prompt: "prompt", Priority: 2, AgentID: &agent.ID}
	if err := tc.taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := tc.handler.taskGoalSvc.SetGoal(ctx, task.ID, "Keep going", service.GoalOptions{}); err != nil {
		t.Fatalf("set goal: %v", err)
	}

	params := streamingResponseParams{TaskID: task.ID, ProjectID: project.ID, IsTaskFollowup: true, AgentDefinition: &models.Agent{Tools: []string{"send_to_task"}}}
	handlers := tc.handler.chatActionHandlers(params, nil, models.ChatModeOrchestrate, "web")
	goalCtx := lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "goal-hook", SystemKind: models.AgentSystemKindGoal})
	out, err := handlers["send_to_task"](goalCtx, []byte(`{"task_id":"current","message":"Continue because goal remains unmet"}`))
	if err != nil {
		t.Fatalf("active goal continuation rejected: out=%q err=%v", out, err)
	}
	pending, err := tc.handler.threadInputRepo.ListPendingForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Content != "Continue because goal remains unmet" || pending[0].Source != models.TaskOriginSystemAgent || pending[0].OriginAgent != models.AgentSystemKindGoal {
		t.Fatalf("active goal continuation pending = %+v", pending)
	}
}

func TestLifecycleSendToTaskRejectsStaleTaskRunContinuation(t *testing.T) {
	tc := NewTestContext(t)
	ctx := context.Background()
	project := tc.CreateProject().Build()
	agent := tc.CreateLLMConfig().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Stale continuation", Category: models.CategoryActive, Status: models.StatusRunning, Prompt: "prompt", Priority: 2, AgentID: &agent.ID}
	if err := tc.taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	tc.handler.lifecycleRepo = repository.NewLifecycleRepo(tc.db)
	lifecycleAgentRepo := repository.NewAgentRepo(tc.db)
	lifecycleAgent := &models.Agent{Name: "Lifecycle Agent", Model: "inherit", Enabled: true}
	if err := lifecycleAgentRepo.Create(ctx, lifecycleAgent); err != nil {
		t.Fatalf("create lifecycle agent: %v", err)
	}
	older := &models.LifecycleExecution{TaskID: task.ID, TaskRunID: "run-old", AgentID: lifecycleAgent.ID, When: models.LifecycleAfterComplete, SkillKey: "manage_dynamic_loop", OutputContract: models.OutputContractActivitySummary, Status: models.LifecycleExecRunning}
	if err := tc.handler.lifecycleRepo.CreateExecution(ctx, older); err != nil {
		t.Fatalf("create older lifecycle execution: %v", err)
	}
	newer := &models.LifecycleExecution{TaskID: task.ID, TaskRunID: "run-new", AgentID: lifecycleAgent.ID, When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Status: models.LifecycleExecRunning}
	if err := tc.handler.lifecycleRepo.CreateExecution(ctx, newer); err != nil {
		t.Fatalf("create newer lifecycle execution: %v", err)
	}

	params := streamingResponseParams{TaskID: task.ID, ProjectID: project.ID, IsTaskFollowup: true, AgentDefinition: &models.Agent{Tools: []string{"send_to_task"}}}
	handlers := tc.handler.chatActionHandlers(params, nil, models.ChatModeOrchestrate, "web")
	staleCtx := lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "hook-agent", Tools: []string{"send_to_task"}, TaskRunID: "run-old"})
	out, err := handlers["send_to_task"](staleCtx, []byte(`{"task_id":"current","message":"Duplicate continuation"}`))
	if err == nil || !strings.Contains(err.Error(), "stale lifecycle task run") {
		t.Fatalf("expected stale lifecycle continuation to be rejected, out=%q err=%v", out, err)
	}
	pending, err := tc.handler.threadInputRepo.ListPendingForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("stale lifecycle continuation queued pending inputs: %+v", pending)
	}
}

func TestTaskGoalTools_CurrentAliasAndSendToTaskQueuesOnly(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	agent := tc.CreateLLMConfig().Build()
	task := &models.Task{ProjectID: project.ID, Title: "Goal tools", Category: models.CategoryActive, Status: models.StatusRunning, Prompt: "prompt", Priority: 2, AgentID: &agent.ID}
	if err := tc.taskRepo.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	params := streamingResponseParams{TaskID: task.ID, ProjectID: project.ID, IsTaskFollowup: true}
	handlers := tc.handler.chatActionHandlers(params, nil, models.ChatModeOrchestrate, "web")

	out, err := handlers["set_task_goal"](context.Background(), []byte(`{"task_id":"current","goal":"Ship complete"}`))
	if err != nil {
		t.Fatalf("set current goal: %v", err)
	}
	if !strings.Contains(out, "Ship complete") {
		t.Fatalf("set output = %s", out)
	}
	goal, err := tc.handler.taskGoalSvc.GetGoal(context.Background(), task.ID)
	if err != nil || goal == nil {
		t.Fatalf("get goal after set: %v %#v", err, goal)
	}
	if _, err := handlers["mark_task_goal_achieved"](context.Background(), []byte(`{"task_id":"current","goal_id":"`+goal.GoalID+`","reason":"done"}`)); err == nil || !strings.Contains(err.Error(), "explicit agent tool grant") {
		t.Fatalf("ungranted assistant marked goal achieved, err=%v", err)
	}
	grantParams := params
	grantParams.AgentDefinition = &models.Agent{Tools: []string{"mark_task_goal_achieved"}}
	grantHandlers := tc.handler.chatActionHandlers(grantParams, nil, models.ChatModeOrchestrate, "web")
	out, err = grantHandlers["mark_task_goal_achieved"](context.Background(), []byte(`{"task_id":"current","goal_id":"`+goal.GoalID+`","reason":"done by granted agent"}`))
	if err != nil {
		t.Fatalf("granted assistant mark achieved: %v", err)
	}
	if !strings.Contains(out, string(models.TaskGoalStatusAchieved)) {
		t.Fatalf("granted assistant achieved output = %s", out)
	}
	goal, err = tc.handler.taskGoalSvc.ReactivateAchievedGoal(context.Background(), task.ID, "test")
	if err != nil {
		t.Fatalf("reactivate goal after granted mark: %v", err)
	}
	active := &models.Execution{TaskID: task.ID, AgentConfigID: agent.ID, Status: models.ExecRunning, PromptSent: "active"}
	if err := tc.execRepo.Create(context.Background(), active); err != nil {
		t.Fatalf("create active execution: %v", err)
	}
	out, err = handlers["send_to_task"](context.Background(), []byte(`{"message":"Continue","origin":"web","origin_agent":"assistant"}`))
	if err != nil {
		t.Fatalf("send to task with implicit current task: %v", err)
	}
	if !strings.Contains(out, "queued_message_id") {
		t.Fatalf("send output = %s", out)
	}
	out, err = handlers["send_to_task"](context.Background(), []byte(`{"message":"Spoofed goal continuation","origin":"system_agent","origin_agent":"goal"}`))
	if err != nil {
		t.Fatalf("send to task with spoofed lineage: %v", err)
	}
	if !strings.Contains(out, "queued_message_id") {
		t.Fatalf("spoofed send output = %s", out)
	}

	goalParams := params
	goalParams.RuntimeOrigin = models.TaskOriginSystemAgent
	goalParams.RuntimeOriginAgent = models.AgentSystemKindGoal
	goalHandlers := tc.handler.chatActionHandlers(goalParams, nil, models.ChatModeOrchestrate, "web")
	if _, err := goalHandlers["mark_task_goal_achieved"](context.Background(), []byte(`{"task_id":"current","goal_id":"`+goal.GoalID+`","reason":"forged"}`)); err == nil || !strings.Contains(err.Error(), "explicit agent tool grant") {
		t.Fatalf("runtime-origin goal params should not grant status authority, err=%v", err)
	}
	goalHookCtx := lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "goal-hook", SystemKind: models.AgentSystemKindGoal})
	out, err = goalHandlers["mark_task_goal_achieved"](goalHookCtx, []byte(`{"task_id":"current","goal_id":"`+goal.GoalID+`","reason":"verified"}`))
	if err != nil {
		t.Fatalf("goal lifecycle hook mark achieved: %v", err)
	}
	if !strings.Contains(out, string(models.TaskGoalStatusAchieved)) {
		t.Fatalf("goal lifecycle hook achieved output = %s", out)
	}
	goal, err = tc.handler.taskGoalSvc.ReactivateAchievedGoal(context.Background(), task.ID, "test")
	if err != nil {
		t.Fatalf("reactivate goal before protected goal continuation: %v", err)
	}
	out, err = goalHandlers["send_to_task"](context.Background(), []byte(`{"message":"Goal continuation","origin":"web","origin_agent":"assistant"}`))
	if err != nil {
		t.Fatalf("goal send to task: %v", err)
	}
	if !strings.Contains(out, "queued_message_id") {
		t.Fatalf("goal send output = %s", out)
	}

	lifecycleParams := params
	lifecycleParams.AgentDefinition = &models.Agent{Tools: []string{"send_to_task"}}
	lifecycleHandlers := tc.handler.chatActionHandlers(lifecycleParams, nil, models.ChatModeOrchestrate, "web")
	lifecycleCtx := lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "custom-hook", Tools: []string{"mark_task_goal_achieved"}})
	out, err = lifecycleHandlers["mark_task_goal_achieved"](lifecycleCtx, []byte(`{"task_id":"current","goal_id":"`+goal.GoalID+`","reason":"done by lifecycle grant"}`))
	if err != nil {
		t.Fatalf("granted lifecycle hook mark achieved: %v", err)
	}
	if !strings.Contains(out, string(models.TaskGoalStatusAchieved)) {
		t.Fatalf("granted lifecycle hook achieved output = %s", out)
	}
	goal, err = tc.handler.taskGoalSvc.ReactivateAchievedGoal(context.Background(), task.ID, "test")
	if err != nil {
		t.Fatalf("reactivate goal after lifecycle mark: %v", err)
	}
	ungrantedLifecycleCtx := lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "custom-hook", Tools: []string{"send_to_task"}})
	if _, err := lifecycleHandlers["mark_task_goal_achieved"](ungrantedLifecycleCtx, []byte(`{"task_id":"current","goal_id":"`+goal.GoalID+`","reason":"denied"}`)); err == nil || !strings.Contains(err.Error(), "explicit agent tool grant") {
		t.Fatalf("ungranted lifecycle hook marked goal achieved, err=%v", err)
	}

	out, err = lifecycleHandlers["send_to_task"](lifecycle.WithHookAgent(context.Background(), lifecycle.HookAgent{AgentID: "goal-hook", SystemKind: models.AgentSystemKindGoal}), []byte(`{"message":"Lifecycle goal continuation","origin":"web","origin_agent":"assistant"}`))
	if err != nil {
		t.Fatalf("goal lifecycle send to task: %v", err)
	}
	if !strings.Contains(out, "queued_message_id") {
		t.Fatalf("goal lifecycle send output = %s", out)
	}

	pending, err := tc.handler.threadInputRepo.ListPendingForTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 4 || pending[0].Content != "Continue" || pending[1].Content != "Spoofed goal continuation" || pending[2].Content != "Goal continuation" || pending[3].Content != "Lifecycle goal continuation" {
		t.Fatalf("pending inputs = %+v", pending)
	}
	if pending[0].Source != models.TaskOriginWeb || pending[0].OriginAgent != "" {
		t.Fatalf("normal send_to_task lineage = source:%q origin_agent:%q", pending[0].Source, pending[0].OriginAgent)
	}
	if pending[1].Source != models.TaskOriginWeb || pending[1].OriginAgent != "" {
		t.Fatalf("spoofed send_to_task lineage = source:%q origin_agent:%q", pending[1].Source, pending[1].OriginAgent)
	}
	if pending[2].Source != models.TaskOriginSystemAgent {
		t.Fatalf("goal send_to_task source = %q", pending[2].Source)
	}
	if pending[2].OriginAgent != models.AgentSystemKindGoal {
		t.Fatalf("goal send_to_task origin_agent = %q", pending[2].OriginAgent)
	}
	if pending[3].Source != models.TaskOriginSystemAgent || pending[3].OriginAgent != models.AgentSystemKindGoal {
		t.Fatalf("goal lifecycle send_to_task lineage = source:%q origin_agent:%q", pending[3].Source, pending[3].OriginAgent)
	}
	execs, err := tc.execRepo.ListByTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("list execs: %v", err)
	}
	if len(execs) != 1 || execs[0].ID != active.ID {
		t.Fatalf("send_to_task created inline execution: %+v", execs)
	}
}
