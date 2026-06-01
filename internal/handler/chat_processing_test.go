package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/events"
	"github.com/openvibely/openvibely/internal/lifecycle"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
	"github.com/openvibely/openvibely/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type chatMemoryHookStore struct {
	hooks []models.AgentLifecycleHook
	seen  []models.LifecycleWhen
}

func (s *chatMemoryHookStore) HooksForWhen(ctx context.Context, when models.LifecycleWhen) ([]models.AgentLifecycleHook, error) {
	s.seen = append(s.seen, when)
	var out []models.AgentLifecycleHook
	for _, h := range s.hooks {
		if h.When == when && h.Enabled {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *chatMemoryHookStore) CreateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	if e.ID == "" {
		e.ID = "chat-memory-" + string(e.When)
	}
	return nil
}

func (s *chatMemoryHookStore) UpdateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	return nil
}

func (s *chatMemoryHookStore) FindExecutionByIdempotencyKey(ctx context.Context, key string) (*models.LifecycleExecution, error) {
	return nil, os.ErrNotExist
}

type chatMemoryHookInvoker struct {
	seen []string
}

func (i *chatMemoryHookInvoker) Invoke(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
	i.seen = append(i.seen, string(hook.When)+"/"+hook.SkillKey)
	if hook.When == models.LifecycleBeforeRun {
		return json.Marshal(lifecycle.ContextBlock{Content: "Remember: prefer repo-local managed memory for this project.", Sources: []string{"MEMORIES.md"}, Confidence: 0.9})
	}
	return json.Marshal(lifecycle.ActivitySummary{Summary: "updated chat memory", ChangedPaths: []string{"chat.md"}})
}

func createHandlerTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit failed: %v\n%s", err, out)
	}

	return dir
}

func TestBuildThreadSystemContext_WithHistory_DoesNotIncludeTaskPrompt(t *testing.T) {
	// When there is prior conversation history, the system context should NOT
	// include the original task prompt because it's already in the conversation
	// history as the first user message. Re-injecting it causes the model to
	// restart work from scratch.
	result := buildThreadSystemContext("Fix login bug", true, "")

	if strings.Contains(result, "Original prompt") {
		t.Error("system context with history should NOT contain 'Original prompt' — it causes model to restart work")
	}
	if strings.Contains(result, "task prompt was") {
		t.Error("system context with history should NOT re-inject the task prompt")
	}
	if !strings.Contains(result, "continue from where you left off") && !strings.Contains(result, "Continue from where you left off") {
		t.Error("system context with history should instruct model to continue, not restart")
	}
	if !strings.Contains(result, "do NOT restart") && !strings.Contains(result, "do not restart") {
		t.Error("system context with history should explicitly say not to restart")
	}
	if !strings.Contains(result, "Fix login bug") {
		t.Error("system context should include the task title for reference")
	}
}

func TestBuildThreadSystemContext_WithoutHistory_NoTaskPrompt(t *testing.T) {
	// When there is no history (first follow-up), the system context should
	// indicate the task prompt follows as the user message.
	result := buildThreadSystemContext("Fix login bug", false, "")

	if strings.Contains(result, "Fix login bug") {
		t.Error("system context without history should not include title (task prompt is the user message)")
	}
	if !strings.Contains(result, "user's message below") {
		t.Error("system context without history should reference the user's message")
	}
}

func TestBuildThreadSystemContext_WithAttachments(t *testing.T) {
	result := buildThreadSystemContext("Fix login bug", true, "Attached file: screenshot.png")

	if !strings.Contains(result, "screenshot.png") {
		t.Error("system context should include attachment context when provided")
	}
}

func TestBuildThreadSystemContext_NoAttachments(t *testing.T) {
	result := buildThreadSystemContext("Fix login bug", true, "")

	// Should not have double newlines from empty attachment context
	if strings.Contains(result, "\n\n\n") {
		t.Error("system context should not have triple newlines when no attachment context")
	}
}

func TestFilterChatHistory_ExcludesRunningAndCurrentExec(t *testing.T) {
	// filterChatHistory should exclude the current execution and running ones,
	// preserving only completed/failed executions for conversation context.
	executions := []models.Execution{
		{ID: "exec1", Status: models.ExecCompleted, PromptSent: "original prompt", Output: "response 1"},
		{ID: "exec2", Status: models.ExecFailed, PromptSent: "follow-up 1", Output: "error msg"},
		{ID: "exec3", Status: models.ExecRunning, PromptSent: "running exec"},
		{ID: "exec4", Status: models.ExecCompleted, PromptSent: "current exec"},
	}

	result := filterChatHistory(executions, "exec4")

	if len(result) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(result))
	}
	if result[0].ID != "exec1" {
		t.Errorf("expected first entry to be exec1, got %s", result[0].ID)
	}
	if result[1].ID != "exec2" {
		t.Errorf("expected second entry to be exec2, got %s", result[1].ID)
	}
}

func TestFilterChatHistory_ReturnsNonNilForEmpty(t *testing.T) {
	// filterChatHistory must return a non-nil slice even when empty,
	// so CallAgentDirectStreaming routes to the chat path.
	result := filterChatHistory([]models.Execution{}, "any-id")

	if result == nil {
		t.Error("filterChatHistory should return non-nil empty slice, not nil")
	}
}

func TestCombineContexts_BothPresent(t *testing.T) {
	result := combineContexts("task context here", "attachment context here")
	if result != "task context here\nattachment context here" {
		t.Errorf("expected combined contexts joined with newline, got %q", result)
	}
}

func TestCombineContexts_OnlyTaskContext(t *testing.T) {
	result := combineContexts("task context only", "")
	if result != "task context only" {
		t.Errorf("expected just task context, got %q", result)
	}
}

func TestCombineContexts_OnlyAttachmentContext(t *testing.T) {
	result := combineContexts("", "attachment context only")
	if result != "attachment context only" {
		t.Errorf("expected just attachment context, got %q", result)
	}
}

func TestCombineContexts_BothEmpty(t *testing.T) {
	result := combineContexts("", "")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestResolveTaskAgentDefinitionForTask_LoadsAssignedDefinition(t *testing.T) {
	h, _, _, db := setupTestHandlerWithDB(t)
	ctx := context.Background()
	agentRepo := repository.NewAgentRepo(db)
	h.SetAgentRepo(agentRepo)

	project := createProject(t, h, "agent-def-project")
	agentDef := &models.Agent{
		Name:         "ui-reviewer",
		Description:  "review ui with playwright",
		SystemPrompt: "Use MCP tools.",
		Model:        "inherit",
		Tools:        []string{"Read", "Bash"},
		Plugins:      []string{"playwright@claude-plugins-official"},
	}
	if err := agentRepo.Create(ctx, agentDef); err != nil {
		t.Fatalf("create agent definition: %v", err)
	}
	task := createTask(t, h, project.ID, "thread task", func(tk *models.Task) {
		tk.AgentDefinitionID = &agentDef.ID
	})

	resolved := h.resolveTaskAgentDefinitionForTask(ctx, task.ID, nil)
	if resolved == nil {
		t.Fatalf("expected resolved agent definition")
	}
	if resolved.ID != agentDef.ID {
		t.Fatalf("expected agent definition id %q, got %q", agentDef.ID, resolved.ID)
	}
}

func TestBuildThreadSystemContext_AttachmentIntegration(t *testing.T) {
	// When attachments are provided, the system context should include them
	// and they should be passed to the LLM as part of the system prompt.
	result := buildThreadSystemContext("Fix CSS bug", true, "\n\n--- Attached Files ---\nFile: screenshot.png")

	if !strings.Contains(result, "screenshot.png") {
		t.Error("system context should include attachment file reference")
	}
	if !strings.Contains(result, "Attached Files") {
		t.Error("system context should include attachment section header")
	}
	if !strings.Contains(result, "continue from where you left off") {
		t.Error("system context with history should still instruct continuation")
	}
}

func TestBuildThreadSystemContext_FollowupDoesNotReInjectTaskPrompt(t *testing.T) {
	// The system context must NOT contain the actual task prompt text when
	// there is history. The task prompt is already in history as the first
	// user message. Re-injecting it causes the model to see it twice and
	// restart from scratch.
	taskTitle := "Implement user authentication"
	result := buildThreadSystemContext(taskTitle, true, "")

	// Should mention the task title for reference
	if !strings.Contains(result, taskTitle) {
		t.Error("system context should include task title for reference")
	}

	// Should explicitly say to continue, not restart
	if !strings.Contains(result, "continue from where you left off") {
		t.Error("system context should instruct to continue from where left off")
	}

	// Should NOT contain any phrase suggesting the prompt is being provided anew
	if strings.Contains(result, "task prompt is provided") {
		t.Error("system context with history should NOT say 'task prompt is provided' — that's for the no-history case")
	}
}

// TestFilterChatHistory_MultiTurnPreservesOrder verifies that filterChatHistory
// maintains chronological order for multi-turn conversations and excludes the
// current and running executions.
func TestFilterChatHistory_MultiTurnPreservesOrder(t *testing.T) {
	executions := []models.Execution{
		{ID: "exec1", Status: models.ExecCompleted, PromptSent: "original prompt", IsFollowup: false},
		{ID: "exec2", Status: models.ExecCompleted, PromptSent: "first followup", IsFollowup: true},
		{ID: "exec3", Status: models.ExecCompleted, PromptSent: "second followup", IsFollowup: true},
		{ID: "exec4", Status: models.ExecRunning, PromptSent: "current followup", IsFollowup: true},
	}

	// Current exec is exec4
	result := filterChatHistory(executions, "exec4")

	if len(result) != 3 {
		t.Fatalf("expected 3 history entries (excluding current running), got %d", len(result))
	}

	// Verify chronological order is preserved
	expectedPrompts := []string{"original prompt", "first followup", "second followup"}
	for i, expected := range expectedPrompts {
		if result[i].PromptSent != expected {
			t.Errorf("entry %d: expected %q, got %q", i, expected, result[i].PromptSent)
		}
	}

	// Verify follow-up flags are preserved
	if result[0].IsFollowup {
		t.Error("first entry should be non-followup (original)")
	}
	if !result[1].IsFollowup {
		t.Error("second entry should be followup")
	}
}

// TestFilterChatHistory_ExcludesMultipleRunning verifies that all running
// executions are filtered out, not just the current one.
func TestFilterChatHistory_ExcludesMultipleRunning(t *testing.T) {
	executions := []models.Execution{
		{ID: "exec1", Status: models.ExecCompleted, PromptSent: "completed1"},
		{ID: "exec2", Status: models.ExecRunning, PromptSent: "orphaned running"},
		{ID: "exec3", Status: models.ExecCompleted, PromptSent: "completed2"},
		{ID: "exec4", Status: models.ExecRunning, PromptSent: "current"},
	}

	result := filterChatHistory(executions, "exec4")

	if len(result) != 2 {
		t.Fatalf("expected 2 entries (only completed), got %d", len(result))
	}
	if result[0].ID != "exec1" || result[1].ID != "exec3" {
		t.Errorf("expected exec1 and exec3, got %s and %s", result[0].ID, result[1].ID)
	}
}

func TestProcessStreamingResponse_InteractiveChatRunsMemoryRecallOnly(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	mock := testutil.NewMockLLMCaller()
	mock.Response = "chat response"
	mock.TextOnly = "chat response"
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Chat Memory Project")
	chatTask := createTask(t, h, project.ID, "Chat host", func(tk *models.Task) {
		tk.Category = models.CategoryChat
		tk.Status = models.StatusPending
		tk.AgentID = &agent.ID
		tk.Prompt = "What should I remember about managed memory?"
	})
	exec := createExec(t, h, chatTask.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = chatTask.Prompt
	})

	store := &chatMemoryHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "other-before", When: models.LifecycleBeforeRun, SkillKey: "load_context", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "recall", When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "update", When: models.LifecycleAfterComplete, SkillKey: "update_memory", OutputContract: models.OutputContractActivitySummary, Blocking: false, Enabled: true},
	}}
	invoker := &chatMemoryHookInvoker{}
	h.workerSvc.SetLifecycleRunner(lifecycle.NewRunner(store, invoker, nil))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         chatTask.ID,
		Message:        chatTask.Prompt,
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "task list context",
		IsTaskFollowup: false,
		ProcessMarkers: false,
		ChatMode:       models.ChatModeOrchestrate,
	})

	if mock.CallCount() != 1 {
		t.Fatalf("expected one chat model call, got %d", mock.CallCount())
	}
	if len(invoker.seen) != 1 || invoker.seen[0] != "before_run/recall_memory" {
		t.Fatalf("expected only before_run recall hook for chat, got %#v", invoker.seen)
	}
	if chatContext := mock.LastAgentRequest().ChatSystemContext; !strings.Contains(chatContext, "Remember: prefer repo-local managed memory for this project.") || !strings.Contains(chatContext, "[recall_memory]") {
		t.Fatalf("expected recalled memory in model-facing chat context, got:\n%s", chatContext)
	}
	for _, when := range store.seen {
		if when == models.LifecycleAfterComplete {
			t.Fatalf("interactive chat must not run after_complete memory extraction, saw slots %#v", store.seen)
		}
	}
	updatedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecCompleted {
		t.Fatalf("expected chat execution completed, got %s", updatedExec.Status)
	}
}

