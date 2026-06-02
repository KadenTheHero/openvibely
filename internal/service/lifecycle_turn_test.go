package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/agentskills"
	"github.com/openvibely/openvibely/internal/lifecycle"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type routeHookStore struct {
	hooks     []models.AgentLifecycleHook
	created   []models.LifecycleExecution
	completed []models.LifecycleExecution
}

func (s *routeHookStore) HooksForWhen(ctx context.Context, when models.LifecycleWhen) ([]models.AgentLifecycleHook, error) {
	var out []models.AgentLifecycleHook
	for _, h := range s.hooks {
		if h.When == when && h.Enabled {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *routeHookStore) CreateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	if e.ID == "" {
		e.ID = "exec"
		if e.LifecycleHookID != nil {
			e.ID += "-" + *e.LifecycleHookID
		}
	}
	s.created = append(s.created, *e)
	return nil
}

func (s *routeHookStore) UpdateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	s.completed = append(s.completed, *e)
	return nil
}

func (s *routeHookStore) FindExecutionByIdempotencyKey(ctx context.Context, key string) (*models.LifecycleExecution, error) {
	return nil, sql.ErrNoRows
}

type routeHookInvoker struct {
	outputs map[string]json.RawMessage
}

func (i *routeHookInvoker) Invoke(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
	if out, ok := i.outputs[hook.ID]; ok {
		return out, nil
	}
	return nil, fmt.Errorf("missing output for hook %s", hook.ID)
}

type routeHookInvokerFunc func(context.Context, models.AgentLifecycleHook, lifecycle.HookInput) (json.RawMessage, error)

func (f routeHookInvokerFunc) Invoke(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
	return f(ctx, hook, in)
}

func routePayload(skills []string, confidence float64) json.RawMessage {
	b, _ := json.Marshal(lifecycle.SelectedSkills{Skills: skills, Confidence: confidence, Reason: "test"})
	return b
}

func routeClarificationPayload(question string) json.RawMessage {
	b, _ := json.Marshal(lifecycle.SelectedSkills{NeedsClarification: true, ClarifyingQuestion: question})
	return b
}

