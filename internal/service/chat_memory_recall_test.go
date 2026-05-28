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

	"github.com/openvibely/openvibely/internal/lifecycle"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type chatRecallHookStore struct {
	hooks []models.AgentLifecycleHook
	seen  []models.LifecycleWhen
}

func (s *chatRecallHookStore) HooksForWhen(ctx context.Context, when models.LifecycleWhen) ([]models.AgentLifecycleHook, error) {
	s.seen = append(s.seen, when)
	var out []models.AgentLifecycleHook
	for _, h := range s.hooks {
		if h.When == when && h.Enabled {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *chatRecallHookStore) CreateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	if e.ID == "" {
		e.ID = "exec-" + string(e.When)
	}
	return nil
}

func (s *chatRecallHookStore) UpdateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	return nil
}

func (s *chatRecallHookStore) FindExecutionByIdempotencyKey(ctx context.Context, key string) (*models.LifecycleExecution, error) {
	return nil, sql.ErrNoRows
}

type chatRecallHookInvoker struct {
	seen []string
}

type memoryReadingCaptureAdapter struct {
	lastReq llmcontracts.AgentRequest
}

func (c *memoryReadingCaptureAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	c.lastReq = req
	rt := llmcontracts.RuntimeToolsFromContext(req.Ctx)
	if rt == nil || !rt.HasDefinition("read_file") || !rt.HasDefinition("list_files") {
		return llmcontracts.AgentResult{}, fmt.Errorf("missing scoped memory read tools")
	}
	out, handled, isErr, err := rt.Executor(req.Ctx, "read_file", json.RawMessage(`{"file_path":"chat_memory_probe.md"}`))
	if !handled || isErr || err != nil {
		return llmcontracts.AgentResult{}, fmt.Errorf("read memory file handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	return llmcontracts.AgentResult{Output: strings.TrimSpace(out), TextOnlyOutput: strings.TrimSpace(out), Usage: llmcontracts.Usage{TotalTokens: 1}}, nil
}

func (i *chatRecallHookInvoker) Invoke(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
	i.seen = append(i.seen, string(hook.When)+"/"+hook.SkillKey)
	if hook.When == models.LifecycleBeforeRun {
		return json.Marshal(lifecycle.ContextBlock{Content: "Remember: chat should use repo-local project memory.", Sources: []string{"MEMORIES.md"}, Confidence: 0.9})
	}
	return json.Marshal(lifecycle.ActivitySummary{Summary: "updated memory", ChangedPaths: []string{"chat.md"}})
}

func TestPrepareRecallOnlyLifecycleTurnInjectsChatMemoryWithoutExtraction(t *testing.T) {
	ctx := context.Background()
	store := &chatRecallHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "other-before", When: models.LifecycleBeforeRun, SkillKey: "load_context", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "recall", When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "update", When: models.LifecycleAfterComplete, SkillKey: "update_memory", OutputContract: models.OutputContractActivitySummary, Blocking: false, Enabled: true},
	}}
	invoker := &chatRecallHookInvoker{}
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(lifecycle.NewRunner(store, invoker, nil))

	turn := worker.PrepareRecallOnlyLifecycleTurn(ctx, models.Task{ID: "chat-task", ProjectID: "project-1", Category: models.CategoryChat, Title: "Chat", Prompt: "Use remembered project context"})
	instructions := additionalProjectInstructionsFromContext(turn.Ctx)
	if !strings.Contains(instructions, "Remember: chat should use repo-local project memory.") || !strings.Contains(instructions, "[recall_memory]") {
		t.Fatalf("expected recall memory in chat lifecycle context, got:\n%s", instructions)
	}
	if len(invoker.seen) != 1 || invoker.seen[0] != "before_run/recall_memory" {
		t.Fatalf("expected only before_run recall hook, got %#v", invoker.seen)
	}
	turn.AfterComplete(nil, llmcontracts.ChatContext{})
	for _, when := range store.seen {
		if when == models.LifecycleAfterComplete {
			t.Fatalf("recall-only chat lifecycle must not run after_complete, saw %#v", store.seen)
		}
	}
}