func TestProcessStreamingResponse_InteractiveChatPlanModeRunsMemoryRecallOnly(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)

	mock := testutil.NewMockLLMCaller()
	mock.Response = "<proposed_plan>Plan with memory.</proposed_plan>"
	mock.TextOnly = mock.Response
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Plan Chat Memory Project")
	chatTask := createTask(t, h, project.ID, "Plan chat host", func(tk *models.Task) {
		tk.Category = models.CategoryChat
		tk.Status = models.StatusPending
		tk.AgentID = &agent.ID
		tk.Prompt = "Plan how to update memory-safe chat."
	})
	exec := createExec(t, h, chatTask.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = chatTask.Prompt
	})

	store := &chatMemoryHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "other-before", When: models.LifecycleBeforeRun, SkillKey: "load_context", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "recall", When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "update", When: models.LifecycleAfterComplete, SkillKey: "update_memory", OutputContract: models.OutputContractActivitySummary, Blocking: false, Enabled: true},
	}}
	invoker := &chatMemoryHookInvoker{}
	h.workerSvc.SetLifecycleRunner(lifecycle.NewRunner(store, invoker, nil))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         chatTask.ID,
		Message:        chatTask.Prompt,
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "task list context",
		IsTaskFollowup: false,
		ProcessMarkers: false,
		ChatMode:       models.ChatModePlan,
	})

	if mock.CallCount() != 1 {
		t.Fatalf("expected one plan chat model call, got %d", mock.CallCount())
	}
	if len(invoker.seen) != 1 || invoker.seen[0] != "before_run/recall_memory" {
		t.Fatalf("expected only before_run recall hook for plan chat, got %#v", invoker.seen)
	}
	if chatContext := mock.LastAgentRequest().ChatSystemContext; !strings.Contains(chatContext, "Remember: prefer repo-local managed memory for this project.") || !strings.Contains(chatContext, "[recall_memory]") {
		t.Fatalf("expected recalled memory in model-facing plan chat context, got:\n%s", chatContext)
	}
	for _, when := range store.seen {
		if when == models.LifecycleAfterComplete {
			t.Fatalf("plan chat must not run after_complete memory extraction, saw slots %#v", store.seen)
		}
	}
	updatedExec, err := h.execRepo.GetByID(context.Background(), exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecCompleted {
		t.Fatalf("expected plan chat execution completed, got %s", updatedExec.Status)
	}
}

func TestProcessStreamingResponse_PromotesQueuedTaskThreadInputsFIFO(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Promotion Project")
	task := createTask(t, h, project.ID, "Queued Promotion Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	active := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active"
		ex.IsFollowup = true
	})
	firstQueued := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: project.ID, TaskID: task.ID, AgentConfigID: agent.ID, InputMode: models.ThreadInputModeQueued, Content: "first queued"}
	secondQueued := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: project.ID, TaskID: task.ID, AgentConfigID: agent.ID, InputMode: models.ThreadInputModeQueued, Content: "second queued"}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, firstQueued))
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, secondQueued))

	started := make(chan string, 2)
	release := make(chan struct{})
	var once sync.Once
	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	mock.OnCall = func(_ context.Context, call testutil.MockLLMCall) {
		if call.Prompt == "active" {
			return
		}
		started <- call.Prompt
		once.Do(func() { <-release })
	}
	h.llmSvc.SetLLMCaller(mock)

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         active.ID,
		TaskID:         task.ID,
		Message:        "active",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	select {
	case got := <-started:
		if got != "first queued" {
			t.Fatalf("expected first queued input to start, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first queued turn to start")
	}
	first, _ := h.threadInputRepo.GetByID(ctx, firstQueued.ID)
	second, _ := h.threadInputRepo.GetByID(ctx, secondQueued.ID)
	if first.InputStatus != models.ThreadInputApplied || second.InputStatus != models.ThreadInputPending {
		t.Fatalf("expected first applied and second pending, got first=%s second=%s", first.InputStatus, second.InputStatus)
	}
	close(release)
	select {
	case got := <-started:
		if got != "second queued" {
			t.Fatalf("expected second queued input to start after first, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second queued turn to start")
	}
	for i := 0; i < 20; i++ {
		latestSecond, _ := h.threadInputRepo.GetByID(ctx, secondQueued.ID)
		if latestSecond.InputStatus == models.ThreadInputApplied {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	latestSecond, _ := h.threadInputRepo.GetByID(ctx, secondQueued.ID)
	if latestSecond.InputStatus != models.ThreadInputApplied {
		t.Fatalf("expected second queued input to apply, got %s", latestSecond.InputStatus)
	}
}

func TestProcessStreamingResponse_SteeredQueuedInputRunsBeforeRemainingQueuedTurns(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Steered Queue Priority Project")
	task := createTask(t, h, project.ID, "Steered Queue Priority Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	active := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active"
		ex.IsFollowup = true
	})
	firstQueued := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: project.ID, TaskID: task.ID, RunExecutionID: active.ID, AgentConfigID: agent.ID, InputMode: models.ThreadInputModeQueued, Content: "first queued"}
	steeredQueued := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: project.ID, TaskID: task.ID, RunExecutionID: active.ID, AgentConfigID: agent.ID, InputMode: models.ThreadInputModeQueued, Content: "steered queued"}
	thirdQueued := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: project.ID, TaskID: task.ID, RunExecutionID: active.ID, AgentConfigID: agent.ID, InputMode: models.ThreadInputModeQueued, Content: "third queued"}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, firstQueued))
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, steeredQueued))
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, thirdQueued))

	calls := make(chan string, 4)
	releasePromoted := make(chan struct{})
	var convertOnce sync.Once
	var blockPromotedOnce sync.Once
	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	mock.OnCall = func(_ context.Context, call testutil.MockLLMCall) {
		calls <- call.Prompt
		if call.Prompt == "active" {
			convertOnce.Do(func() {
				converted, err := h.threadInputRepo.ConvertQueuedToSteering(ctx, steeredQueued.ID, active.ID, active.ID)
				require.NoError(t, err)
				require.NotNil(t, converted)
			})
			return
		}
		if strings.Contains(call.Prompt, "steered queued") {
			return
		}
		blockPromotedOnce.Do(func() { <-releasePromoted })
	}
	h.llmSvc.SetLLMCaller(mock)

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         active.ID,
		TaskID:         task.ID,
		Message:        "active",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	var seen []string
	deadline := time.After(2 * time.Second)
	for len(seen) < 3 {
		select {
		case prompt := <-calls:
			seen = append(seen, prompt)
		case <-deadline:
			t.Fatalf("timed out waiting for active steering and first promoted call, saw %#v", seen)
		}
	}
	if seen[0] != "active" || !strings.Contains(seen[1], "steered queued") || seen[2] != "first queued" {
		t.Fatalf("expected steered queued input to run before FIFO promotion, got %#v", seen)
	}
	if strings.Contains(seen[1], "latest user instruction") || strings.Contains(seen[1], "Start the next visible assistant text") {
		t.Fatalf("expected steered queued input without wrapper text, got %q", seen[1])
	}

	steered, err := h.threadInputRepo.GetByID(ctx, steeredQueued.ID)
	require.NoError(t, err)
	first, err := h.threadInputRepo.GetByID(ctx, firstQueued.ID)
	require.NoError(t, err)
	third, err := h.threadInputRepo.GetByID(ctx, thirdQueued.ID)
	require.NoError(t, err)
	if steered.InputStatus != models.ThreadInputApplied || steered.InputMode != models.ThreadInputModeSteering || steered.RunExecutionID != active.ID {
		t.Fatalf("expected steered row applied to active execution, got %#v", steered)
	}
	if first.InputStatus != models.ThreadInputApplied {
		t.Fatalf("expected first queued row to promote after steered turn completed, got %#v", first)
	}
	if third.InputStatus != models.ThreadInputPending || third.InputMode != models.ThreadInputModeQueued {
		t.Fatalf("expected remaining queued row to stay queued until promoted turn completes, got %#v", third)
	}
	close(releasePromoted)
}

func TestStartQueuedChatInputProcessesSavedAttachmentSession(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Chat Attachment Project")
	activeTask := createTask(t, h, project.ID, "Active Chat", func(tk *models.Task) {
		tk.Category = models.CategoryChat
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	activeExec := createExec(t, h, activeTask.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active chat"
	})

	tmpDir := t.TempDir()
	oldUploadsDir := uploadsDir
	uploadsDir = tmpDir
	defer func() { uploadsDir = oldUploadsDir }()

	sessionID := "queued-chat-attachments"
	pendingDir := filepath.Join(tmpDir, "chat", "pending", sessionID)
	require.NoError(t, os.MkdirAll(pendingDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "notes.txt"), []byte("queued text attachment"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "diagram.png"), []byte("fake-png"), 0644))

	input := &models.ThreadInput{
		Scope:               models.ThreadInputScopeChat,
		ProjectID:           project.ID,
		RunExecutionID:      activeExec.ID,
		AgentConfigID:       agent.ID,
		InputMode:           models.ThreadInputModeQueued,
		InputStatus:         models.ThreadInputPending,
		Content:             "review queued attachments",
		AttachmentSessionID: sessionID,
		ChatMode:            models.ChatModeOrchestrate,
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))

	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	h.llmSvc.SetLLMCaller(mock)
	cb := events.NewChatBroadcaster()
	h.SetChatBroadcaster(cb)
	sub, err := cb.Subscribe()
	require.NoError(t, err)
	defer cb.Unsubscribe(sub)

	h.startQueuedChatInput(ctx, *input)
	require.Eventually(t, func() bool { return mock.CallCount() == 1 }, 2*time.Second, 25*time.Millisecond)

	var newMessage events.ChatEvent
	select {
	case newMessage = <-sub:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for promoted chat new-message event")
	}
	require.Equal(t, events.ChatNewMessage, newMessage.Type)
	require.Equal(t, input.ProjectID, newMessage.ProjectID)
	require.NotEqual(t, input.ID, newMessage.ExecID)
	require.Equal(t, input.ID, newMessage.PendingInputID)
	require.False(t, newMessage.Queued)

	request := mock.LastAgentRequest()
	require.Len(t, request.Attachments, 1)
	require.Equal(t, "diagram.png", request.Attachments[0].FileName)
	require.Contains(t, request.ChatSystemContext, "queued text attachment")
	attachments, err := h.chatAttachmentRepo.ListByExecutionIDs(ctx, []string{request.ExecID})
	require.NoError(t, err)
	require.Len(t, attachments[request.ExecID], 2)
}

func TestStartQueuedChatInputFallsBackWhenQueuedAgentDeleted(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	queuedAgent := createAgent(t, llmConfigRepo, func(a *models.LLMConfig) {
		a.Name = "Queued Deleted Agent"
		a.IsDefault = false
	})
	fallbackAgent := createAgent(t, llmConfigRepo, func(a *models.LLMConfig) {
		a.Name = "Fallback Agent"
		a.IsDefault = true
	})
	project := createProject(t, h, "Queued Deleted Agent Project")
	activeTask := createTask(t, h, project.ID, "Active Chat", func(tk *models.Task) {
		tk.Category = models.CategoryChat
		tk.Status = models.StatusRunning
		tk.AgentID = &fallbackAgent.ID
	})
	activeExec := createExec(t, h, activeTask.ID, fallbackAgent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active chat"
	})
	input := &models.ThreadInput{
		Scope:          models.ThreadInputScopeChat,
		ProjectID:      project.ID,
		RunExecutionID: activeExec.ID,
		AgentConfigID:  queuedAgent.ID,
		InputMode:      models.ThreadInputModeQueued,
		InputStatus:    models.ThreadInputPending,
		Content:        "queued after model deletion",
		ChatMode:       models.ChatModeOrchestrate,
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))
	require.NoError(t, llmConfigRepo.Delete(ctx, queuedAgent.ID))
	input.AgentConfigID = ""

	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	h.llmSvc.SetLLMCaller(mock)

	h.startQueuedChatInput(ctx, *input)
	require.Eventually(t, func() bool { return mock.CallCount() == 1 }, 2*time.Second, 25*time.Millisecond)
	request := mock.LastAgentRequest()
	require.Equal(t, fallbackAgent.ID, request.Agent.ID)
	updated, err := h.threadInputRepo.GetByID(ctx, input.ID)
	require.NoError(t, err)
	require.Equal(t, models.ThreadInputApplied, updated.InputStatus)
}