func contextBlockPayload(title string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"title":%q,"content":"test context"}`, title))
}

func memoryContextBlockPayload(content string, sources ...string) json.RawMessage {
	b, _ := json.Marshal(lifecycle.ContextBlock{Content: content, Sources: sources, Confidence: 0.9})
	return b
}

func routeTestRunner(outputs map[string]json.RawMessage) *lifecycle.Runner {
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "a-low", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true},
		{ID: "b-high", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true},
	}}
	return lifecycle.NewRunner(store, &routeHookInvoker{outputs: outputs}, nil)
}

func routeTestRunnerWithStore(outputs map[string]json.RawMessage) (*lifecycle.Runner, *routeHookStore) {
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "a-low", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true},
		{ID: "b-high", When: models.LifecycleBeforeRun, SkillKey: "before_run", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
	}}
	return lifecycle.NewRunner(store, &routeHookInvoker{outputs: outputs}, nil), store
}

func TestPrepareLifecycleTurn_RouteTaskSelectsSkillsWithoutSwitchingAgent(t *testing.T) {
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(routeTestRunner(map[string]json.RawMessage{
		"a-low":  routePayload([]string{"missing/low"}, 0.2),
		"b-high": routePayload([]string{"missing_high"}, 0.9),
	}))

	turn := worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-1"})
	if turn.Task.AgentDefinitionID != nil {
		t.Fatalf("route_task must not auto-assign agents, got %v", *turn.Task.AgentDefinitionID)
	}
}

func TestPrepareLifecycleTurn_RouteTaskIgnoresInvalidSelectedSkills(t *testing.T) {
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(routeTestRunner(map[string]json.RawMessage{
		"a-low":  routeClarificationPayload("which skill area?"),
		"b-high": json.RawMessage(`{"skills":["bad"],"confidence":0.9}`),
	}))

	turn := worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-1"})
	if turn.Task.AgentDefinitionID != nil {
		t.Fatalf("invalid skill routing must not auto-assign agents, got %v", *turn.Task.AgentDefinitionID)
	}
}

func TestPrepareLifecycleTurn_RouteTaskInputIncludesUserTaskText(t *testing.T) {
	var captured lifecycle.HookInput
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{
		ID:             "route",
		When:           models.LifecycleRouteTask,
		SkillKey:       "route_task",
		OutputContract: models.OutputContractSelectedSkills,
		Blocking:       true,
		Enabled:        true,
	}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(_ context.Context, _ models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		captured = in
		return routePayload(nil, 0.8), nil
	}), nil)
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)

	worker.PrepareLifecycleTurn(context.Background(), models.Task{
		ID:        "task-art",
		ProjectID: "project-1",
		Title:     "Generate algorithmic dizzy city skyline artwork",
		Prompt:    "Create an algorithmic art implementation with a dizzy city skyline.",
	})

	if captured.TaskTitle != "Generate algorithmic dizzy city skyline artwork" {
		t.Fatalf("route_task input missing task title, got %q", captured.TaskTitle)
	}
	if !strings.Contains(captured.TaskPrompt, "dizzy city skyline") {
		t.Fatalf("route_task input missing task prompt, got %q", captured.TaskPrompt)
	}
	if _, ok := captured.Extras["available_skills"]; !ok {
		t.Fatalf("route_task input missing available_skills: %#v", captured.Extras)
	}
}

func TestPrepareLifecycleTurn_UsesDistinctTaskRunIDPerRun(t *testing.T) {
	runner, store := routeTestRunnerWithStore(map[string]json.RawMessage{
		"a-low":  routePayload([]string{"skill"}, 0.8),
		"b-high": contextBlockPayload("prepared"),
	})
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)

	worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-1"})
	worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-1"})

	seen := map[string]bool{}
	for _, exec := range store.created {
		if exec.TaskID != "task-1" {
			t.Fatalf("unexpected task id %q", exec.TaskID)
		}
		if exec.TaskRunID == "" || exec.TaskRunID == "task-1" {
			t.Fatalf("expected generated task run id, got %q", exec.TaskRunID)
		}
		seen[exec.TaskRunID] = true
	}
	if len(store.created) < 4 {
		t.Fatalf("expected lifecycle rows from both runs, got %d", len(store.created))
	}
	if len(seen) != 2 {
		t.Fatalf("expected exactly two task run ids, got %v", seen)
	}
}

func TestPrepareLifecycleTurn_AfterCompleteUsesProvidedTaskChatContext(t *testing.T) {
	ctx := context.Background()
	var captured lifecycle.HookInput
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{
		ID:             "learn",
		When:           models.LifecycleAfterComplete,
		SkillKey:       "observe_task_for_learning",
		OutputContract: models.OutputContractLearningSummary,
		Blocking:       false,
		Enabled:        true,
	}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(_ context.Context, _ models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		captured = in
		return json.RawMessage(`{"summary":"ok","nothing_to_save":true}`), nil
	}), nil)
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)

	worker.runLifecycleSlot(ctx, models.LifecycleAfterComplete, models.Task{ID: "task-transcript", ProjectID: "default", Title: "Transcript task", Prompt: "initial user request", Status: models.StatusCompleted}, "run-1", nil, llmcontracts.ChatContext{Messages: []llmcontracts.ChatContextMessage{
		{Role: "user", Content: "compacted user context"},
		{Role: "assistant", Content: "compacted assistant context"},
	}})

	raw, ok := captured.Extras[lifecycle.ConversationTranscriptKey]
	if !ok {
		t.Fatalf("expected conversation transcript in extras, got %#v", captured.Extras)
	}
	transcript, ok := raw.(llmcontracts.ChatContext)
	if !ok {
		t.Fatalf("expected task chat context, got %T", raw)
	}
	if len(transcript.Messages) != 2 {
		t.Fatalf("expected provided chat messages, got %#v", transcript.Messages)
	}
	if transcript.Messages[0].Content != "compacted user context" || transcript.Messages[1].Content != "compacted assistant context" {
		t.Fatalf("expected provided chat context, got %#v", transcript.Messages)
	}
}

func TestPrepareLifecycleTurn_AfterCompleteDoesNotRebuildFromExecutions(t *testing.T) {
	ctx := context.Background()
	var captured lifecycle.HookInput
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{
		ID:             "learn",
		When:           models.LifecycleAfterComplete,
		SkillKey:       "observe_task_for_learning",
		OutputContract: models.OutputContractLearningSummary,
		Enabled:        true,
	}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(_ context.Context, _ models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		captured = in
		return json.RawMessage(`{"summary":"ok","nothing_to_save":true}`), nil
	}), nil)
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)

	worker.runLifecycleSlot(ctx, models.LifecycleAfterComplete, models.Task{ID: "task-chat-context", ProjectID: "default", Title: "Chat context task", Prompt: "initial user request", Status: models.StatusCompleted}, "run-1", nil, llmcontracts.ChatContext{Messages: []llmcontracts.ChatContextMessage{
		{Role: "user", Content: "current compacted request"},
	}})
	transcript := captured.Extras[lifecycle.ConversationTranscriptKey].(llmcontracts.ChatContext)
	encoded, err := json.Marshal(transcript)
	if err != nil {
		t.Fatalf("marshal transcript: %v", err)
	}
	if string(encoded) != `{"messages":[{"role":"user","content":"current compacted request"}]}` {
		t.Fatalf("expected only provided chat context, got %s", encoded)
	}
	for _, forbidden := range []string{"diff_output", "error_message", "status", "execution_id", "task_id", "truncated", "initial user request"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("chat context should not include %q: %s", forbidden, encoded)
		}
	}
}

func TestPrepareLifecycleTurn_AfterCompletePreservesRuntimeTools(t *testing.T) {
	ctx := context.Background()
	done := make(chan error, 1)
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{
		ID:             "learn",
		When:           models.LifecycleAfterComplete,
		SkillKey:       "observe_task_for_learning",
		OutputContract: models.OutputContractLearningSummary,
		Enabled:        true,
	}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, _ models.AgentLifecycleHook, _ lifecycle.HookInput) (json.RawMessage, error) {
		rt := llmcontracts.RuntimeToolsFromContext(ctx)
		if rt == nil || !rt.HasDefinition("skill_manage") || !rt.HasDefinition("skills_list") || !rt.HasDefinition("agent_list") {
			done <- fmt.Errorf("expected lifecycle runtime tools in after_complete context, got %#v", rt)
			return json.RawMessage(`{"summary":"missing tools","nothing_to_save":true}`), nil
		}
		done <- nil
		return json.RawMessage(`{"summary":"ok","nothing_to_save":true}`), nil
	}), nil)
	db := testutil.NewTestDB(t)
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(t.TempDir())
	worker.SetLifecycleAgentRepo(repository.NewAgentRepo(db))
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))

	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-tools"})
	turn.AfterComplete(nil, llmcontracts.ChatContext{})

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for after_complete hook")
	}
}

func TestPrepareLifecycleTurn_RouteTaskNoValidSkillsKeepsAgentUnchanged(t *testing.T) {
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(routeTestRunner(map[string]json.RawMessage{
		"a-low":  routeClarificationPayload("which skill area?"),
		"b-high": json.RawMessage(`{"skills":[],"confidence":0.8,"reason":"none"}`),
	}))

	turn := worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-1"})
	if turn.Task.AgentDefinitionID != nil {
		t.Fatalf("route_task must not auto-assign agents, got %v", turn.Task.AgentDefinitionID)
	}
}

func TestPrepareLifecycleTurn_AssignedAgentRoutesAgentOwnedSkills(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLifecycleTestSkill(t, root, "task_agent", "task_skill", "assigned agent skill body")
	writeLifecycleStandaloneSkill(t, root, "standalone_skill", "standalone body")

	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent := &models.Agent{ID: "task-agent-id", Key: "task_agent", Name: "Task Agent", Enabled: true, Tools: []string{"skill_view"}}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create task agent: %v", err)
	}

	var routeCalled bool
	var available any
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		routeCalled = true
		available = in.Extras["available_skills"]
		return routePayload([]string{"task_skill", "standalone_skill"}, 0.9), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-1", AgentDefinitionID: &agent.ID})
	if !routeCalled {
		t.Fatal("assigned agent tasks should run route_task against the assigned agent skill index")
	}
	if turn.Task.AgentDefinitionID == nil || *turn.Task.AgentDefinitionID != agent.ID {
		t.Fatalf("expected explicit assigned agent kept, got %v", turn.Task.AgentDefinitionID)
	}
	availableText, _ := available.(string)
	if !strings.Contains(availableText, "Available Assigned-Agent Skills") || !strings.Contains(availableText, "task_agent/task_skill") || strings.Contains(availableText, "standalone_skill") {
		t.Fatalf("route_task should receive only assigned-agent skills, got:\n%s", availableText)
	}
	instructions := additionalProjectInstructionsFromContext(turn.Ctx)
	if !strings.Contains(instructions, "task_skill") || strings.Contains(instructions, "standalone_skill") {
		t.Fatalf("task prompt should include only selected assigned-agent skill, got:\n%s", instructions)
	}
	rt := llmcontracts.RuntimeToolsFromContext(turn.Ctx)
	out, handled, isErr, err := rt.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"task_skill"}`))
	if !handled || isErr || err != nil || !strings.Contains(out, "assigned agent skill body") {
		t.Fatalf("selected assigned-agent skill_view failed handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = rt.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"standalone_skill"}`))
	if !handled || err != nil || !isErr || !strings.Contains(out, "not in this turn's authorized index") {
		t.Fatalf("standalone skill must not load for assigned-agent turn handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
}

func TestPrepareLifecycleTurn_SelectedTaskSkillsDoNotHideHookSkills(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLifecycleStandaloneSkill(t, root, "task_skill", "task skill body")
	writeLifecycleTestSkill(t, root, "skill_curator", "observe_task_for_learning", "observer skill body")

	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent, err := agentRepo.GetByKey(ctx, "skill_curator")
	if err != nil {
		t.Fatalf("load skill curator: %v", err)
	}
	if agent == nil {
		t.Fatal("expected seeded skill_curator")
	}

	store := &routeHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "route", AgentID: agent.ID, When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true},
		{ID: "learn", AgentID: agent.ID, When: models.LifecycleAfterComplete, SkillKey: "observe_task_for_learning", OutputContract: models.OutputContractLearningSummary, Enabled: true},
	}}
	var afterSkillBody string
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(_ context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		switch hook.ID {
		case "route":
			return routePayload([]string{"task_skill"}, 0.9), nil
		case "learn":
			afterSkillBody = in.SkillBody
			return json.RawMessage(`{"summary":"ok","nothing_to_save":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected hook %s", hook.ID)
		}
	}), NewCatalogSkillResolver(agentRepo, func() *agentskills.Catalog {
		return nil
	}, root, nil))

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))

	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-selected-skill"})
	turn.AfterComplete(nil, llmcontracts.ChatContext{})

	deadline := time.After(2 * time.Second)
	for afterSkillBody == "" {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for after_complete skill resolution")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !strings.Contains(afterSkillBody, "observer skill body") {
		t.Fatalf("expected after_complete hook skill from full catalog, got %q", afterSkillBody)
	}
}

func TestPrepareLifecycleTurn_SeparatesTaskTurnAndAfterCompleteRuntimeTools(t *testing.T) {
	ctx := context.Background()
	mainTools := &llmcontracts.RuntimeTools{Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "main_task_tool"}}}
	afterTools := &llmcontracts.RuntimeTools{Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "after_complete_tool"}}}
	ctx = llmcontracts.WithRuntimeTools(ctx, mainTools)
	ctx = WithAfterCompleteRuntimeTools(ctx, afterTools)

	worker := NewWorkerService(nil, 0, nil)
	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-runtime-separation"})
	mainRT := llmcontracts.RuntimeToolsFromContext(turn.Ctx)
	if mainRT == nil || !mainRT.HasDefinition("main_task_tool") {
		t.Fatalf("main task turn lost incoming runtime tools: %#v", mainRT)
	}
	if mainRT.HasDefinition("after_complete_tool") {
		t.Fatalf("after-complete-only tools leaked into main task turn: %#v", mainRT.Definitions)
	}

	seen := make(chan *llmcontracts.RuntimeTools, 1)
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "after-hook", AgentID: "agent", When: models.LifecycleAfterComplete, SkillKey: "observe", OutputContract: models.OutputContractActivitySummary, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		seen <- llmcontracts.RuntimeToolsFromContext(ctx)
		return json.RawMessage(`{"summary":"ok","changed_paths":[]}`), nil
	}), nil)
	worker.SetLifecycleRunner(runner)
	turn.AfterComplete(nil, llmcontracts.ChatContext{})
	select {
	case rt := <-seen:
		if rt == nil || !rt.HasDefinition("after_complete_tool") {
			t.Fatalf("after_complete hook did not receive after-complete runtime tools: %#v", rt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for after_complete hook")
	}
}

func TestPrepareLifecycleTurn_TaskRuntimeToolsExposeOnlySelectedSkillView(t *testing.T) {
	root := t.TempDir()
	writeLifecycleStandaloneSkill(t, root, "task_skill", "task skill body")

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleSkillRoot(root)
	turn := worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-runtime-tools"})
	rt := llmcontracts.RuntimeToolsFromContext(turn.Ctx)
	if rt == nil || !rt.HasDefinition("skill_view") {
		t.Fatalf("expected task skill_view runtime tools, got %#v", rt)
	}
	assertTaskRuntimeIsSelectedSkillOnly(t, rt)
	out, handled, isErr, err := rt.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"task_skill"}`))
	if !handled || err != nil || !isErr || !strings.Contains(out, "not in this turn's authorized index") {
		t.Fatalf("unselected skill must not load, handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
}

func TestPrepareLifecycleTurn_NormalTaskAssignedToSkillMaintainerDoesNotGetMutationTools(t *testing.T) {
	root := t.TempDir()
	writeLifecycleTestSkill(t, root, "skill_curator", "maintain_skill_library", "maintenance skill body")
	writeLifecycleStandaloneSkill(t, root, "other_skill", "other skill body")
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent, err := agentRepo.GetBySystemKind(ctx, models.AgentSystemKindSkillCurator)
	if err != nil || agent == nil {
		t.Fatalf("load system agent: agent=%#v err=%v", agent, err)
	}

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))
	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "normal-task", Title: "User task", Category: models.CategoryActive, AgentDefinitionID: &agent.ID})
	rt := llmcontracts.RuntimeToolsFromContext(turn.Ctx)
	assertTaskRuntimeIsSelectedSkillOnly(t, rt)
	if cat := lifecycleTurnFromContext(turn.Ctx).Catalog; cat == nil {
		t.Fatal("expected selected-task catalog in lifecycle context")
	} else if _, ok := cat.Lookup("other_skill"); ok {
		t.Fatal("assigned-agent task context must not retain full skill catalog when router is skipped")
	}
}