func TestPrepareRecallOnlyLifecycleTurnUsesOnlyMemoryCuratorOwner(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	agentRepo := repository.NewAgentRepo(db)

	memoryAgent := &models.Agent{Key: models.AgentSystemKindMemoryCurator, Name: "System: Memory Curator", SystemKind: models.AgentSystemKindMemoryCurator, Model: "inherit", Enabled: true, GeneratedStatus: models.AgentStatusProtected}
	if err := agentRepo.Create(ctx, memoryAgent); err != nil {
		t.Fatalf("create memory agent: %v", err)
	}
	otherAgent := &models.Agent{Key: "custom_recaller", Name: "Custom Recaller", Model: "inherit", Enabled: true}
	if err := agentRepo.Create(ctx, otherAgent); err != nil {
		t.Fatalf("create other agent: %v", err)
	}

	store := &chatRecallHookStore{hooks: []models.AgentLifecycleHook{
		{ID: "wrong-owner", AgentID: otherAgent.ID, When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "memory-owner", AgentID: memoryAgent.ID, When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true},
		{ID: "update", AgentID: memoryAgent.ID, When: models.LifecycleAfterComplete, SkillKey: "update_memory", OutputContract: models.OutputContractActivitySummary, Enabled: true},
	}}
	invoked := []string{}
	runner := lifecycle.NewRunner(store, routeHookInvokerFunc(func(ctx context.Context, hook models.AgentLifecycleHook, in lifecycle.HookInput) (json.RawMessage, error) {
		invoked = append(invoked, hook.ID)
		return json.Marshal(lifecycle.ContextBlock{Content: "Remembered from Memory Curator only.", Sources: []string{"MEMORIES.md"}, Confidence: 0.95})
	}), nil)
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleAgentRepo(agentRepo)
	worker.SetLifecycleRunner(runner)

	turn := worker.PrepareRecallOnlyLifecycleTurn(ctx, models.Task{ID: "chat-task", ProjectID: "project-1", Category: models.CategoryChat, Title: "Chat", Prompt: "What do you remember about chat memory?"})
	instructions := additionalProjectInstructionsFromContext(turn.Ctx)
	if !strings.Contains(instructions, "Remembered from Memory Curator only.") {
		t.Fatalf("expected Memory Curator recall in chat context, got:\n%s", instructions)
	}
	if len(invoked) != 1 || invoked[0] != "memory-owner" {
		t.Fatalf("expected only Memory Curator recall hook, got %#v", invoked)
	}
	turn.AfterComplete(nil, llmcontracts.ChatContext{})
	for _, when := range store.seen {
		if when == models.LifecycleAfterComplete {
			t.Fatalf("chat recall must not run memory extraction, saw slots %#v", store.seen)
		}
	}
}

func TestCallAgentDirectWithDefinitionMemoryCuratorCanReadRealMemoryFile(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	capture := &memoryReadingCaptureAdapter{}
	svc.providerAdapters[models.ProviderTest] = capture

	repoPath := t.TempDir()
	memoryDir := filepath.Join(repoPath, ".openvibely", "memories")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "MEMORIES.md"), []byte("# Memories\n\n- chat_memory_probe.md: Chat should recall the user's preferred test keyword.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "chat_memory_probe.md"), []byte("The user prefers answers to mention kiwi-signal when discussing chat memory recall."), 0o644); err != nil {
		t.Fatal(err)
	}
	memoryAgent := &models.Agent{
		Key:        models.AgentSystemKindMemoryCurator,
		Name:       "System: Memory Curator",
		SystemKind: models.AgentSystemKindMemoryCurator,
		Model:      "inherit",
		Enabled:    true,
		Tools:      []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{ScopedFiles: []models.ScopedFilesConfig{{Directory: ".openvibely/memories", Permissions: []string{"read", "write", "delete"}}}, SkipDefaultTools: true, DisableRuntimeWorktree: true},
	}
	out, _, err := svc.CallAgentDirectWithDefinition(context.Background(), "read relevant memory", nil, models.LLMConfig{Provider: models.ProviderTest, Model: "test-model"}, repoPath, memoryAgent)
	if err != nil {
		t.Fatalf("CallAgentDirectWithDefinition: %v", err)
	}
	if !strings.Contains(out, "kiwi-signal") {
		t.Fatalf("expected real memory file content through scoped runtime, got %q", out)
	}
	if capture.lastReq.WorkDir != memoryDir {
		t.Fatalf("expected Memory Curator scoped workdir %q, got %q", memoryDir, capture.lastReq.WorkDir)
	}
}

func TestCallAgentDirectStreamingDetailedIncludesRecallOnlyMemoryInChatContext(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	capture := &captureProviderAdapter{}
	svc.providerAdapters[models.ProviderTest] = capture

	ctx := context.Background()
	store := &chatRecallHookStore{hooks: []models.AgentLifecycleHook{{ID: "recall", When: models.LifecycleBeforeRun, SkillKey: "recall_memory", OutputContract: models.OutputContractContextBlock, Blocking: true, Enabled: true}}}
	invoker := &chatRecallHookInvoker{}
	worker := NewWorkerService(nil, 0, nil)
	worker.SetLifecycleRunner(lifecycle.NewRunner(store, invoker, nil))
	turn := worker.PrepareRecallOnlyLifecycleTurn(ctx, models.Task{ID: "chat-task", ProjectID: "project-1", Category: models.CategoryChat, Title: "Chat", Prompt: "Use remembered project context"})

	_, err := svc.CallAgentDirectStreamingDetailed(turn.Ctx, "What project memory matters?", nil, models.LLMConfig{Provider: models.ProviderTest, Model: "test-model"}, "exec-1", []models.Execution{}, "base chat context", "", nil, false)
	if err != nil {
		t.Fatalf("CallAgentDirectStreamingDetailed: %v", err)
	}
	if !strings.Contains(capture.lastReq.ChatSystemContext, "base chat context") {
		t.Fatalf("expected original chat context, got %q", capture.lastReq.ChatSystemContext)
	}
	if !strings.Contains(capture.lastReq.ChatSystemContext, "Remember: chat should use repo-local project memory.") || !strings.Contains(capture.lastReq.ChatSystemContext, "[recall_memory]") {
		t.Fatalf("expected recall memory in model-facing chat context, got:\n%s", capture.lastReq.ChatSystemContext)
	}
}