func TestStartQueuedTaskThreadInputCancelsQueuedInputWhenNoModelAvailable(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Missing Agent Project")
	task := createTask(t, h, project.ID, "Queued Missing Agent Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	activeExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active task"
		ex.IsFollowup = true
	})
	input := &models.ThreadInput{Scope: models.ThreadInputScopeTask, ProjectID: project.ID, TaskID: task.ID, RunExecutionID: activeExec.ID, AgentConfigID: agent.ID, InputMode: models.ThreadInputModeQueued, InputStatus: models.ThreadInputPending, Content: "queued with deleted model"}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))
	agents, err := llmConfigRepo.List(ctx)
	require.NoError(t, err)
	for _, cfg := range agents {
		require.NoError(t, llmConfigRepo.Delete(ctx, cfg.ID))
	}
	input.AgentConfigID = ""

	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	h.llmSvc.SetLLMCaller(mock)

	h.startQueuedTaskThreadInput(ctx, *input)
	updated, err := h.threadInputRepo.GetByID(ctx, input.ID)
	require.NoError(t, err)
	require.Equal(t, models.ThreadInputCancelled, updated.InputStatus)
	require.Equal(t, 0, mock.CallCount())
}

func TestStartQueuedTaskThreadInputUsesQueuedChannelReplyContext(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Task Channel Reply Project")
	task := createTask(t, h, project.ID, "Queued Channel Reply Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	activeExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active task"
		ex.IsFollowup = true
	})
	input := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: activeExec.ID,
		AgentConfigID:  agent.ID,
		InputMode:      models.ThreadInputModeQueued,
		InputStatus:    models.ThreadInputPending,
		Content:        "queued from slack",
		Source:         models.TaskOriginSlack,
		SlackChannelID: "C1",
		SlackThreadTS:  "1710000000.100000",
		SlackUserID:    "U1",
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))

	var sentChannel, sentThread, sentTitle, sentOutput, sentErr, sentUser string
	h.SetSlackService(&fakeSlackService{taskCompletionFn: func(_ context.Context, channelID, threadTS, taskTitle, output, errMsg, userID string) {
		sentChannel = channelID
		sentThread = threadTS
		sentTitle = taskTitle
		sentOutput = output
		sentErr = errMsg
		sentUser = userID
	}})
	mock := testutil.NewMockLLMCaller()
	mock.Response = "queued task done"
	mock.TextOnly = "queued task done"
	h.llmSvc.SetLLMCaller(mock)

	h.startQueuedTaskThreadInput(ctx, *input)
	require.Eventually(t, func() bool { return mock.CallCount() == 1 }, 2*time.Second, 25*time.Millisecond)
	require.Eventually(t, func() bool { return sentChannel == "C1" }, 2*time.Second, 25*time.Millisecond)
	require.Equal(t, "1710000000.100000", sentThread)
	require.Equal(t, "Queued Channel Reply Task", sentTitle)
	require.Equal(t, "queued task done", sentOutput)
	require.Empty(t, sentErr)
	require.Equal(t, "U1", sentUser)
	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.NotEqual(t, models.TaskOriginSlack, updatedTask.CreatedVia)
}

func TestStartQueuedTaskThreadInputMovesCompletedTaskBackToActive(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Task Reactivation Project")
	task := createTask(t, h, project.ID, "Queued Reactivated Task", func(tk *models.Task) {
		tk.Category = models.CategoryCompleted
		tk.Status = models.StatusCompleted
		tk.AgentID = &agent.ID
	})
	activeExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecCompleted
		ex.PromptSent = "previous run"
		ex.IsFollowup = true
	})
	input := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: activeExec.ID,
		AgentConfigID:  agent.ID,
		InputMode:      models.ThreadInputModeQueued,
		InputStatus:    models.ThreadInputPending,
		Content:        "queued follow-up after completion",
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))

	started := make(chan struct{})
	release := make(chan struct{})
	mock := testutil.NewMockLLMCaller()
	mock.Response = "reactivated task done"
	mock.TextOnly = "reactivated task done"
	mock.OnCall = func(ctx context.Context, _ testutil.MockLLMCall) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	h.llmSvc.SetLLMCaller(mock)
	broadcaster := events.NewBroadcaster()
	h.broadcaster = broadcaster
	sub, err := broadcaster.Subscribe()
	require.NoError(t, err)
	defer broadcaster.Unsubscribe(sub)

	h.startQueuedTaskThreadInput(ctx, *input)
	var startEvent events.TaskEvent
	select {
	case startEvent = <-sub:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued task-thread start event")
	}
	require.Equal(t, events.TaskThreadExecutionStarted, startEvent.Type)
	require.Equal(t, task.ID, startEvent.TaskID)
	require.Equal(t, input.ID, startEvent.PendingInputID)
	require.NotEmpty(t, startEvent.ExecID)
	require.Equal(t, input.Content, startEvent.Message)

	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, 2*time.Second, 25*time.Millisecond)
	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, models.CategoryActive, updatedTask.Category)
	close(release)
}

func TestStartQueuedTaskThreadInputFailureUsesQueuedChannelReplyContext(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Queued Task Failure Reply Project", RepoPath: repoDir}
	require.NoError(t, h.projectSvc.Create(ctx, project))
	task := createTask(t, h, project.ID, "Queued Failure Reply Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, wtBranch, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	require.NoError(t, err)
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, ".git"), []byte("not a gitdir"), 0644))
	activeExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecCompleted
		ex.PromptSent = "previous run"
		ex.IsFollowup = true
	})
	input := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: activeExec.ID,
		AgentConfigID:  agent.ID,
		InputMode:      models.ThreadInputModeQueued,
		InputStatus:    models.ThreadInputPending,
		Content:        "queued from slack",
		Source:         models.TaskOriginSlack,
		SlackChannelID: "Cfail",
		SlackThreadTS:  "1710000000.200000",
		SlackUserID:    "Ufail",
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))

	var sentChannel, sentThread, sentErr, sentUser string
	h.SetSlackService(&fakeSlackService{taskCompletionFn: func(_ context.Context, channelID, threadTS, taskTitle, output, errMsg, userID string) {
		sentChannel = channelID
		sentThread = threadTS
		sentErr = errMsg
		sentUser = userID
	}})

	h.startQueuedTaskThreadInput(ctx, *input)
	require.Eventually(t, func() bool { return sentChannel == "Cfail" }, 2*time.Second, 25*time.Millisecond)
	require.Equal(t, "1710000000.200000", sentThread)
	require.Contains(t, sentErr, "could not check worktree status")
	require.Equal(t, "Ufail", sentUser)
}

func TestStartChannelTaskRunSetupFailureUsesReplyContext(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Immediate Channel Failure Reply Project", RepoPath: repoDir}
	require.NoError(t, h.projectSvc.Create(ctx, project))
	task := createTask(t, h, project.ID, "Immediate Failure Reply Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, wtBranch, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	require.NoError(t, err)
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, ".git"), []byte("not a gitdir"), 0644))
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "channel follow-up"
		ex.IsFollowup = true
	})

	var sentChannel, sentThread, sentErr, sentUser string
	h.SetSlackService(&fakeSlackService{taskCompletionFn: func(_ context.Context, channelID, threadTS, taskTitle, output, errMsg, userID string) {
		sentChannel = channelID
		sentThread = threadTS
		sentErr = errMsg
		sentUser = userID
	}})

	h.StartChannelTaskRun(ctx, service.ChannelTaskRunRequest{
		ExecID:    exec.ID,
		TaskID:    task.ID,
		ProjectID: project.ID,
		Message:   "channel follow-up",
		Agent:     *agent,
		Surface:   "slack",
		ReplyContext: service.ChannelReplyContext{
			Source:         models.TaskOriginSlack,
			SlackChannelID: "Cimmediate",
			SlackThreadTS:  "1710000000.300000",
			SlackUserID:    "Uimmediate",
		},
	})
	require.Eventually(t, func() bool { return sentChannel == "Cimmediate" }, 2*time.Second, 25*time.Millisecond)
	require.Equal(t, "1710000000.300000", sentThread)
	require.Contains(t, sentErr, "could not check worktree status")
	require.Equal(t, "Uimmediate", sentUser)
}

func TestStartQueuedTaskThreadInputProcessesSavedAttachmentSession(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Task Attachment Project")
	task := createTask(t, h, project.ID, "Queued Attachment Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	activeExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active task"
		ex.IsFollowup = true
	})

	tmpDir := t.TempDir()
	oldUploadsDir := uploadsDir
	uploadsDir = tmpDir
	defer func() { uploadsDir = oldUploadsDir }()

	sessionID := "queued-task-attachments"
	pendingDir := filepath.Join(tmpDir, "chat", "pending", sessionID)
	require.NoError(t, os.MkdirAll(pendingDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "instructions.txt"), []byte("queued task attachment"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "screen.png"), []byte("fake-png"), 0644))

	input := &models.ThreadInput{
		Scope:               models.ThreadInputScopeTask,
		ProjectID:           project.ID,
		TaskID:              task.ID,
		RunExecutionID:      activeExec.ID,
		AgentConfigID:       agent.ID,
		InputMode:           models.ThreadInputModeQueued,
		InputStatus:         models.ThreadInputPending,
		Content:             "review queued task attachments",
		AttachmentSessionID: sessionID,
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))

	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	h.llmSvc.SetLLMCaller(mock)
	broadcaster := events.NewBroadcaster()
	h.broadcaster = broadcaster
	sub, err := broadcaster.Subscribe()
	require.NoError(t, err)
	defer broadcaster.Unsubscribe(sub)

	h.startQueuedTaskThreadInput(ctx, *input)

	var started events.TaskEvent
	select {
	case started = <-sub:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for promoted task-thread event")
	}
	require.Equal(t, events.TaskThreadExecutionStarted, started.Type)
	require.Equal(t, input.ID, started.PendingInputID)
	require.NotEmpty(t, started.ExecID)

	fragment := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/thread/executions/"+started.ExecID+"/fragment", nil)
	c := echo.New().NewContext(req, fragment)
	c.SetParamNames("taskId", "execId")
	c.SetParamValues(task.ID, started.ExecID)
	require.NoError(t, h.GetTaskThreadExecutionFragment(c))
	assert.Equal(t, http.StatusOK, fragment.Code)
	assert.Contains(t, fragment.Body.String(), "review queued task attachments")
	assert.Contains(t, fragment.Body.String(), "screen.png")
	assert.Contains(t, fragment.Body.String(), "/chat/attachments/")

	require.Eventually(t, func() bool { return mock.CallCount() == 1 }, 2*time.Second, 25*time.Millisecond)
	request := mock.LastAgentRequest()
	require.Len(t, request.Attachments, 1)
	require.Equal(t, "screen.png", request.Attachments[0].FileName)
	require.Contains(t, request.ChatSystemContext, "queued task attachment")
	attachments, err := h.chatAttachmentRepo.ListByExecutionIDs(ctx, []string{request.ExecID})
	require.NoError(t, err)
	require.Len(t, attachments[request.ExecID], 2)
}

func TestProcessStreamingResponse_AppliesPendingSteeringBeforeModelCall(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "handled steering"
	mock.TextOnly = "handled steering"
	var exec *models.Execution
	var steering *models.ThreadInput
	broadcaster := events.NewBroadcaster()
	h.broadcaster = broadcaster
	sub, err := broadcaster.Subscribe()
	require.NoError(t, err)
	defer broadcaster.Unsubscribe(sub)
	mock.OnCall = func(_ context.Context, _ testutil.MockLLMCall) {
		select {
		case event := <-sub:
			require.Equal(t, events.TaskThreadInputApplied, event.Type)
			require.Equal(t, exec.ID, event.ExecID)
			require.Equal(t, steering.ID, event.PendingInputID)
		default:
			t.Fatal("expected pending steering removal event before provider call")
		}
		stored, err := h.threadInputRepo.GetByID(ctx, steering.ID)
		require.NoError(t, err)
		require.Equal(t, models.ThreadInputPending, stored.InputStatus)
	}
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Steering Project")
	task := createTask(t, h, project.ID, "Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec = createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering = &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "do not change the public API",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	if mock.CallCount() != 1 {
		t.Fatalf("expected one model call, got %d", mock.CallCount())
	}
	request := mock.LastAgentRequest()
	if request.Message == "" {
		t.Fatalf("expected steering request to keep a non-empty current user turn")
	}
	if !strings.Contains(request.Message, "active prompt") || !strings.Contains(request.Message, "do not change the public API") {
		t.Fatalf("expected current request message to combine active prompt and steering, got %q", request.Message)
	}
	if strings.Contains(request.Message, "latest user instruction") || strings.Contains(request.Message, "Start the next visible assistant text") {
		t.Fatalf("expected steering without wrapper text, got %q", request.Message)
	}
	for _, turn := range request.ChatHistory {
		if turn.PromptSent == "do not change the public API" {
			t.Fatalf("steering should be delivered as the current provider message, not trailing user-only history: %#v", request.ChatHistory)
		}
	}
	applied, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	if applied.InputStatus != models.ThreadInputApplied {
		t.Fatalf("expected steering input applied, got %s", applied.InputStatus)
	}
}