func TestPrepareLifecycleTurn_ScheduledSkillMaintenanceTaskUsesScopedSkillLibraryTools(t *testing.T) {
	root := t.TempDir()
	writeLifecycleTestSkill(t, root, "skill_curator", "maintain_skill_library", "maintenance skill body")
	writeLifecycleTestSkill(t, root, "skill_curator", "observe_task_for_learning", "observe body")
	writeLifecycleStandaloneSkill(t, root, "other_skill", "other skill body")
	writeLifecycleStandaloneSkill(t, root, "maintain_skill_library", "standalone maintenance body")
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent, err := agentRepo.GetBySystemKind(ctx, models.AgentSystemKindSkillCurator)
	if err != nil || agent == nil {
		t.Fatalf("load system agent: agent=%#v err=%v", agent, err)
	}

	var routeCalled bool
	var available any
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		routeCalled = true
		available = in.Extras["available_skills"]
		return routePayload([]string{"maintain_skill_library"}, 0.9), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))
	task := models.Task{ID: "maintenance-task", Title: agentLibraryMaintenanceTaskTitle, Category: models.CategoryScheduled, AgentDefinitionID: &agent.ID}
	turn := worker.PrepareLifecycleTurn(ctx, task)
	if !routeCalled {
		t.Fatal("system skill maintenance task should run route_task against Skill Curator's owned skills")
	}
	availableText, _ := available.(string)
	if !strings.Contains(availableText, "skill_curator/maintain_skill_library") || strings.Contains(availableText, "other_skill") {
		t.Fatalf("route_task should receive Skill Curator skill index only, got:\n%s", availableText)
	}
	rt := llmcontracts.RuntimeToolsFromContext(turn.Ctx)
	assertTaskRuntimeIsSelectedSkillOnly(t, rt)
	instructions := additionalProjectInstructionsFromContext(turn.Ctx)
	if !strings.Contains(instructions, "maintain_skill_library") || strings.Contains(instructions, "observe_task_for_learning") || strings.Contains(instructions, "other_skill") {
		t.Fatalf("maintenance prompt should include only router-selected maintainer skill, got:\n%s", instructions)
	}
	out, handled, isErr, err := rt.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"maintain_skill_library"}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "maintenance skill body") {
		t.Fatalf("selected maintenance skill_view failed handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = rt.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"skill_curator/maintain_skill_library"}`))
	if !handled || err != nil || !isErr || !strings.Contains(out, "not a valid") {
		t.Fatalf("agent-prefixed handle must be rejected handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}

	llmSvc := NewLLMService(nil, nil, nil, nil, nil, nil)
	llmSvc.SetGlobalSkillRoot(root)
	llmSvc.SetAgentRepo(agentRepo)
	llmSvc.SetLifecycleRepo(repository.NewLifecycleRepo(db))
	maintenanceTools := llmSvc.agentDeclaredSkillRuntimeTools(turn.Ctx, task, agent, "")
	mergedTools := llmcontracts.CompositeRuntimeTools(maintenanceTools, rt)
	for _, want := range []string{"skill_view", "skills_list", "agent_list", "agent_view", "skill_manage", "agent_skill_manage"} {
		if !mergedTools.HasDefinition(want) {
			t.Fatalf("scheduled maintenance runtime missing %s: %#v", want, mergedTools.Definitions)
		}
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skills_list", json.RawMessage(`{}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "## other_skill") || !strings.Contains(out, "standalone:other_skill") || !strings.Contains(out, "standalone:maintain_skill_library") || strings.Contains(out, "skill_curator/maintain_skill_library") {
		t.Fatalf("maintenance skills_list should expose standalone skills only with qualified view handles handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"other_skill"}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "other skill body") {
		t.Fatalf("maintenance skill_view must load unambiguous listed standalone skill handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"maintain_skill_library"}`))
	if !handled || err != nil || !isErr || !strings.Contains(out, "ambiguous") {
		t.Fatalf("maintenance skill_view must reject colliding bare handles handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"standalone:maintain_skill_library"}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "standalone maintenance body") {
		t.Fatalf("maintenance skill_view must load qualified standalone skill handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"agent:skill_curator/maintain_skill_library"}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "maintenance skill body") {
		t.Fatalf("maintenance skill_view must load qualified selected agent skill read-only handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	reviewerAgent := &models.Agent{ID: "reviewer-agent-id", Key: "reviewer", Name: "Reviewer", Description: "Reviews code", Scope: models.AgentScopeGlobal, Enabled: true, SelectableAsPrimary: true, GeneratedStatus: models.AgentStatusGenerated, Skills: []models.SkillConfig{{Name: "review_migrations", Description: "Review migrations"}}}
	if err := agentRepo.Create(ctx, reviewerAgent); err != nil {
		t.Fatalf("create reviewer agent: %v", err)
	}
	disabledAgent := &models.Agent{ID: "disabled-agent-id", Key: "disabled_agent", Name: "Disabled", Enabled: false, GeneratedStatus: models.AgentStatusGenerated}
	if err := agentRepo.Create(ctx, disabledAgent); err != nil {
		t.Fatalf("create disabled agent: %v", err)
	}
	archivedAgent := &models.Agent{ID: "archived-agent-id", Key: "archived_agent", Name: "Archived", Enabled: true, GeneratedStatus: models.AgentStatusArchived}
	if err := agentRepo.Create(ctx, archivedAgent); err != nil {
		t.Fatalf("create archived agent: %v", err)
	}

	out, handled, isErr, err = mergedTools.Executor(context.Background(), "agent_list", json.RawMessage(`{}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "reviewer") || strings.Contains(out, "skill_curator") || strings.Contains(out, "memory_curator") || strings.Contains(out, "disabled_agent") || strings.Contains(out, "archived_agent") {
		t.Fatalf("agent_list should expose active non-system agents only handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skill_manage", json.RawMessage(`{"action":"write_file","handle":"other_skill","scope":"global","support":{"kind":"references","path":"maintenance-note.md","content":"kept"}}`))
	if !handled || err != nil || isErr {
		t.Fatalf("maintenance skill_manage write_file failed handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "other_skill", "references", "maintenance-note.md")); err != nil {
		t.Fatalf("expected skill_manage to write standalone support file: %v", err)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "skill_manage", json.RawMessage(`{"action":"write_file","handle":"skill_curator/maintain_skill_library","scope":"global","support":{"kind":"references","path":"forbidden.md","content":"blocked"}}`))
	if !handled || err != nil || !isErr || !strings.Contains(out, "invalid standalone skill handle") {
		t.Fatalf("skill_manage must not mutate agent-owned system skills handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	out, handled, isErr, err = mergedTools.Executor(context.Background(), "agent_skill_manage", json.RawMessage(`{"action":"create","agent":"reviewer","scope":"global","declaration":"---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n  key: review_migrations\n---\n# Review migrations\n"}`))
	if !handled || err != nil || isErr {
		t.Fatalf("agent_skill_manage should mutate non-system agent skills handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "agents", "reviewer", "skills", "review_migrations", "SKILL.md")); err != nil {
		t.Fatalf("agent_skill_manage did not write non-system agent skill: %v", err)
	}
	for _, tc := range []struct {
		agent string
		skill string
		want  string
	}{
		{agent: "skill_curator", skill: "maintain_skill_library", want: "protected"},
		{agent: "memory_curator", skill: "consolidate_memory", want: "protected"},
		{agent: "disabled_agent", skill: "disabled_skill", want: "disabled"},
		{agent: "archived_agent", skill: "archived_skill", want: "archived"},
	} {
		params := fmt.Sprintf(`{"action":"write_file","agent":%q,"scope":"global","handle":%q,"support":{"kind":"references","path":"forbidden.md","content":"blocked"}}`, tc.agent, tc.skill)
		out, handled, isErr, err = mergedTools.Executor(context.Background(), "agent_skill_manage", json.RawMessage(params))
		if !handled || err != nil || !isErr || !strings.Contains(out, tc.want) {
			t.Fatalf("agent_skill_manage must block %s agent skills handled=%v isErr=%v err=%v out=%q", tc.want, handled, isErr, err, out)
		}
		if _, err := os.Stat(filepath.Join(root, "agents", tc.agent, "skills", tc.skill, "references", "forbidden.md")); !os.IsNotExist(err) {
			t.Fatalf("agent_skill_manage wrote into protected system agent skill path err=%v", err)
		}
	}
}

func TestPrepareLifecycleTurn_MemoryCuratorBeforeRunInjectsRelevantMemory(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent := &models.Agent{
		ID:                  "memory-agent",
		Key:                 "memory_curator",
		Name:                "System: Memory Curator",
		SystemKind:          models.AgentSystemKindMemoryCurator,
		SelectableAsPrimary: false,
		Tools:               []string{models.AgentToolScopedFiles},
	}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create memory curator: %v", err)
	}

	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "memory-recall", AgentID: agent.ID, When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(_ context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		if hook.SkillKey != "recall_memory" || in.ProjectID != "project-1" {
			t.Fatalf("unexpected memory hook input hook=%#v input=%#v", hook, in)
		}
		return memoryContextBlockPayload("Remember to preserve repo-local managed memory.", "MEMORIES.md", "managed_memory.md"), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleAgentRepo(agentRepo)
	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-memory", ProjectID: "project-1", Title: "Need memory", Prompt: "Use relevant context"})
	instructions := additionalProjectInstructionsFromContext(turn.Ctx)
	if !strings.Contains(instructions, "Remember to preserve repo-local managed memory.") || !strings.Contains(instructions, "[recall_memory]") {
		t.Fatalf("expected Memory Curator before_run context in task prompt, got:\n%s", instructions)
	}
}

func TestPrepareLifecycleTurn_MemoryConsolidationTaskRoutesConsolidateMemorySkill(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLifecycleTestSkill(t, root, "memory_curator", "recall_memory", "recall body")
	writeLifecycleTestSkill(t, root, "memory_curator", "update_memory", "update body")
	writeLifecycleTestSkill(t, root, "memory_curator", "consolidate_memory", "consolidate body")

	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent := &models.Agent{
		ID:                  "memory-agent",
		Key:                 "memory_curator",
		Name:                "System: Memory Curator",
		SystemKind:          models.AgentSystemKindMemoryCurator,
		SelectableAsPrimary: false,
		Tools:               []string{models.AgentToolScopedFiles},
	}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create memory curator: %v", err)
	}

	var routeCalled bool
	var available any
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		routeCalled = true
		available = in.Extras["available_skills"]
		return routePayload([]string{"consolidate_memory"}, 0.95), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))
	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "memory-task", Title: "System: Memory Consolidation", Category: models.CategoryScheduled, AgentDefinitionID: &agent.ID})
	if !routeCalled {
		t.Fatal("assigned system tasks should run skill routing")
	}
	if availableText, _ := available.(string); !strings.Contains(availableText, "memory_curator/consolidate_memory") || strings.Contains(availableText, "other_skill") {
		t.Fatalf("route_task should receive Memory Curator skill index, got:\n%s", availableText)
	}
	instructions := additionalProjectInstructionsFromContext(turn.Ctx)
	if !strings.Contains(instructions, "consolidate_memory") || strings.Contains(instructions, "recall_memory") || strings.Contains(instructions, "update_memory") {
		t.Fatalf("scheduled Memory Curator task should include only router-selected consolidate_memory skill, got:\n%s", instructions)
	}
	rt := llmcontracts.RuntimeToolsFromContext(turn.Ctx)
	out, handled, isErr, err := rt.Executor(context.Background(), "skill_view", json.RawMessage(`{"handle":"consolidate_memory"}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "consolidate body") {
		t.Fatalf("selected consolidate_memory skill_view failed handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
}

func TestPrepareLifecycleTurn_AssignedAgentWithNoSkillsRoutesEmptyIndex(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent := &models.Agent{
		ID:                  "memory-agent",
		Key:                 "memory_curator",
		Name:                "System: Memory Curator",
		SystemKind:          models.AgentSystemKindMemoryCurator,
		SelectableAsPrimary: false,
		Tools:               []string{models.AgentToolScopedFiles},
	}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create memory curator: %v", err)
	}

	var routeCalled bool
	var available any
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		routeCalled = true
		available = in.Extras["available_skills"]
		return routePayload([]string{"maintain_skill_library"}, 0.9), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleAgentRepo(agentRepo)
	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "memory-task", Title: "System: Memory Consolidation", Category: models.CategoryScheduled, AgentDefinitionID: &agent.ID})
	if !routeCalled {
		t.Fatal("assigned system tasks should still run skill routing")
	}
	if availableText, _ := available.(string); !strings.Contains(availableText, "No skills indexed for assigned agent") {
		t.Fatalf("expected empty assigned-agent skill index, got:\n%s", availableText)
	}
	if got := additionalProjectInstructionsFromContext(turn.Ctx); got != "" {
		t.Fatalf("no selected skills should produce no skill prompt, got:\n%s", got)
	}
}

func assertTaskRuntimeIsSelectedSkillOnly(t *testing.T, rt *llmcontracts.RuntimeTools) {
	t.Helper()
	if rt == nil || !rt.HasDefinition("skill_view") {
		t.Fatalf("expected task skill_view runtime tools, got %#v", rt)
	}
	for _, denied := range []string{"skill_manage", "agent_manage", "skills_list", "agent_list", "agent_view"} {
		if rt.HasDefinition(denied) {
			t.Fatalf("task runtime tools must not expose %s, got %#v", denied, rt.Definitions)
		}
		if allow, handled := rt.Filter(denied); allow || handled {
			t.Fatalf("task runtime filter must not own %s", denied)
		}
	}
}

func TestPrepareLifecycleTurn_RouteHookDoesNotExposeMutationTools(t *testing.T) {
	root := t.TempDir()
	writeLifecycleStandaloneSkill(t, root, "task_skill", "task skill body")

	var routeTools *llmcontracts.RuntimeTools
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		routeTools = llmcontracts.RuntimeToolsFromContext(ctx)
		return routePayload([]string{"task_skill"}, 0.9), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.PrepareLifecycleTurn(context.Background(), models.Task{ID: "task-route-tools"})
	if routeTools == nil || !routeTools.HasDefinition("skill_view") || !routeTools.HasDefinition("skills_list") {
		t.Fatalf("expected standalone route hook read tools, got %#v", routeTools)
	}
	if routeTools.HasDefinition("skill_manage") || routeTools.HasDefinition("agent_manage") {
		t.Fatalf("route hook must not expose mutation tools, got %#v", routeTools.Definitions)
	}
}

func TestPrepareLifecycleTurn_AssignedAgentRouteHookDoesNotExposeStandaloneSkillsList(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLifecycleTestSkill(t, root, "task_agent", "task_skill", "task skill body")
	writeLifecycleStandaloneSkill(t, root, "standalone_skill", "standalone body")

	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent := &models.Agent{ID: "task-agent-id", Key: "task_agent", Name: "Task Agent", Enabled: true, Tools: []string{"skill_view", "skills_list"}}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create task agent: %v", err)
	}

	var routeTools *llmcontracts.RuntimeTools
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		routeTools = llmcontracts.RuntimeToolsFromContext(ctx)
		return routePayload([]string{"task_skill"}, 0.9), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-route-agent-tools", AgentDefinitionID: &agent.ID})
	if routeTools == nil || !routeTools.HasDefinition("skill_view") {
		t.Fatalf("expected assigned-agent route hook skill_view, got %#v", routeTools)
	}
	if routeTools.HasDefinition("skills_list") || routeTools.HasDefinition("agent_list") || routeTools.HasDefinition("agent_view") || routeTools.HasDefinition("skill_manage") {
		t.Fatalf("assigned-agent route hook must expose only scoped skill_view, got %#v", routeTools.Definitions)
	}
	out, handled, isErr, err := routeTools.Executor(ctx, "skill_view", json.RawMessage(`{"handle":"task_skill"}`))
	if !handled || err != nil || isErr || !strings.Contains(out, "task skill body") {
		t.Fatalf("expected assigned-agent skill_view to load task skill handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
}

func writeLifecycleTestSkill(t *testing.T, root, agent, skill, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "agents", agent, "skills", skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", agent, "skills", skill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	appendLifecycleTestHeader(t, filepath.Join(root, "agents", "AGENTS.md"), agent)
	appendLifecycleTestHeader(t, filepath.Join(root, "agents", agent, "SKILLS.md"), agent+"/"+skill)
}

func writeLifecycleStandaloneSkill(t *testing.T, root, skill, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "skills", skill), 0o755); err != nil {
		t.Fatal(err)
	}
	content := body
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		content = "---\nkind: openvibely.agent_skill\nversion: 1\nskill:\n  key: " + skill + "\n  scope: global\n---\n" + body
	}
	if err := os.WriteFile(filepath.Join(root, "skills", skill, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	appendLifecycleTestHeader(t, filepath.Join(root, "skills", "SKILLS.md"), skill)
}

func appendLifecycleTestHeader(t *testing.T, path, header string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("## " + header + "\n\n"); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLifecycleTurn_AfterCompleteDoesNotPreExposeScopedFileTools(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	hookAgent := &models.Agent{
		Key:     "custom_memory_hook",
		Name:    "Custom Memory Hook",
		Enabled: true,
		Tools:   []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{ScopedFiles: []models.ScopedFilesConfig{{
			Directory:   ".openvibely/custom-memory",
			Permissions: []string{"read", "write"},
		}}},
	}
	if err := agentRepo.Create(ctx, hookAgent); err != nil {
		t.Fatalf("create hook agent: %v", err)
	}

	done := make(chan error, 1)
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{{
		ID:             "custom-memory-update",
		AgentID:        hookAgent.ID,
		When:           models.LifecycleAfterComplete,
		SkillKey:       "update_custom_memory",
		OutputContract: models.OutputContractActivitySummary,
		Enabled:        true,
	}}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		rt := llmcontracts.RuntimeToolsFromContext(ctx)
		if rt != nil && rt.HasDefinition("write_file") {
			done <- fmt.Errorf("after_complete worker context must not pre-expose scoped file tools before per-agent direct-call setup: %#v", rt.Definitions)
			return json.RawMessage(`{"summary":"bad","skipped":true,"skip_reason":"pre-exposed tools"}`), nil
		}
		done <- nil
		return json.RawMessage(`{"summary":"ok","changed_paths":[]}`), nil
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))

	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-hook-scoped-files", ProjectID: "default"})
	turn.AfterComplete(nil, llmcontracts.ChatContext{})

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for after_complete hook")
	}
}

func TestPrepareLifecycleTurn_AfterCompleteIncludesAssignedAgentLearningContextAndTool(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLifecycleTestSkill(t, root, "reviewer", "review_migrations", "migration review body")

	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	agent := &models.Agent{ID: "reviewer-id", Key: "reviewer", Name: "Reviewer", Description: "Reviews code changes", Enabled: true, Scope: models.AgentScopeProject, Tools: []string{"skill_view"}}
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	done := make(chan error, 1)
	store := &routeHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "route", When: models.LifecycleRouteTask, SkillKey: "route_task", OutputContract: models.OutputContractSelectedSkills, Blocking: true, Enabled: true},
		{ID: "learn", When: models.LifecycleAfterComplete, SkillKey: "observe_task_for_learning", OutputContract: models.OutputContractLearningSummary, Enabled: true},
	}}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		switch hook.ID {
		case "route":
			return routePayload([]string{"review_migrations"}, 0.9), nil
		case "learn":
			rt := llmcontracts.RuntimeToolsFromContext(ctx)
			if rt == nil || !rt.HasDefinition("agent_skill_manage") || !rt.HasDefinition("skill_manage") {
				done <- fmt.Errorf("expected learning mutation tools, got %#v", rt)
				return json.RawMessage(`{"summary":"bad","nothing_to_save":true}`), nil
			}
			raw, ok := in.Extras[lifecycle.LearningSnapshotKey].(lifecycle.LearningInputSnapshot)
			if !ok {
				done <- fmt.Errorf("missing learning snapshot: %#v", in.Extras[lifecycle.LearningSnapshotKey])
				return json.RawMessage(`{"summary":"bad","nothing_to_save":true}`), nil
			}
			if raw.AssignedAgent == nil || raw.AssignedAgent.Key != "reviewer" || raw.AssignedAgent.Description != "Reviews code changes" {
				done <- fmt.Errorf("bad assigned agent context: %+v", raw.AssignedAgent)
				return json.RawMessage(`{"summary":"bad","nothing_to_save":true}`), nil
			}
			if len(raw.SelectedAgentSkills) != 1 || raw.SelectedAgentSkills[0].Handle != "review_migrations" || raw.SelectedAgentSkills[0].Owner != "assigned_agent" {
				done <- fmt.Errorf("bad selected agent skills: %+v", raw.SelectedAgentSkills)
				return json.RawMessage(`{"summary":"bad","nothing_to_save":true}`), nil
			}
			if len(raw.SelectedStandaloneSkills) != 0 || !stringSliceContains(raw.SkillWritePolicy, "Use agent_skill_manage only for changes specific to the assigned agent's role, workflow, or selected agent-owned skills.") {
				done <- fmt.Errorf("bad write policy/scope: standalone=%+v policy=%+v", raw.SelectedStandaloneSkills, raw.SkillWritePolicy)
				return json.RawMessage(`{"summary":"bad","nothing_to_save":true}`), nil
			}
			done <- nil
			return json.RawMessage(`{"summary":"ok","nothing_to_save":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected hook %s", hook.ID)
		}
	}), nil)

	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(runner)
	worker.SetLifecycleSkillRoot(root)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRepo(repository.NewLifecycleRepo(db))

	turn := worker.PrepareLifecycleTurn(ctx, models.Task{ID: "task-learning", ProjectID: "default", AgentDefinitionID: &agent.ID})
	turn.AfterComplete(nil, llmcontracts.ChatContext{})

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for after_complete hook")
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAfterCompleteEligibilityRunsProtectedGoalAgentOnlyForActiveGoal(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	goalSvc := NewTaskGoalService(repository.NewTaskGoalRepo(db), taskRepo, nil)
	w := &WorkerService{agentRepo: agentRepo, taskGoalSvc: goalSvc}

	project := &models.Project{Name: "Goal Eligibility Project"}
	if err := repository.NewProjectRepo(db).Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := models.Task{ID: "task-goal-eligibility", ProjectID: project.ID, Title: "Goal eligibility", Prompt: "work", Category: models.CategoryActive, Status: models.StatusRunning}
	if err := taskRepo.Create(ctx, &task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	otherHook := models.AgentLifecycleHook{ID: "other-hook", AgentID: "other-agent", When: models.LifecycleAfterComplete, SkillKey: "evaluate_task_goal", Enabled: true}
	if !w.afterCompleteHookEligible(ctx, task)(otherHook) {
		t.Fatal("generic after_complete must continue to run non-goal hooks sharing the evaluate_task_goal skill key")
	}

	goalAgent := &models.Agent{Key: models.AgentSystemKindGoal, Name: "System: Goal Agent", Model: "inherit", SystemKind: models.AgentSystemKindGoal, GeneratedStatus: models.AgentStatusProtected, CreatedBy: models.AgentCreatedBySystem, Enabled: true}
	if err := agentRepo.Create(ctx, goalAgent); err != nil {
		t.Fatalf("create goal agent: %v", err)
	}
	goalHook := models.AgentLifecycleHook{ID: "goal-hook", AgentID: goalAgent.ID, When: models.LifecycleAfterComplete, SkillKey: "evaluate_task_goal", Enabled: true}
	if w.afterCompleteHookEligible(ctx, task)(goalHook) {
		t.Fatal("protected Goal Agent hook must not run when the task has no active goal")
	}
	goal, err := goalSvc.SetGoal(ctx, task.ID, "Keep the goal active", GoalOptions{})
	if err != nil {
		t.Fatalf("set goal: %v", err)
	}
	if w.afterCompleteHookEligible(ctx, task)(goalHook) {
		t.Fatal("protected Goal Agent hook must honor task_thread_only policy and skip ordinary worker turns")
	}
	taskThreadCtx := WithTaskThreadLifecycleTurn(ctx)
	if !w.afterCompleteHookEligible(taskThreadCtx, task)(goalHook) {
		t.Fatal("protected Goal Agent hook must run through generic after_complete for task-thread turns with an active goal")
	}
	_, err = goalSvc.MarkAchieved(ctx, task.ID, goal.GoalID, "done")
	if err != nil {
		t.Fatalf("mark achieved: %v", err)
	}
	if w.afterCompleteHookEligible(taskThreadCtx, task)(goalHook) {
		t.Fatal("protected Goal Agent hook must not run when the goal is no longer active")
	}
}