func TestClaimPendingTextSteeringInputsSkipsAttachmentSteering(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Text Boundary Steering Attachment Project")
	task := createTask(t, h, project.ID, "Text Boundary Steering Attachment Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})

	textSteer := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "3+2=?",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, textSteer, exec.ID))
	attachmentSteer := &models.ThreadInput{
		Scope:               models.ThreadInputScopeTask,
		ProjectID:           project.ID,
		TaskID:              task.ID,
		RunExecutionID:      exec.ID,
		InputMode:           models.ThreadInputModeSteering,
		InputStatus:         models.ThreadInputPending,
		TurnID:              exec.ID,
		ExpectedTurnID:      exec.ID,
		Content:             "also inspect this screenshot",
		AttachmentSessionID: "attach-session-1",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, attachmentSteer, exec.ID))

	params := streamingResponseParams{ExecID: exec.ID, TaskID: task.ID, ProjectID: project.ID, Agent: *agent, IsTaskFollowup: true}
	batch, err := h.claimPendingTextSteeringInputs(ctx, &params)
	require.NoError(t, err)
	if batch.count() != 1 || batch.inputs[0].ID != textSteer.ID {
		t.Fatalf("prepared text-only batch = %#v, want only text steer %s", batch.inputs, textSteer.ID)
	}

	textStored, err := h.threadInputRepo.GetByID(ctx, textSteer.ID)
	require.NoError(t, err)
	if textStored.ExpectedTurnID != "" {
		t.Fatalf("text steer expected_turn_id = %q, want prepared empty", textStored.ExpectedTurnID)
	}
	attachmentStored, err := h.threadInputRepo.GetByID(ctx, attachmentSteer.ID)
	require.NoError(t, err)
	if attachmentStored.ExpectedTurnID != exec.ID || attachmentStored.InputStatus != models.ThreadInputPending {
		t.Fatalf("attachment steer state = status %s expected_turn_id %q, want pending/unprepared", attachmentStored.InputStatus, attachmentStored.ExpectedTurnID)
	}
}

func TestProcessStreamingResponse_CancelledContextUpdatesTaskStatus(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "partial before cancel"
	mock.TextOnly = "partial before cancel"
	mock.Err = context.Canceled
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Cancelled Shared Runner Project")
	task := createTask(t, h, project.ID, "Cancelled Shared Runner Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	updatedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	require.NoError(t, err)
	require.Equal(t, models.ExecCancelled, updatedExec.Status)
	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusCancelled, updatedTask.Status)
}

func TestProcessStreamingResponse_DoesNotApplyPreparedSteeringWhenProviderCallFails(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "partial output"
	mock.TextOnly = "partial output"
	mock.Err = errors.New("provider failed")
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Prepared Failed Steering Project")
	task := createTask(t, h, project.ID, "Prepared Failed Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "retry this steering",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	require.Equal(t, 1, mock.CallCount())
	require.Contains(t, mock.LastCall().Prompt, "retry this steering")
	require.NotContains(t, mock.LastCall().Prompt, "latest user instruction")
	require.NotContains(t, mock.LastCall().Prompt, "Start the next visible assistant text")
	stillPending, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, stillPending)
	require.Equal(t, models.ThreadInputPending, stillPending.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, stillPending.InputMode)
}

func TestProcessStreamingResponse_RequeuesPendingSteeringWithCancelledContext(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Cancelled Cleanup Steering Project")
	task := createTask(t, h, project.ID, "Cancelled Cleanup Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "recover despite cancelled request context",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))
	prepared, err := h.threadInputRepo.PreparePendingTextSteering(ctx, exec.ID, exec.ID)
	require.NoError(t, err)
	require.Len(t, prepared, 1)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	h.requeuePendingSteeringForExecution(cancelledCtx, exec.ID)

	requeued, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, requeued)
	require.Equal(t, models.ThreadInputPending, requeued.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, requeued.InputMode)
	require.Empty(t, requeued.TurnID)
	require.Empty(t, requeued.ExpectedTurnID)
}

func TestProcessStreamingResponse_RequeuesUncommittedSteeringWithCancelledContext(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Cancelled Uncommitted Steering Project")
	task := createTask(t, h, project.ID, "Cancelled Uncommitted Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "recover uncommitted despite cancelled request context",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))
	prepared, err := h.threadInputRepo.PreparePendingTextSteering(ctx, exec.ID, exec.ID)
	require.NoError(t, err)
	require.Len(t, prepared, 1)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	h.requeueUncommittedSteering(cancelledCtx, exec.ID, preparedSteeringBatch{inputs: prepared})

	requeued, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, requeued)
	require.Equal(t, models.ThreadInputPending, requeued.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, requeued.InputMode)
	require.Empty(t, requeued.TurnID)
	require.Empty(t, requeued.ExpectedTurnID)
}

func TestProcessStreamingResponse_AppliesPreparedSteeringAfterSuccessfulProviderCall(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "steered output"
	mock.TextOnly = "steered output"
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Prepared Successful Steering Project")
	task := createTask(t, h, project.ID, "Prepared Successful Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "apply this steering",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	require.Equal(t, 1, mock.CallCount())
	require.Contains(t, mock.LastCall().Prompt, "apply this steering")
	require.NotContains(t, mock.LastCall().Prompt, "latest user instruction")
	require.NotContains(t, mock.LastCall().Prompt, "Start the next visible assistant text")
	applied, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, applied)
	require.Equal(t, models.ThreadInputApplied, applied.InputStatus)
}

func TestProcessStreamingResponse_DoesNotMovePreparedSteeringAttachmentsWhenProviderCallFails(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	tmpDir := t.TempDir()
	originalUploadsDir := uploadsDir
	uploadsDir = tmpDir
	t.Cleanup(func() { uploadsDir = originalUploadsDir })
	mock := testutil.NewMockLLMCaller()
	mock.Response = "partial output"
	mock.TextOnly = "partial output"
	mock.Err = errors.New("provider failed")
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Prepared Failed Steering Attachments Project")
	task := createTask(t, h, project.ID, "Prepared Failed Steering Attachments Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	sessionID := "steering-session"
	pendingDir := filepath.Join(tmpDir, "chat", "pending", sessionID)
	require.NoError(t, os.MkdirAll(pendingDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "notes.txt"), []byte("steering notes"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "screen.png"), []byte("fake-png"), 0644))
	steering := &models.ThreadInput{
		Scope:               models.ThreadInputScopeTask,
		ProjectID:           project.ID,
		TaskID:              task.ID,
		RunExecutionID:      exec.ID,
		InputMode:           models.ThreadInputModeSteering,
		InputStatus:         models.ThreadInputPending,
		TurnID:              exec.ID,
		ExpectedTurnID:      exec.ID,
		Content:             "use attached steering files",
		AttachmentSessionID: sessionID,
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	require.Equal(t, 1, mock.CallCount())
	require.Contains(t, mock.LastCall().Prompt, "steering notes")
	require.Len(t, mock.LastCall().Attachments, 1)
	require.Equal(t, filepath.Join(pendingDir, "screen.png"), mock.LastCall().Attachments[0].FilePath)
	stillPending, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, stillPending)
	require.Equal(t, models.ThreadInputPending, stillPending.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, stillPending.InputMode)
	require.FileExists(t, filepath.Join(pendingDir, "notes.txt"))
	require.FileExists(t, filepath.Join(pendingDir, "screen.png"))
}

func TestProcessStreamingResponse_RequeuesPreparedSteeringWhenCommitFailsAfterProviderSuccess(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	tmpDir := t.TempDir()
	originalUploadsDir := uploadsDir
	uploadsDir = tmpDir
	t.Cleanup(func() { uploadsDir = originalUploadsDir })
	mock := testutil.NewMockLLMCaller()
	mock.Response = "model used steering"
	mock.TextOnly = "model used steering"
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Prepared Commit Failed Steering Project")
	task := createTask(t, h, project.ID, "Prepared Commit Failed Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	sessionID := "steering-commit-fails"
	pendingDir := filepath.Join(tmpDir, "chat", "pending", sessionID)
	require.NoError(t, os.MkdirAll(pendingDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "notes.txt"), []byte("steering notes"), 0644))
	h.chatAttachmentRepo = nil
	steering := &models.ThreadInput{
		Scope:               models.ThreadInputScopeTask,
		ProjectID:           project.ID,
		TaskID:              task.ID,
		RunExecutionID:      exec.ID,
		InputMode:           models.ThreadInputModeSteering,
		InputStatus:         models.ThreadInputPending,
		TurnID:              exec.ID,
		ExpectedTurnID:      exec.ID,
		Content:             "commit should not strand me",
		AttachmentSessionID: sessionID,
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	require.Equal(t, 1, mock.CallCount())
	require.Contains(t, mock.LastCall().Prompt, "commit should not strand me")
	requeued, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, requeued)
	require.Equal(t, models.ThreadInputPending, requeued.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, requeued.InputMode)
	require.Empty(t, requeued.TurnID)
}

func TestProcessStreamingResponse_RequeuesOnlyUncommittedSteeringWhenLaterCommitFails(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	tmpDir := t.TempDir()
	originalUploadsDir := uploadsDir
	uploadsDir = tmpDir
	t.Cleanup(func() { uploadsDir = originalUploadsDir })
	mock := testutil.NewMockLLMCaller()
	mock.Response = "model used steering"
	mock.TextOnly = "model used steering"
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Mixed Commit Failed Steering Project")
	task := createTask(t, h, project.ID, "Mixed Commit Failed Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	first := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "first steering commits",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, first, exec.ID))
	sessionID := "mixed-steering-commit-fails"
	pendingDir := filepath.Join(tmpDir, "chat", "pending", sessionID)
	require.NoError(t, os.MkdirAll(pendingDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pendingDir, "notes.txt"), []byte("second notes"), 0644))
	second := &models.ThreadInput{
		Scope:               models.ThreadInputScopeTask,
		ProjectID:           project.ID,
		TaskID:              task.ID,
		RunExecutionID:      exec.ID,
		InputMode:           models.ThreadInputModeSteering,
		InputStatus:         models.ThreadInputPending,
		TurnID:              exec.ID,
		ExpectedTurnID:      exec.ID,
		Content:             "second steering recovers",
		AttachmentSessionID: sessionID,
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, second, exec.ID))
	h.chatAttachmentRepo = nil

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	require.Equal(t, 1, mock.CallCount())
	storedFirst, err := h.threadInputRepo.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, storedFirst)
	require.Equal(t, models.ThreadInputApplied, storedFirst.InputStatus)
	require.Equal(t, models.ThreadInputModeSteering, storedFirst.InputMode)
	storedSecond, err := h.threadInputRepo.GetByID(ctx, second.ID)
	require.NoError(t, err)
	require.NotNil(t, storedSecond)
	require.Equal(t, models.ThreadInputPending, storedSecond.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, storedSecond.InputMode)
	require.Empty(t, storedSecond.TurnID)
}

func TestProcessStreamingResponse_DoesNotApplySteeringCreatedDuringFailedModelCall(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "partial output"
	mock.TextOnly = "partial output"
	mock.Err = errors.New("provider failed")
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Failed Steering Project")
	task := createTask(t, h, project.ID, "Failed Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})

	var steeringID string
	mock.OnCall = func(context.Context, testutil.MockLLMCall) {
		steering := &models.ThreadInput{
			Scope:          models.ThreadInputScopeTask,
			ProjectID:      project.ID,
			TaskID:         task.ID,
			RunExecutionID: exec.ID,
			InputMode:      models.ThreadInputModeSteering,
			InputStatus:    models.ThreadInputPending,
			TurnID:         exec.ID,
			ExpectedTurnID: exec.ID,
			Content:        "steering during failed call",
		}
		require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))
		steeringID = steering.ID
	}

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	require.Equal(t, 1, mock.CallCount())
	require.NotEmpty(t, steeringID)
	steering, err := h.threadInputRepo.GetByID(ctx, steeringID)
	require.NoError(t, err)
	require.NotNil(t, steering)
	require.Equal(t, models.ThreadInputPending, steering.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, steering.InputMode)
	require.Empty(t, steering.TurnID)
}

func TestProcessStreamingResponse_AppliesSteeringCreatedDuringFinalGraceBeforeCompletion(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "initial final response"
	mock.TextOnly = "initial final response"
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Late Steering Project")
	task := createTask(t, h, project.ID, "Late Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})

	created := make(chan struct{})
	var once sync.Once
	mock.OnCall = func(context.Context, testutil.MockLLMCall) {
		once.Do(func() {
			go func() {
				time.Sleep(finalSteeringPollInterval)
				steering := &models.ThreadInput{
					Scope:          models.ThreadInputScopeTask,
					ProjectID:      project.ID,
					TaskID:         task.ID,
					RunExecutionID: exec.ID,
					InputMode:      models.ThreadInputModeSteering,
					InputStatus:    models.ThreadInputPending,
					TurnID:         exec.ID,
					ExpectedTurnID: exec.ID,
					Content:        "late steering before completion",
				}
				require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))
				close(created)
			}()
		})
	}

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})
	<-created

	if mock.CallCount() != 2 {
		t.Fatalf("expected late steering to extend the active turn with a second model call, got %d", mock.CallCount())
	}
	finalRequest := mock.LastAgentRequest()
	if !strings.Contains(finalRequest.Message, "late steering before completion") {
		t.Fatalf("expected late steering in provider request, got %q", finalRequest.Message)
	}
	if strings.Contains(finalRequest.Message, "latest user instruction") || strings.Contains(finalRequest.Message, "Start the next visible assistant text") {
		t.Fatalf("expected late steering without wrapper text, got %q", finalRequest.Message)
	}
}

func TestProcessStreamingResponse_MultipleSteeringBatchesDoNotDuplicateActiveContext(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "assistant step one"
	mock.TextOnly = "assistant step one"
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Repeated Steering Project")
	task := createTask(t, h, project.ID, "Repeated Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	first := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "first steering",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, first, exec.ID))

	secondCreated := make(chan struct{})
	var once sync.Once
	mock.OnCall = func(context.Context, testutil.MockLLMCall) {
		once.Do(func() {
			second := &models.ThreadInput{
				Scope:          models.ThreadInputScopeTask,
				ProjectID:      project.ID,
				TaskID:         task.ID,
				RunExecutionID: exec.ID,
				InputMode:      models.ThreadInputModeSteering,
				InputStatus:    models.ThreadInputPending,
				TurnID:         exec.ID,
				ExpectedTurnID: exec.ID,
				Content:        "second steering",
			}
			require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, second, exec.ID))
			close(secondCreated)
		})
	}

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})
	<-secondCreated

	if mock.CallCount() != 2 {
		t.Fatalf("expected two model calls after second steering, got %d", mock.CallCount())
	}
	requests := mock.AgentRequests
	if len(requests) != 2 {
		t.Fatalf("expected two recorded provider requests, got %d", len(requests))
	}
	firstRequest := requests[0]
	if !strings.Contains(firstRequest.Message, "active prompt") || !strings.Contains(firstRequest.Message, "first steering") {
		t.Fatalf("expected first request to combine active prompt and first steering, got %q", firstRequest.Message)
	}
	finalRequest := requests[1]
	finalHistory := finalRequest.ChatHistory
	var firstTurnOutput string
	var duplicateSecondSteering bool
	for _, turn := range finalHistory {
		if strings.Contains(turn.PromptSent, "first steering") {
			firstTurnOutput = turn.Output
		}
		if turn.PromptSent == "second steering" {
			duplicateSecondSteering = true
		}
	}
	if firstTurnOutput != "assistant step one" {
		t.Fatalf("expected first request output attached before second steering, got %q in %#v", firstTurnOutput, finalHistory)
	}
	if duplicateSecondSteering {
		t.Fatalf("second steering should be delivered as current provider message, not trailing user-only history: %#v", finalHistory)
	}
	if !strings.Contains(finalRequest.Message, "second steering") {
		t.Fatalf("expected second steering as current provider message, got %q", finalRequest.Message)
	}
	if strings.Contains(finalRequest.Message, "latest user instruction") || strings.Contains(finalRequest.Message, "Start the next visible assistant text") {
		t.Fatalf("expected second steering without wrapper text, got %q", finalRequest.Message)
	}
}

func TestProcessStreamingResponse_TaskFollowupFailedMarkerMarksTaskFailed(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()

	mock := testutil.NewMockLLMCaller()
	mock.Response = "I couldn't finish this.\n[STATUS: FAILED | tests failed]"
	mock.TextOnly = mock.Response
	mock.Tokens = 42
	h.llmSvc.SetLLMCaller(mock)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Followup Failure Project")
	task := createTask(t, h, project.ID, "Followup Failure Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Please continue"
		ex.IsFollowup = true
	})

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "Please continue",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "continue from where you left off",
		WorkDir:        "",
		IsTaskFollowup: true,
	})

	updatedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecFailed {
		t.Fatalf("expected execution failed, got %s", updatedExec.Status)
	}
	if updatedExec.ErrorMessage != "tests failed" {
		t.Fatalf("expected error message %q, got %q", "tests failed", updatedExec.ErrorMessage)
	}
	if !strings.Contains(updatedExec.Output, "[STATUS: FAILED | tests failed]") {
		t.Fatalf("expected preserved failed output, got %q", updatedExec.Output)
	}

	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updatedTask.Status != models.StatusFailed {
		t.Fatalf("expected task failed, got %s", updatedTask.Status)
	}
	if updatedTask.Category != models.CategoryBacklog {
		t.Fatalf("expected task moved to backlog, got %s", updatedTask.Category)
	}
}

func TestResolveWorktreeWorkDir_SyncsExistingWorktreeFromTargetBeforeFollowup(t *testing.T) {
	h, _, _ := setupTestHandler(t)
	ctx := context.Background()

	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Stale Followup Project", RepoPath: repoDir}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	task := createTask(t, h, project.ID, "Stale worktree followup", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
	})

	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, wtBranch, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup worktree: %v", err)
	}
	task.WorktreePath = wtPath
	task.WorktreeBranch = wtBranch

	mainOnlyPath := filepath.Join(repoDir, "main-only.txt")
	if err := os.WriteFile(mainOnlyPath, []byte("new main change\n"), 0644); err != nil {
		t.Fatalf("write main-only file: %v", err)
	}
	cmd := exec.Command("git", "add", "main-only.txt")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add main-only: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "main advanced")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit main-only: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "main-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale worktree to not have main-only.txt before followup sync, stat err=%v", err)
	}

	workDir, err := h.resolveWorktreeWorkDir(ctx, task)
	if err != nil {
		t.Fatalf("resolve worktree workdir: %v", err)
	}
	if workDir != wtPath {
		t.Fatalf("expected workDir %q, got %q", wtPath, workDir)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "main-only.txt")); err != nil {
		t.Fatalf("expected followup worktree sync to include main-only.txt: %v", err)
	}

	defaultBranch := service.GetDefaultBranch(repoDir)
	cmd = exec.Command("git", "merge-base", "--is-ancestor", defaultBranch, "HEAD")
	cmd.Dir = wtPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected %s to be ancestor of synced worktree HEAD: %v\n%s", defaultBranch, err, out)
	}
}

func TestResolveWorktreeWorkDir_MergedStaleFollowupStartsFromCurrentTarget(t *testing.T) {
	h, _, _ := setupTestHandler(t)
	ctx := context.Background()

	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Merged Stale Followup Project", RepoPath: repoDir}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	task := createTask(t, h, project.ID, "Merged stale followup", func(tk *models.Task) {
		tk.Category = models.CategoryCompleted
		tk.Status = models.StatusCompleted
		tk.MergeStatus = models.MergeStatusMerged
	})

	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	oldPath, oldBranch, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup original worktree: %v", err)
	}
	task.WorktreePath = oldPath
	task.WorktreeBranch = oldBranch

	if err := os.WriteFile(filepath.Join(oldPath, "registry.go"), []byte("package registry\n\nconst value = \"stale task edit\"\n"), 0644); err != nil {
		t.Fatalf("write stale task edit: %v", err)
	}
	if err := service.CommitWorktreeChanges(oldPath, "stale task edit"); err != nil {
		t.Fatalf("commit stale task edit: %v", err)
	}

	mainFile := filepath.Join(repoDir, "registry.go")
	if err := os.WriteFile(mainFile, []byte("package registry\n\nconst value = \"accepted main edit\"\n"), 0644); err != nil {
		t.Fatalf("write accepted main edit: %v", err)
	}
	cmd := exec.Command("git", "add", "registry.go")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add accepted main edit: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "accepted main edit")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit accepted main edit: %v\n%s", err, out)
	}

	workDir, err := h.resolveWorktreeWorkDir(ctx, task)
	if err != nil {
		t.Fatalf("resolve worktree workdir should skip stale startup merge conflict: %v", err)
	}
	if workDir == oldPath {
		t.Fatalf("expected fresh current-target follow-up worktree, got original stale path %q", workDir)
	}
	if !strings.Contains(task.WorktreeBranch, "-followup-") {
		t.Fatalf("expected follow-up branch, got %q", task.WorktreeBranch)
	}
	if task.MergeTargetBranch != service.GetDefaultBranch(repoDir) {
		t.Fatalf("expected merge target %q, got %q", service.GetDefaultBranch(repoDir), task.MergeTargetBranch)
	}
	content, err := os.ReadFile(filepath.Join(workDir, "registry.go"))
	if err != nil {
		t.Fatalf("read fresh followup file: %v", err)
	}
	if !strings.Contains(string(content), "accepted main edit") || strings.Contains(string(content), "stale task edit") {
		t.Fatalf("expected fresh worktree from current target, got content:\n%s", content)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected original stale worktree to remain preserved: %v", err)
	}
}

func TestProcessStreamingResponse_TaskFollowupWithOnlyPreexistingDiffCompletes(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()

	mock := testutil.NewMockLLMCaller()
	mock.Response = "I inspected the codebase and I'm ready for the next step."
	mock.TextOnly = mock.Response
	mock.Tokens = 17
	h.llmSvc.SetLLMCaller(mock)

	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Worktree Followup Project", RepoPath: repoDir}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent := createAgent(t, llmConfigRepo)
	task := createTask(t, h, project.ID, "Worktree Followup Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})

	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, wtBranch, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wtPath, "followup.txt"), []byte("existing change\n"), 0644); err != nil {
		t.Fatalf("write preexisting change: %v", err)
	}
	if err := service.CommitWorktreeChanges(wtPath, "existing change"); err != nil {
		t.Fatalf("commit preexisting change: %v", err)
	}
	if diff := service.GetWorktreeDiff(repoDir, wtBranch, service.GetDefaultBranch(repoDir)); strings.TrimSpace(diff) == "" {
		t.Fatal("expected preexisting worktree diff before followup")
	}

	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Keep going"
		ex.IsFollowup = true
	})

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "Keep going",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "continue from where you left off",
		WorkDir:        wtPath,
		IsTaskFollowup: true,
	})

	updatedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecCompleted {
		t.Fatalf("expected execution completed, got %s", updatedExec.Status)
	}
	if updatedExec.ErrorMessage != "" {
		t.Fatalf("expected empty error message, got %q", updatedExec.ErrorMessage)
	}

	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updatedTask.Status != models.StatusCompleted {
		t.Fatalf("expected task completed, got %s", updatedTask.Status)
	}
	if updatedTask.Category != models.CategoryCompleted {
		t.Fatalf("expected task moved to completed, got %s", updatedTask.Category)
	}
}

func TestCompleteWithSuccess_UpdatesTaskStatusBeforeDiffCapture(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	project := &models.Project{Name: "Test Project"}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	task := &models.Task{
		ProjectID: project.ID, Title: "Test Task",
		Category: models.CategoryActive, Priority: 2, Prompt: "Test",
		Status: models.StatusRunning,
	}
	if err := h.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	exec := &models.Execution{
		TaskID: task.ID, AgentConfigID: agent.ID,
		Status: models.ExecRunning, PromptSent: "Test",
	}
	if err := h.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	// Call completeWithSuccess (no workDir so no git diff capture)
	h.completeWithSuccess(ctx, exec.ID, task.ID, "output text", "", 100, 5000)

	// Verify execution is completed
	completedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if completedExec.Status != models.ExecCompleted {
		t.Errorf("expected execution status %q, got %q", models.ExecCompleted, completedExec.Status)
	}

	// Verify task status is completed
	completedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if completedTask.Status != models.StatusCompleted {
		t.Errorf("expected task status %q, got %q", models.StatusCompleted, completedTask.Status)
	}

	// Verify category moved to completed
	if completedTask.Category != models.CategoryCompleted {
		t.Errorf("expected category %q, got %q", models.CategoryCompleted, completedTask.Category)
	}
}

func TestCompleteWithFailure_UpdatesTaskStatus(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	project := &models.Project{Name: "Test Project"}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	task := &models.Task{
		ProjectID: project.ID, Title: "Test Task",
		Category: models.CategoryActive, Priority: 2, Prompt: "Test",
		Status: models.StatusRunning,
	}
	if err := h.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	exec := &models.Execution{
		TaskID: task.ID, AgentConfigID: agent.ID,
		Status: models.ExecRunning, PromptSent: "Test",
	}
	if err := h.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	h.completeWithFailure(ctx, exec.ID, task.ID, "something failed", 3000)

	// Verify execution is failed
	failedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if failedExec.Status != models.ExecFailed {
		t.Errorf("expected execution status %q, got %q", models.ExecFailed, failedExec.Status)
	}

	// Verify task status is failed
	failedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if failedTask.Status != models.StatusFailed {
		t.Errorf("expected task status %q, got %q", models.StatusFailed, failedTask.Status)
	}

	// Verify task moved to backlog (not stuck in active)
	if failedTask.Category != models.CategoryBacklog {
		t.Errorf("expected category %q, got %q", models.CategoryBacklog, failedTask.Category)
	}

	// Verify failure alert was created
	alerts, err := h.alertSvc.ListByProject(ctx, project.ID, 100)
	if err != nil {
		t.Fatalf("failed to list alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Type != models.AlertTaskFailed {
		t.Errorf("expected alert type %q, got %q", models.AlertTaskFailed, alerts[0].Type)
	}
	if alerts[0].Severity != models.SeverityError {
		t.Errorf("expected alert severity %q, got %q", models.SeverityError, alerts[0].Severity)
	}
	if !strings.Contains(alerts[0].Message, "something failed") {
		t.Errorf("expected alert message to contain error, got %q", alerts[0].Message)
	}
}

func TestCompleteWithFailure_MovesCompletedTaskToBacklog(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	project := &models.Project{Name: "Test Project"}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Reproduces follow-up failure on a task already in the completed column.
	task := &models.Task{
		ProjectID: project.ID, Title: "Previously completed task",
		Category: models.CategoryCompleted, Priority: 2, Prompt: "Test",
		Status: models.StatusRunning,
	}
	if err := h.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	exec := &models.Execution{
		TaskID: task.ID, AgentConfigID: agent.ID,
		Status: models.ExecRunning, PromptSent: "Test",
	}
	if err := h.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	h.completeWithFailure(ctx, exec.ID, task.ID, "follow-up failed", 1200)

	failedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if failedTask.Status != models.StatusFailed {
		t.Errorf("expected task status %q, got %q", models.StatusFailed, failedTask.Status)
	}
	if failedTask.Category != models.CategoryBacklog {
		t.Errorf("expected category %q, got %q", models.CategoryBacklog, failedTask.Category)
	}
}

func TestCompleteWithFailure_WorksWithExpiredContext(t *testing.T) {
	// This is the exact bug scenario: the 5-minute timeout fires, killing the
	// LLM call. The caller's context is expired, but completeWithFailure must
	// still update the DB using its own fresh context.
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	project := &models.Project{Name: "Test Project"}
	if err := h.projectSvc.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	task := &models.Task{
		ProjectID: project.ID, Title: "Timeout Task",
		Category: models.CategoryActive, Priority: 2, Prompt: "Test",
		Status: models.StatusRunning,
	}
	if err := h.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	exec := &models.Execution{
		TaskID: task.ID, AgentConfigID: agent.ID,
		Status: models.ExecRunning, PromptSent: "Test",
	}
	if err := h.execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	// Simulate the bug: call completeWithFailure with an already-cancelled context
	expiredCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — this is what happens when the 5-min timeout fires

	h.completeWithFailure(expiredCtx, exec.ID, task.ID, "claude CLI error: signal: killed", 300000)

	// Verify everything still updated despite the expired context
	failedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("failed to get execution: %v", err)
	}
	if failedExec.Status != models.ExecFailed {
		t.Errorf("expected execution status %q, got %q — DB update failed with expired context", models.ExecFailed, failedExec.Status)
	}

	failedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if failedTask.Status != models.StatusFailed {
		t.Errorf("expected task status %q, got %q — task stuck in running", models.StatusFailed, failedTask.Status)
	}
	if failedTask.Category != models.CategoryBacklog {
		t.Errorf("expected category %q, got %q — task not moved to backlog", models.CategoryBacklog, failedTask.Category)
	}

	// Verify alert was created even with expired caller context
	alerts, err := h.alertSvc.ListByProject(ctx, project.ID, 100)
	if err != nil {
		t.Fatalf("failed to list alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d — alert not created with expired context", len(alerts))
	}
	if !strings.Contains(alerts[0].Title, "Timeout Task") {
		t.Errorf("expected alert title to contain task name, got %q", alerts[0].Title)
	}
}

func TestSelectAgent_DefaultReturnsDefaultModel(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	// Create two agents, mark the second as default
	agent1 := &models.LLMConfig{
		Name: "Haiku", Provider: models.ProviderTest, Model: "claude-3-haiku",
		APIKey: "key1", MaxTokens: 4096, Temperature: 1.0, IsDefault: false,
	}
	agent2 := &models.LLMConfig{
		Name: "Sonnet", Provider: models.ProviderTest, Model: "claude-3-5-sonnet",
		APIKey: "key2", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent1); err != nil {
		t.Fatalf("failed to create agent1: %v", err)
	}
	if err := llmConfigRepo.Create(ctx, agent2); err != nil {
		t.Fatalf("failed to create agent2: %v", err)
	}

	// selectAgent with "default" should return the default agent (agent2)
	selected, err := h.selectAgent(ctx, "default", "hello", false)
	if err != nil {
		t.Fatalf("selectAgent default failed: %v", err)
	}
	if selected.ID != agent2.ID {
		t.Errorf("expected default agent %s, got %s", agent2.ID, selected.ID)
	}
	if selected.Name != "Sonnet" {
		t.Errorf("expected agent name 'Sonnet', got %q", selected.Name)
	}
}

func TestSelectAgent_DefaultFallsBackWhenNoDefaultConfigured(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	// Delete any seeded default agents (migration 003 seeds "Claude Max" with is_default=1)
	seeded, _ := llmConfigRepo.GetDefault(ctx)
	if seeded != nil {
		_ = llmConfigRepo.Delete(ctx, seeded.ID)
	}

	// Create agent without IsDefault set
	agent := &models.LLMConfig{
		Name: "Haiku", Provider: models.ProviderTest, Model: "claude-3-haiku",
		APIKey: "key1", MaxTokens: 4096, Temperature: 1.0, IsDefault: false,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// selectAgent with "default" should fall back to first available
	selected, err := h.selectAgent(ctx, "default", "hello", false)
	if err != nil {
		t.Fatalf("selectAgent default fallback failed: %v", err)
	}
	if selected.ID != agent.ID {
		t.Errorf("expected fallback to first agent %s, got %s", agent.ID, selected.ID)
	}
}

func TestSelectAgent_AutoStillWorks(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Sonnet", Provider: models.ProviderTest, Model: "claude-3-5-sonnet",
		APIKey: "key1", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// "auto" should still use auto-selection (not default)
	selected, err := h.selectAgent(ctx, "auto", "hello", false)
	if err != nil {
		t.Fatalf("selectAgent auto failed: %v", err)
	}
	if selected == nil {
		t.Fatal("selectAgent auto returned nil")
	}
}

func TestSelectAgent_ExplicitIDStillWorks(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent1 := &models.LLMConfig{
		Name: "Haiku", Provider: models.ProviderTest, Model: "claude-3-haiku",
		APIKey: "key1", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	agent2 := &models.LLMConfig{
		Name: "Sonnet", Provider: models.ProviderTest, Model: "claude-3-5-sonnet",
		APIKey: "key2", MaxTokens: 4096, Temperature: 1.0, IsDefault: false,
	}
	if err := llmConfigRepo.Create(ctx, agent1); err != nil {
		t.Fatalf("failed to create agent1: %v", err)
	}
	if err := llmConfigRepo.Create(ctx, agent2); err != nil {
		t.Fatalf("failed to create agent2: %v", err)
	}

	// Explicit agent ID should bypass both auto and default
	selected, err := h.selectAgent(ctx, agent2.ID, "hello", false)
	if err != nil {
		t.Fatalf("selectAgent explicit failed: %v", err)
	}
	if selected.ID != agent2.ID {
		t.Errorf("expected agent2 %s, got %s", agent2.ID, selected.ID)
	}
}

// TestFollowupResetsMergeStatus verifies that when a follow-up creates new changes
// after a task has been merged, the merge_status is reset from "merged" to "pending"
// so the merge button re-appears in the UI.
func TestFollowupResetsMergeStatus(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create a test git repository
	repoDir := createHandlerTestGitRepo(t)

	// Create project with repo
	project := &models.Project{Name: "Test Project", RepoPath: repoDir}
	if err := h.projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create task with worktree
	task := createTask(t, h, project.ID, "Test Task with Worktree", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
		tk.AutoMerge = false
	})

	// Set up worktree service and create worktree
	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, _, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup worktree: %v", err)
	}

	// Create initial changes and commit them
	if err := os.WriteFile(filepath.Join(wtPath, "test.txt"), []byte("initial change\n"), 0644); err != nil {
		t.Fatalf("write initial change: %v", err)
	}
	if err := service.CommitWorktreeChanges(wtPath, "initial change"); err != nil {
		t.Fatalf("commit initial change: %v", err)
	}

	// Simulate the task being merged
	if err := h.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusMerged); err != nil {
		t.Fatalf("update merge status to merged: %v", err)
	}

	// Verify merge status is "merged"
	task, err = h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.MergeStatus != models.MergeStatusMerged {
		t.Fatalf("expected merge_status=merged before followup, got %s", task.MergeStatus)
	}

	// Create follow-up execution that will make new changes
	followupExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Make more changes"
		ex.IsFollowup = true
	})

	// Create new change in worktree before processing (simulating LLM making changes)
	if err := os.WriteFile(filepath.Join(wtPath, "followup.txt"), []byte("followup change\n"), 0644); err != nil {
		t.Fatalf("write followup change: %v", err)
	}

	// Mock LLM caller
	h.llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())

	// Process the follow-up
	h.processStreamingResponse(streamingResponseParams{
		ExecID:         followupExec.ID,
		TaskID:         task.ID,
		Message:        "Make more changes",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "continue work",
		WorkDir:        wtPath,
		IsTaskFollowup: true,
	})

	// Verify execution completed
	updatedExec, err := h.execRepo.GetByID(ctx, followupExec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecCompleted {
		t.Fatalf("expected execution completed, got %s (error: %s)", updatedExec.Status, updatedExec.ErrorMessage)
	}

	// Verify merge status was reset to "pending"
	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updatedTask.MergeStatus != models.MergeStatusPending {
		t.Errorf("expected merge_status=pending after followup with new changes, got %s", updatedTask.MergeStatus)
	}

	// Verify the diff was captured
	if updatedExec.DiffOutput == "" {
		t.Error("expected diff output to be captured for followup")
	}

	// Verify the changes are committed in the worktree
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = wtPath
	out, _ := statusCmd.Output()
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Errorf("expected worktree to have no uncommitted changes after followup, got: %s", string(out))
	}
}

func TestFollowupResetsConflictMergeStatus(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Test Project", RepoPath: repoDir}
	if err := h.projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	task := createTask(t, h, project.ID, "Conflicted Task with Followup", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
		tk.AutoMerge = false
	})

	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, _, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup worktree: %v", err)
	}

	if err := h.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusConflict); err != nil {
		t.Fatalf("update merge status to conflict: %v", err)
	}

	followupExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Resolve and continue"
		ex.IsFollowup = true
	})

	if err := os.WriteFile(filepath.Join(wtPath, "resolved-followup.txt"), []byte("new followup work\n"), 0644); err != nil {
		t.Fatalf("write followup change: %v", err)
	}

	h.completeWithSuccess(ctx, followupExec.ID, task.ID, "done", wtPath, 0, 1)

	updatedExec, err := h.execRepo.GetByID(ctx, followupExec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecCompleted {
		t.Fatalf("expected execution completed, got %s (error: %s)", updatedExec.Status, updatedExec.ErrorMessage)
	}
	if updatedExec.DiffOutput == "" {
		t.Fatal("expected diff output to be captured for followup")
	}

	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updatedTask.MergeStatus != models.MergeStatusPending {
		t.Errorf("expected merge_status=pending after conflict followup with new changes, got %s", updatedTask.MergeStatus)
	}
}

// TestFollowupNoChangesDoesNotResetMergeStatus verifies that when a follow-up
// does NOT create new changes, the merge_status stays as "merged".
func TestFollowupNoChangesDoesNotResetMergeStatus(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	ctx := context.Background()

	agent := &models.LLMConfig{
		Name: "Test Agent", Provider: models.ProviderTest,
		Model: "claude-sonnet-4-5", MaxTokens: 4096, Temperature: 1.0, IsDefault: true,
	}
	if err := llmConfigRepo.Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create a test git repository
	repoDir := createHandlerTestGitRepo(t)

	// Create project with repo
	project := &models.Project{Name: "Test Project", RepoPath: repoDir}
	if err := h.projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create task with worktree
	task := createTask(t, h, project.ID, "Read-only Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
		tk.AutoMerge = false
	})

	// Set up worktree service and create worktree
	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, _, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup worktree: %v", err)
	}

	// Simulate the task being merged
	if err := h.taskRepo.UpdateMergeStatus(ctx, task.ID, models.MergeStatusMerged); err != nil {
		t.Fatalf("update merge status to merged: %v", err)
	}

	// Verify merge status is "merged"
	task, err = h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.MergeStatus != models.MergeStatusMerged {
		t.Fatalf("expected merge_status=merged before followup, got %s", task.MergeStatus)
	}

	// Create follow-up execution that will NOT make changes (read-only)
	followupExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "What's in this repository?"
		ex.IsFollowup = true
	})

	// Mock LLM caller (doesn't create any files)
	h.llmSvc.SetLLMCaller(testutil.NewMockLLMCaller())

	// Process the follow-up
	h.processStreamingResponse(streamingResponseParams{
		ExecID:         followupExec.ID,
		TaskID:         task.ID,
		Message:        "What's in this repository?",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "continue work",
		WorkDir:        wtPath,
		IsTaskFollowup: true,
	})

	// Verify execution completed
	updatedExec, err := h.execRepo.GetByID(ctx, followupExec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if updatedExec.Status != models.ExecCompleted {
		t.Fatalf("expected execution completed, got %s (error: %s)", updatedExec.Status, updatedExec.ErrorMessage)
	}

	// Verify merge status stayed as "merged" (not reset)
	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updatedTask.MergeStatus != models.MergeStatusMerged {
		t.Errorf("expected merge_status=merged after followup without changes, got %s", updatedTask.MergeStatus)
	}

	// Verify no diff was captured (no changes)
	if updatedExec.DiffOutput != "" {
		t.Errorf("expected no diff output for read-only followup, got %d bytes", len(updatedExec.DiffOutput))
	}
}

func TestProcessStreamingResponse_TaskFollowupRateLimitFailurePreservesHistory(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Rate Limit Followup Project")
	task := createTask(t, h, project.ID, "Rate Limit Followup Task", func(tk *models.Task) {
		tk.Category = models.CategoryCompleted
		tk.Status = models.StatusCompleted
		tk.AgentID = &agent.ID
		tk.Prompt = "Original implementation prompt"
	})

	initial := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecCompleted
		ex.PromptSent = "Original implementation prompt"
		ex.IsFollowup = false
	})
	if err := h.execRepo.Complete(ctx, initial.ID, models.ExecCompleted, "initial success output", "", 33, 120); err != nil {
		t.Fatalf("complete initial execution: %v", err)
	}

	failedFollowup := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Continue with follow-up"
		ex.IsFollowup = true
	})
	streamedPrefix := "Investigating prior changes..."
	if err := h.execRepo.UpdateOutput(ctx, failedFollowup.ID, streamedPrefix); err != nil {
		t.Fatalf("seed streamed output: %v", err)
	}

	mock := testutil.NewMockLLMCaller()
	mock.Err = fmt.Errorf("API error 429: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"This request would exceed your account's rate limit. Please try again later.\"}}")
	h.llmSvc.SetLLMCaller(mock)

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         failedFollowup.ID,
		TaskID:         task.ID,
		Message:        "Continue with follow-up",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "continue work",
		WorkDir:        "",
		IsTaskFollowup: true,
	})

	failedExec, err := h.execRepo.GetByID(ctx, failedFollowup.ID)
	if err != nil {
		t.Fatalf("get failed execution: %v", err)
	}
	if failedExec.Status != models.ExecFailed {
		t.Fatalf("expected failed execution status, got %s", failedExec.Status)
	}
	if !strings.Contains(failedExec.ErrorMessage, "429") || !strings.Contains(strings.ToLower(failedExec.ErrorMessage), "rate_limit_error") {
		t.Fatalf("expected rate-limit error message, got %q", failedExec.ErrorMessage)
	}
	if failedExec.Output != streamedPrefix {
		t.Fatalf("expected failed execution to preserve streamed output, got %q", failedExec.Output)
	}

	updatedTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if updatedTask.Status != models.StatusFailed {
		t.Fatalf("expected task status failed after 429, got %s", updatedTask.Status)
	}
	if updatedTask.Category != models.CategoryBacklog {
		t.Fatalf("expected task moved to backlog after failure, got %s", updatedTask.Category)
	}

	execs, err := h.execRepo.ListByTaskChronological(ctx, task.ID)
	if err != nil {
		t.Fatalf("list task executions: %v", err)
	}
	if len(execs) != 2 {
		t.Fatalf("expected 2 executions preserved after 429, got %d", len(execs))
	}
	if execs[0].Output != "initial success output" {
		t.Fatalf("expected initial execution output preserved, got %q", execs[0].Output)
	}
	if execs[1].Status != models.ExecFailed {
		t.Fatalf("expected second execution failed, got %s", execs[1].Status)
	}
}

func TestProcessStreamingResponse_TaskFollowupBroadcastsRealtimeDiffSnapshots(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()

	fileChangeBroadcaster := events.NewFileChangeBroadcaster()
	h.SetFileChangeBroadcaster(fileChangeBroadcaster)

	agent := createAgent(t, llmConfigRepo)
	repoDir := createHandlerTestGitRepo(t)
	project := &models.Project{Name: "Followup Diff Project", RepoPath: repoDir}
	if err := h.projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	task := createTask(t, h, project.ID, "Completed task followup", func(tk *models.Task) {
		tk.Category = models.CategoryCompleted
		tk.Status = models.StatusQueued
		tk.AgentID = &agent.ID
	})

	h.worktreeSvc = service.NewWorktreeService(h.taskRepo, h.projectRepo, h.settingsRepo)
	wtPath, _, err := h.worktreeSvc.SetupWorktree(ctx, task, repoDir)
	if err != nil {
		t.Fatalf("setup worktree: %v", err)
	}

	followupExec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Apply follow-up"
		ex.IsFollowup = true
	})

	mock := testutil.NewMockLLMCaller()
	mock.Response = "done"
	mock.TextOnly = "done"
	h.llmSvc.SetLLMCaller(mock)

	sub, err := fileChangeBroadcaster.Subscribe()
	if err != nil {
		t.Fatalf("subscribe filechange broadcaster: %v", err)
	}
	defer fileChangeBroadcaster.Unsubscribe(sub)

	if err := os.WriteFile(filepath.Join(wtPath, "followup.txt"), []byte("followup change\n"), 0644); err != nil {
		t.Fatalf("write followup change: %v", err)
	}

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         followupExec.ID,
		TaskID:         task.ID,
		Message:        "Apply follow-up",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "continue work",
		WorkDir:        wtPath,
		IsTaskFollowup: true,
	})

	timeout := time.After(2 * time.Second)
	receivedDiffSnapshot := false
	for !receivedDiffSnapshot {
		select {
		case evt := <-sub:
			if evt.Type == events.DiffSnapshot && evt.TaskID == task.ID && evt.ExecID == followupExec.ID && strings.TrimSpace(evt.DiffOutput) != "" {
				receivedDiffSnapshot = true
			}
		case <-timeout:
			t.Fatal("expected diff_snapshot event for follow-up execution")
		}
	}

	updatedExec, err := h.execRepo.GetByID(ctx, followupExec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if strings.TrimSpace(updatedExec.DiffOutput) == "" {
		t.Fatal("expected follow-up execution diff_output to be persisted during realtime snapshot broadcast")
	}
}

func TestFormatThreadTranscript_FullContent(t *testing.T) {
	tc := NewTestContext(t)
	h := tc.handler

	task := &models.Task{
		ID:       "task-full",
		Title:    "Full Content Task",
		Status:   models.StatusCompleted,
		Category: models.CategoryCompleted,
		Prompt:   "Build the API",
		Priority: 2,
	}

	executions := []models.Execution{
		{
			ID:         "exec1",
			PromptSent: "Build the API",
			Output:     "Created 3 endpoints for users, posts, and comments with full CRUD operations.",
			Status:     models.ExecCompleted,
			StartedAt:  time.Now().Add(-2 * time.Hour),
		},
		{
			ID:         "exec2",
			PromptSent: "Add authentication middleware",
			Output:     "Added JWT auth middleware with token validation and refresh logic.",
			Status:     models.ExecCompleted,
			IsFollowup: true,
			StartedAt:  time.Now().Add(-1 * time.Hour),
		},
	}

	transcript := h.formatThreadTranscript(task, executions, 0, 0)

	// All content should be present without truncation
	if !strings.Contains(transcript, "Created 3 endpoints for users, posts, and comments with full CRUD operations.") {
		t.Error("expected full first execution output, got truncated")
	}
	if !strings.Contains(transcript, "Added JWT auth middleware with token validation and refresh logic.") {
		t.Error("expected full second execution output, got truncated")
	}
	if !strings.Contains(transcript, "Total executions: 2") {
		t.Error("expected total executions count")
	}
	if strings.Contains(transcript, "truncated") {
		t.Error("short content should not be truncated")
	}
	if strings.Contains(transcript, "offset") {
		t.Error("short content should not show pagination")
	}
}

func TestFormatThreadTranscript_Pagination(t *testing.T) {
	tc := NewTestContext(t)
	h := tc.handler

	task := &models.Task{
		ID:       "task-page",
		Title:    "Paginated Task",
		Status:   models.StatusCompleted,
		Category: models.CategoryCompleted,
		Prompt:   "Do work",
		Priority: 1,
	}

	// Create enough executions with large output to exceed budget
	var executions []models.Execution
	largeOutput := strings.Repeat("A", 20*1024) // 20KB each
	for i := 0; i < 10; i++ {
		executions = append(executions, models.Execution{
			ID:         "exec-" + strings.Repeat("x", i+1),
			PromptSent: "step " + strings.Repeat("x", i+1),
			Output:     largeOutput,
			Status:     models.ExecCompleted,
			IsFollowup: i > 0,
			StartedAt:  time.Now().Add(-time.Duration(10-i) * time.Hour),
		})
	}

	// First page (offset=0, no limit)
	page1 := h.formatThreadTranscript(task, executions, 0, 0)
	if !strings.Contains(page1, "Total executions: 10") {
		t.Error("expected total execution count of 10")
	}
	// Should hit budget and show pagination hint
	if !strings.Contains(page1, "Transcript size limit reached") {
		t.Error("expected size limit pagination hint for large thread")
	}
	if !strings.Contains(page1, "offset") {
		t.Error("expected offset hint in pagination message")
	}
}

func TestFormatThreadTranscript_OffsetAndLimit(t *testing.T) {
	tc := NewTestContext(t)
	h := tc.handler

	task := &models.Task{
		ID:       "task-ol",
		Title:    "Offset Limit Task",
		Status:   models.StatusCompleted,
		Category: models.CategoryCompleted,
		Prompt:   "original prompt",
		Priority: 1,
	}

	executions := []models.Execution{
		{ID: "e0", PromptSent: "msg0", Output: "out0", Status: models.ExecCompleted, StartedAt: time.Now().Add(-3 * time.Hour)},
		{ID: "e1", PromptSent: "msg1", Output: "out1", Status: models.ExecCompleted, IsFollowup: true, StartedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "e2", PromptSent: "msg2", Output: "out2", Status: models.ExecCompleted, IsFollowup: true, StartedAt: time.Now().Add(-1 * time.Hour)},
	}

	// With offset=1, limit=1: should show only exec1
	transcript := h.formatThreadTranscript(task, executions, 1, 1)
	if !strings.Contains(transcript, "msg1") {
		t.Error("expected exec at offset 1")
	}
	if !strings.Contains(transcript, "out1") {
		t.Error("expected output at offset 1")
	}
	if strings.Contains(transcript, "msg0") {
		t.Error("should not include exec before offset")
	}
	if strings.Contains(transcript, "msg2") {
		t.Error("should not include exec beyond limit")
	}
	if !strings.Contains(transcript, "Showing executions 2–2 of 3") {
		t.Error("expected pagination summary showing position")
	}
}

func TestFormatThreadTranscript_OffsetBeyondTotal(t *testing.T) {
	tc := NewTestContext(t)
	h := tc.handler

	task := &models.Task{
		ID:       "task-oob",
		Title:    "OOB Task",
		Status:   models.StatusCompleted,
		Category: models.CategoryCompleted,
		Priority: 1,
	}

	executions := []models.Execution{
		{ID: "e0", Output: "out0", Status: models.ExecCompleted, StartedAt: time.Now()},
	}

	transcript := h.formatThreadTranscript(task, executions, 5, 0)
	if !strings.Contains(transcript, "Offset 5 exceeds total executions (1)") {
		t.Error("expected offset out-of-bounds message")
	}
}

func TestFormatThreadTranscript_Empty(t *testing.T) {
	tc := NewTestContext(t)
	h := tc.handler

	task := &models.Task{
		ID:       "task-empty",
		Title:    "Empty Task",
		Status:   models.StatusPending,
		Category: models.CategoryBacklog,
		Priority: 1,
	}

	transcript := h.formatThreadTranscript(task, []models.Execution{}, 0, 0)
	if !strings.Contains(transcript, "No execution history found") {
		t.Error("expected empty history message")
	}
}

func TestFormatThreadTranscript_LargeMessageTruncation(t *testing.T) {
	tc := NewTestContext(t)
	h := tc.handler

	task := &models.Task{
		ID:       "task-large",
		Title:    "Large Msg Task",
		Status:   models.StatusCompleted,
		Category: models.CategoryCompleted,
		Prompt:   "Do it",
		Priority: 1,
	}

	// Output larger than maxPerMessageBytes (50KB)
	hugeOutput := strings.Repeat("B", 60*1024)
	executions := []models.Execution{
		{ID: "e0", PromptSent: "go", Output: hugeOutput, Status: models.ExecCompleted, StartedAt: time.Now()},
	}

	transcript := h.formatThreadTranscript(task, executions, 0, 0)
	if !strings.Contains(transcript, "message truncated at 50KB") {
		t.Error("expected per-message truncation suffix for oversized output")
	}
	// The transcript itself should be well under 100KB even with truncation
	if len(transcript) > 100*1024 {
		t.Errorf("transcript too large: %d bytes", len(transcript))
	}
}

func TestViewThreadRequest_OffsetLimit(t *testing.T) {
	// Verify ViewThreadRequest parses offset/limit from JSON
	output := `[VIEW_TASK_CHAT]
{"task_id": "abc123", "offset": 5, "limit": 3}
[/VIEW_TASK_CHAT]`

	requests := service.ParseViewThread(output)
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].TaskID != "abc123" {
		t.Errorf("expected task_id abc123, got %q", requests[0].TaskID)
	}
	if requests[0].Offset != 5 {
		t.Errorf("expected offset 5, got %d", requests[0].Offset)
	}
	if requests[0].Limit != 3 {
		t.Errorf("expected limit 3, got %d", requests[0].Limit)
	}
}

// TestProcessStreamingResponse_PhantomCreateTaskRegression verifies the fix for
// the phantom-create_task bug: when the caller (e.g. ChatSend) passes
// ProcessMarkers=false and the agent's provider/auth does NOT support runtime
// action tools (Claude CLI, Codex CLI, Ollama, test), processStreamingResponse
// must fall back to marker processing so [CREATE_TASK] blocks emitted by the
// model are actually executed. Without the fallback, the assistant transcript
// would show a "create_task" action that the backend never executed — leaving
// the task absent from the project task list.
func TestProcessStreamingResponse_PhantomCreateTaskRegression(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()

	// ProviderTest deliberately returns false from supportsChatActionTools,
	// mirroring the behavior of Claude CLI / Codex CLI / Ollama in production.
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Phantom Task Project")

	// Mock the model's response: a normal chat turn that emits a CREATE_TASK
	// marker, as a CLI-backed model would.
	mock := testutil.NewMockLLMCaller()
	mock.Response = "I'll create that task for you.\n\n[CREATE_TASK]\n" +
		`{"title": "Fix overlapping thinking and non-thinking content in task thread view", "prompt": "Investigate and fix the overlapping rendering."}` +
		"\n[/CREATE_TASK]"
	mock.TextOnly = mock.Response
	mock.Tokens = 25
	h.llmSvc.SetLLMCaller(mock)

	// ChatSend creates a CategoryChat host task that owns the chat execution
	// record. Mirror that so the FK on executions(task_id) is satisfied.
	chatHostTask := createTask(t, h, project.ID, "Chat host", func(tk *models.Task) {
		tk.Category = models.CategoryChat
		tk.Status = models.StatusPending
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, chatHostTask.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "Create a task to fix overlapping thinking content"
	})

	// Simulate the call pattern used by ChatSend / APIChatMessage: a chat turn
	// (not a task follow-up) with ProcessMarkers explicitly set to false.
	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         chatHostTask.ID,
		Message:        "Create a task to fix overlapping thinking content",
		Agent:          *agent,
		ProjectID:      project.ID,
		SystemContext:  "",
		WorkDir:        "",
		IsTaskFollowup: false,
		ProcessMarkers: false,
	})

	tasks, err := h.taskRepo.ListByProject(ctx, project.ID, "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	// Expect the marker-created task in addition to the CategoryChat host task.
	var created *models.Task
	for i := range tasks {
		if strings.Contains(tasks[i].Title, "overlapping thinking") {
			created = &tasks[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("expected a task created from the [CREATE_TASK] marker to be present in the project task list, got %+v", tasks)
	}

	// The execution output should also be rewritten to include the canonical
	// [TASK_ID:...] confirmation marker that proves the task was persisted.
	updatedExec, err := h.execRepo.GetByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if !strings.Contains(updatedExec.Output, "[TASK_ID:") {
		t.Errorf("expected execution output to contain [TASK_ID:...] confirmation marker after marker processing, got: %s", updatedExec.Output)
	}
}

func TestStartQueuedChatInputPreservesChannelOriginMetadata(t *testing.T) {
	h, _, llmConfigRepo, db := setupTestHandlerWithDB(t)
	h.workerSvc = nil
	h.SetSlackTaskContextRepo(repository.NewSlackTaskContextRepo(db))
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Queued Channel Metadata Project")
	activeTask := createTask(t, h, project.ID, "Active Channel Chat", func(tk *models.Task) {
		tk.Category = models.CategoryChat
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	activeExec := createExec(t, h, activeTask.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active chat"
	})

	telegramInput := &models.ThreadInput{
		Scope:          models.ThreadInputScopeChat,
		ProjectID:      project.ID,
		RunExecutionID: activeExec.ID,
		AgentConfigID:  agent.ID,
		InputMode:      models.ThreadInputModeQueued,
		InputStatus:    models.ThreadInputPending,
		Content:        "telegram follow-up",
		ChatMode:       models.ChatModeOrchestrate,
		Source:         models.TaskOriginTelegram,
		TelegramChatID: 12345,
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, telegramInput))

	mock := testutil.NewMockLLMCaller()
	mock.Response = "telegram done"
	mock.TextOnly = "telegram done"
	h.llmSvc.SetLLMCaller(mock)
	h.startQueuedChatInput(ctx, *telegramInput)
	require.Eventually(t, func() bool { return mock.CallCount() == 1 }, 2*time.Second, 25*time.Millisecond)

	telegramReq := mock.LastAgentRequest()
	telegramExec, err := h.execRepo.GetByID(ctx, telegramReq.ExecID)
	require.NoError(t, err)
	require.NotNil(t, telegramExec)
	telegramTask, err := h.taskRepo.GetByID(ctx, telegramExec.TaskID)
	require.NoError(t, err)
	require.NotNil(t, telegramTask)
	require.Equal(t, models.TaskOriginTelegram, telegramTask.CreatedVia)
	require.Equal(t, int64(12345), telegramTask.TelegramChatID)

	activeExec2 := createExec(t, h, activeTask.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "second active chat"
	})
	slackInput := &models.ThreadInput{
		Scope:          models.ThreadInputScopeChat,
		ProjectID:      project.ID,
		RunExecutionID: activeExec2.ID,
		AgentConfigID:  agent.ID,
		InputMode:      models.ThreadInputModeQueued,
		InputStatus:    models.ThreadInputPending,
		Content:        "slack follow-up",
		ChatMode:       models.ChatModeOrchestrate,
		Source:         models.TaskOriginSlack,
		SlackTeamID:    "T1",
		SlackChannelID: "C1",
		SlackThreadTS:  "1710000000.100000",
		SlackUserID:    "U1",
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, slackInput))

	h.startQueuedChatInput(ctx, *slackInput)
	require.Eventually(t, func() bool { return mock.CallCount() == 2 }, 2*time.Second, 25*time.Millisecond)

	slackReq := mock.LastAgentRequest()
	slackExec, err := h.execRepo.GetByID(ctx, slackReq.ExecID)
	require.NoError(t, err)
	require.NotNil(t, slackExec)
	slackTask, err := h.taskRepo.GetByID(ctx, slackExec.TaskID)
	require.NoError(t, err)
	require.NotNil(t, slackTask)
	require.Equal(t, models.TaskOriginSlack, slackTask.CreatedVia)
	stc, err := h.slackTaskContextRepo.GetByTaskID(ctx, slackTask.ID)
	require.NoError(t, err)
	require.NotNil(t, stc)
	require.Equal(t, "C1", stc.SlackChannelID)
	require.Equal(t, "1710000000.100000", stc.SlackThreadTS)
}

func TestProcessStreamingResponse_ReaddsRecoveredPreparedSteeringToRealtimeUI(t *testing.T) {
	h, _, llmConfigRepo := setupTestHandler(t)
	h.workerSvc = nil
	ctx := context.Background()
	mock := testutil.NewMockLLMCaller()
	mock.Response = "partial output"
	mock.TextOnly = "partial output"
	mock.Err = errors.New("provider failed")
	h.llmSvc.SetLLMCaller(mock)
	broadcaster := events.NewBroadcaster()
	h.broadcaster = broadcaster
	sub, err := broadcaster.Subscribe()
	require.NoError(t, err)
	defer broadcaster.Unsubscribe(sub)

	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Recovered Steering Event Project")
	task := createTask(t, h, project.ID, "Recovered Steering Event Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "recovered steer",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	h.processStreamingResponse(streamingResponseParams{
		ExecID:         exec.ID,
		TaskID:         task.ID,
		Message:        "active prompt",
		Agent:          *agent,
		ProjectID:      project.ID,
		IsTaskFollowup: true,
	})

	var sawRemoval bool
	var sawReadd bool
	deadline := time.After(2 * time.Second)
	for !sawRemoval || !sawReadd {
		select {
		case event := <-sub:
			if event.PendingInputID != steering.ID {
				continue
			}
			if event.Type == events.TaskThreadInputApplied {
				sawRemoval = true
			}
			if event.Type == events.TaskThreadInputQueued {
				sawReadd = true
				require.Equal(t, "recovered steer", event.Message)
				require.Equal(t, exec.ID, event.ExecID)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for recovery events; removal=%v readd=%v", sawRemoval, sawReadd)
		}
	}

	stored, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, models.ThreadInputPending, stored.InputStatus)
	require.Equal(t, models.ThreadInputModeQueued, stored.InputMode)
}

func TestCancelThreadInputAllowsPendingSteeringBeforePreparation(t *testing.T) {
	h, e, llmConfigRepo := setupTestHandler(t)
	broadcaster := events.NewBroadcaster()
	h.broadcaster = broadcaster
	sub, err := broadcaster.Subscribe()
	require.NoError(t, err)
	defer broadcaster.Unsubscribe(sub)
	ctx := context.Background()
	agent := createAgent(t, llmConfigRepo)
	project := createProject(t, h, "Cancel Pending Steering Project")
	task := createTask(t, h, project.ID, "Cancel Pending Steering Task", func(tk *models.Task) {
		tk.Category = models.CategoryActive
		tk.Status = models.StatusRunning
		tk.AgentID = &agent.ID
	})
	exec := createExec(t, h, task.ID, agent.ID, func(ex *models.Execution) {
		ex.Status = models.ExecRunning
		ex.PromptSent = "active prompt"
		ex.IsFollowup = true
	})
	steering := &models.ThreadInput{
		Scope:          models.ThreadInputScopeTask,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		RunExecutionID: exec.ID,
		InputMode:      models.ThreadInputModeSteering,
		InputStatus:    models.ThreadInputPending,
		TurnID:         exec.ID,
		ExpectedTurnID: exec.ID,
		Content:        "cancel me before processing",
	}
	require.NoError(t, h.threadInputRepo.CreateSteeringForActiveExecution(ctx, steering, exec.ID))

	req := httptest.NewRequest(http.MethodPost, "/thread-inputs/"+steering.ID+"/cancel", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="thread-input-`+steering.ID+`"`)

	select {
	case event := <-sub:
		require.Equal(t, events.TaskThreadInputCancelled, event.Type)
		require.Equal(t, task.ID, event.TaskID)
		require.Equal(t, project.ID, event.ProjectID)
		require.Equal(t, steering.ID, event.PendingInputID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task thread input cancellation event")
	}

	stored, err := h.threadInputRepo.GetByID(ctx, steering.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, models.ThreadInputCancelled, stored.InputStatus)
}

func TestCancelThreadInputBroadcastsChatCancellation(t *testing.T) {
	h, e, _ := setupTestHandler(t)
	ctx := context.Background()
	project := createProject(t, h, "Cancel Chat Pending Input Project")
	input := &models.ThreadInput{
		Scope:       models.ThreadInputScopeChat,
		ProjectID:   project.ID,
		InputMode:   models.ThreadInputModeQueued,
		InputStatus: models.ThreadInputPending,
		Content:     "cancel queued chat",
		Source:      "web",
	}
	require.NoError(t, h.threadInputRepo.CreateQueued(ctx, input))
	chatBroadcaster := events.NewChatBroadcaster()
	h.SetChatBroadcaster(chatBroadcaster)
	sub, err := chatBroadcaster.Subscribe()
	require.NoError(t, err)
	defer chatBroadcaster.Unsubscribe(sub)

	req := httptest.NewRequest(http.MethodPost, "/thread-inputs/"+input.ID+"/cancel", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	select {
	case event := <-sub:
		require.Equal(t, events.ChatThreadInputCancelled, event.Type)
		require.Equal(t, project.ID, event.ProjectID)
		require.Equal(t, input.ID, event.PendingInputID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for chat thread input cancellation event")
	}
}
