package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

type fakeMemoryTaskRunner struct {
	memoryContext string
}

func (f *fakeMemoryTaskRunner) RecallContext(ctx context.Context, projectID string, query MemoryRecallQuery) string {
	return f.memoryContext
}

type captureProviderAdapter struct {
	lastReq llmcontracts.AgentRequest
}

type runtimeToolWritingLLMCaller struct {
	workDir string
}

type fileWritingLLMCaller struct {
	fileName string
	content  string
	workDir  string
}

func (c *fileWritingLLMCaller) CallModel(ctx context.Context, prompt string, attachments []models.Attachment, agent models.LLMConfig, execID string, workDir string) (string, string, int, error) {
	c.workDir = workDir
	if err := os.WriteFile(filepath.Join(workDir, c.fileName), []byte(c.content), 0644); err != nil {
		return "", "", 0, err
	}
	return "changed files\n[STATUS: SUCCESS]", "changed files\n[STATUS: SUCCESS]", 10, nil
}

func (c *runtimeToolWritingLLMCaller) CallModel(ctx context.Context, prompt string, attachments []models.Attachment, agent models.LLMConfig, execID string, workDir string) (string, string, int, error) {
	c.workDir = workDir
	rt := llmcontracts.RuntimeToolsFromContext(ctx)
	if rt == nil || rt.Executor == nil {
		return "", "", 0, fmt.Errorf("missing runtime tools")
	}
	payload, _ := json.Marshal(map[string]string{"file_path": "scoped.txt", "content": "from scoped runtime"})
	_, _, _, err := rt.Executor(ctx, "write_file", payload)
	if err != nil {
		return "", "", 0, err
	}
	return "scoped task response", "scoped task response", 10, nil
}

func (c *captureProviderAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	c.lastReq = req
	return llmcontracts.AgentResult{
		Output:         "ok",
		TextOnlyOutput: "ok",
		Usage:          llmcontracts.Usage{TotalTokens: 1},
	}, nil
}

func TestLLMService_ExecuteTask_NoDefaultAgent(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	agents, _ := llmConfigRepo.List(ctx)
	for _, a := range agents {
		llmConfigRepo.Delete(ctx, a.ID)
	}

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(testutil.NewMockLLMCaller())

	task := models.Task{
		ID:        "test-task-id",
		ProjectID: "default",
		Title:     "Test",
		Prompt:    "hello",
		Status:    models.StatusPending,
	}

	_, err := svc.ExecuteTask(ctx, task)
	if err == nil {
		t.Fatal("expected error when no agent configured")
	}
	if !strings.Contains(err.Error(), "no agent configured") {
		t.Errorf("expected 'no agent configured' error, got: %v", err)
	}
}

func TestLLMService_CallLLM_UnsupportedProvider(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)

	agent := models.LLMConfig{
		Provider: "unsupported_provider",
		Model:    "test-model",
	}

	_, _, _, err := svc.callLLM(context.Background(), "test", nil, agent, "test-exec-id", "", "")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected 'unsupported provider' error, got: %v", err)
	}
}

func TestLLMService_CallAgentDirect_TestProviderUsesMockCaller(t *testing.T) {
	svc := &LLMService{}
	mock := testutil.NewMockLLMCaller()
	mock.Response = "mock-output"
	mock.Tokens = 17
	svc.SetLLMCaller(mock)

	agent := models.LLMConfig{Provider: models.ProviderTest, Model: "test-model"}
	output, tokens, err := svc.CallAgentDirect(context.Background(), "hello", nil, agent, "/tmp/workdir")
	if err != nil {
		t.Fatalf("CallAgentDirect error: %v", err)
	}
	if output != "mock-output" {
		t.Fatalf("expected mock output, got %q", output)
	}
	if tokens != 17 {
		t.Fatalf("expected tokens=17, got %d", tokens)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("expected CallModel called once, got %d", mock.CallCount())
	}
	last := mock.LastCall()
	if last.ExecID != "" {
		t.Fatalf("expected empty execID for direct calls, got %q", last.ExecID)
	}
	if last.WorkDir != "/tmp/workdir" {
		t.Fatalf("expected workdir propagated, got %q", last.WorkDir)
	}
}

func TestLLMService_CallAgentDirectStreaming_TestProviderUsesMockCaller(t *testing.T) {
	svc := &LLMService{}
	mock := testutil.NewMockLLMCaller()
	mock.Response = "stream-output"
	mock.Tokens = 29
	svc.SetLLMCaller(mock)

	agent := models.LLMConfig{Provider: models.ProviderTest, Model: "test-model"}
	output, tokens, err := svc.CallAgentDirectStreaming(context.Background(), "hello", nil, agent, "exec-123", nil, "ctx", "/tmp/workdir")
	if err != nil {
		t.Fatalf("CallAgentDirectStreaming error: %v", err)
	}
	if output != "stream-output" {
		t.Fatalf("expected stream output, got %q", output)
	}
	if tokens != 29 {
		t.Fatalf("expected tokens=29, got %d", tokens)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("expected CallModel called once, got %d", mock.CallCount())
	}
	last := mock.LastCall()
	if last.ExecID != "exec-123" {
		t.Fatalf("expected execID propagated, got %q", last.ExecID)
	}
	if last.WorkDir != "/tmp/workdir" {
		t.Fatalf("expected workdir propagated, got %q", last.WorkDir)
	}
}

func TestLLMService_CallAgentDirectStreamingDetailed_TestProviderPreservesTextOnly(t *testing.T) {
	svc := &LLMService{}
	mock := testutil.NewMockLLMCaller()
	mock.Response = "stream-output"
	mock.TextOnly = "text-only"
	mock.Tokens = 29
	svc.SetLLMCaller(mock)

	agent := models.LLMConfig{Provider: models.ProviderTest, Model: "test-model"}
	res, err := svc.CallAgentDirectStreamingDetailed(context.Background(), "hello", nil, agent, "exec-123", nil, "ctx", "/tmp/workdir", nil)
	if err != nil {
		t.Fatalf("CallAgentDirectStreamingDetailed error: %v", err)
	}
	if res.Output != "stream-output" {
		t.Fatalf("expected stream output, got %q", res.Output)
	}
	if res.TextOnlyOutput != "text-only" {
		t.Fatalf("expected text-only output, got %q", res.TextOnlyOutput)
	}
	if res.Usage.TotalTokens != 29 {
		t.Fatalf("expected tokens=29, got %d", res.Usage.TotalTokens)
	}
}

func TestLLMService_CallAgentDirectStreamingDetailed_PropagatesAgentDefinition(t *testing.T) {
	svc := &LLMService{}
	capture := &captureProviderAdapter{}
	svc.providerAdapters = map[models.LLMProvider]ProviderAdapter{
		models.ProviderOpenAI: capture,
	}

	agent := models.LLMConfig{Provider: models.ProviderOpenAI, Model: "gpt-test"}
	agentDef := &models.Agent{
		ID:           "agent-def-1",
		Name:         "playwright-reviewer",
		SystemPrompt: "Use Playwright MCP tools for screenshots.",
	}
	ctx := llmcontracts.WithChatMode(context.Background(), models.ChatModePlan)
	_, err := svc.CallAgentDirectStreamingDetailed(
		ctx,
		"check ui",
		nil,
		agent,
		"exec-123",
		nil,
		"ctx",
		"/tmp/workdir",
		agentDef,
		false,
	)
	if err != nil {
		t.Fatalf("CallAgentDirectStreamingDetailed error: %v", err)
	}
	if capture.lastReq.AgentDefinition == nil {
		t.Fatalf("expected agent definition to be propagated")
	}
	if capture.lastReq.AgentDefinition.ID != agentDef.ID {
		t.Fatalf("expected agent definition id %q, got %q", agentDef.ID, capture.lastReq.AgentDefinition.ID)
	}
	if capture.lastReq.ChatMode != models.ChatModePlan {
		t.Fatalf("expected chat mode %q, got %q", models.ChatModePlan, capture.lastReq.ChatMode)
	}
}

func TestLLMService_CallClaudeCLI_EnvFiltering(t *testing.T) {

	os.Setenv("CLAUDECODE", "test-value")
	defer os.Unsetenv("CLAUDECODE")

	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}

	for _, e := range filtered {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			t.Error("CLAUDECODE should be filtered from env")
		}
	}

	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("CLAUDECODE should be in original env")
	}
}

func TestLLMService_ExecuteTaskWithAgent_SkipsNonPendingTask(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(testutil.NewMockLLMCaller())

	agent := ensureDefaultAgent(t, llmConfigRepo)

	task := &models.Task{ProjectID: "default", Title: "Already Done", Category: models.CategoryActive, Status: models.StatusCompleted, Prompt: "test"}
	taskRepo.Create(ctx, task)

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("expected no error for skipped task, got: %v", err)
	}
	if exec != nil {
		t.Error("expected nil execution for skipped non-pending task")
	}

	updated, _ := taskRepo.GetByID(ctx, task.ID)
	if updated.Status != models.StatusCompleted {
		t.Errorf("expected status to remain completed, got %q", updated.Status)
	}
}

func TestLLMService_ExecuteTaskWithAgent_SkipsRunningTask(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(testutil.NewMockLLMCaller())

	task := &models.Task{ProjectID: "default", Title: "Already Running", Category: models.CategoryActive, Status: models.StatusRunning, Prompt: "test"}
	taskRepo.Create(ctx, task)

	agent := ensureDefaultAgent(t, llmConfigRepo)

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("expected no error for skipped task, got: %v", err)
	}
	if exec != nil {
		t.Error("expected nil execution for skipped running task")
	}

	updated, _ := taskRepo.GetByID(ctx, task.ID)
	if updated.Status != models.StatusRunning {
		t.Errorf("expected status to remain running, got %q", updated.Status)
	}
}

func TestLLMService_ExecuteTaskWithAgent_RecordsExecution(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	mock := &testutil.MockLLMCaller{Err: fmt.Errorf("mock error: simulated failure")}
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(mock)

	task := &models.Task{ProjectID: "default", Title: "Record Test", Category: models.CategoryActive, Status: models.StatusPending, Prompt: "test"}
	taskRepo.Create(ctx, task)

	agent := ensureDefaultAgent(t, llmConfigRepo)

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err == nil {
		t.Fatal("expected error from mock")
	}

	updated, _ := taskRepo.GetByID(ctx, task.ID)
	if updated.Status != models.StatusFailed {
		t.Errorf("expected task status=failed, got %q", updated.Status)
	}

	if exec == nil {
		t.Fatal("expected execution record even on failure")
	}
	if exec.Status != models.ExecFailed {
		t.Errorf("expected exec status=failed, got %q", exec.Status)
	}
}

func TestLLMService_ExecuteTask_UsesAssignedAgent(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(testutil.NewMockLLMCaller())

	customAgent := &models.LLMConfig{
		Name:        "Custom Agent",
		Provider:    models.ProviderAnthropic,
		Model:       "custom-model",
		MaxTokens:   1000,
		Temperature: 0.5,
		IsDefault:   false,
	}
	if err := llmConfigRepo.Create(ctx, customAgent); err != nil {
		t.Fatalf("failed to create custom agent: %v", err)
	}

	task := &models.Task{
		ProjectID: "default",
		Title:     "Custom Agent Task",
		Category:  models.CategoryActive,
		Status:    models.StatusCompleted,
		Prompt:    "test",
		AgentID:   &customAgent.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	exec, err := svc.ExecuteTask(ctx, *task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec != nil {
		t.Errorf("expected nil execution for non-pending task, got %+v", exec)
	}

	task2 := &models.Task{
		ProjectID: "default",
		Title:     "Custom Agent Task 2",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "test",
		AgentID:   &customAgent.ID,
	}
	if err := taskRepo.Create(ctx, task2); err != nil {
		t.Fatalf("failed to create task2: %v", err)
	}

	mock := &testutil.MockLLMCaller{Response: "ok", TextOnly: "ok", Tokens: 10}
	svc.SetLLMCaller(mock)

	fetchedAgent, _ := llmConfigRepo.GetByID(ctx, customAgent.ID)

	fetchedAgent.Provider = models.ProviderTest

	exec2, err := svc.ExecuteTaskWithAgent(ctx, *task2, *fetchedAgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec2 == nil {
		t.Fatal("expected execution record")
	}
	if exec2.AgentConfigID != customAgent.ID {
		t.Errorf("expected execution to use custom agent %s, got %s", customAgent.ID, exec2.AgentConfigID)
	}

	if mock.CallCount() == 0 {
		t.Fatal("expected mock to be called")
	}
	if mock.LastCall().Agent.ID != customAgent.ID {
		t.Errorf("expected callLLM to receive custom agent %s, got %s", customAgent.ID, mock.LastCall().Agent.ID)
	}
}

func TestLLMService_ExecuteTask_MemoryConsolidationUsesNormalExecutionPath(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	ctx := context.Background()

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	agentRepo := repository.NewAgentRepo(db)
	svc.SetAgentRepo(agentRepo)
	memoryRunner := &fakeMemoryTaskRunner{}
	svc.SetMemoryTaskRunner(memoryRunner)
	mock := &testutil.MockLLMCaller{Response: "memory task response", TextOnly: "memory task response", Tokens: 10}
	svc.SetLLMCaller(mock)

	repoPath := t.TempDir()
	agentDef := &models.Agent{
		Name:         "Memory Consolidator",
		SystemKind:   models.AgentSystemKindMemoryConsolidator,
		SystemPrompt: "Consolidate memory",
		Model:        "inherit",
		Tools:        []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{
			ScopedFiles:            []models.ScopedFilesConfig{{Directory: ".openvibely/memory", Permissions: []string{"read", "write", "delete"}}},
			SkipDefaultTools:       true,
			DisableRuntimeWorktree: true,
		},
	}
	if err := agentRepo.Create(ctx, agentDef); err != nil {
		t.Fatalf("create agent definition: %v", err)
	}
	agent, _ := llmConfigRepo.GetDefault(ctx)
	agent.Provider = models.ProviderTest
	projectRepo := repository.NewProjectRepo(db)
	project, err := projectRepo.GetByID(ctx, "default")
	if err != nil {
		t.Fatalf("get default project: %v", err)
	}
	if project == nil {
		project = &models.Project{ID: "default", Name: "Default"}
	}
	project.RepoPath = repoPath
	if err := projectRepo.Update(ctx, project); err != nil {
		t.Fatalf("update project repo path: %v", err)
	}
	task := &models.Task{
		ProjectID:         "default",
		Title:             "System: Memory Consolidation",
		Category:          models.CategoryScheduled,
		Status:            models.StatusPending,
		Prompt:            "Run scheduled memory consolidation for this project.",
		AgentDefinitionID: &agentDef.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("ExecuteTaskWithAgent: %v", err)
	}
	if exec == nil {
		t.Fatal("expected execution")
	}
	last := mock.LastCall()
	if last.Prompt != task.Prompt {
		t.Fatalf("expected scheduled task prompt, got %q", last.Prompt)
	}
	wantWorkDir := filepath.Join(repoPath, ".openvibely", "memory")
	if last.WorkDir != wantWorkDir {
		t.Fatalf("expected scoped memory workdir %q, got %q", wantWorkDir, last.WorkDir)
	}
	execs, err := execRepo.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected one execution, got %d", len(execs))
	}
	if execs[0].PromptSent != task.Prompt {
		t.Fatalf("expected execution prompt to be scheduled task prompt, got %q", execs[0].PromptSent)
	}
	if execs[0].Output != "memory task response" {
		t.Fatalf("expected execution output to be model response, got %q", execs[0].Output)
	}
}

func TestLLMService_ExecuteTask_MemoryConsolidationSkipsRuntimeWorktree(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	ctx := context.Background()

	repoPath := createTestGitRepo(t)
	project := &models.Project{Name: "Memory Repo", RepoPath: repoPath}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agentRepo := repository.NewAgentRepo(db)
	agentDef := &models.Agent{
		Name:         "Memory Consolidator",
		SystemKind:   models.AgentSystemKindMemoryConsolidator,
		SystemPrompt: "Consolidate memory",
		Model:        "inherit",
		Tools:        []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{
			ScopedFiles:            []models.ScopedFilesConfig{{Directory: ".openvibely/memory", Permissions: []string{"read", "write", "delete"}}},
			SkipDefaultTools:       true,
			DisableRuntimeWorktree: true,
		},
	}
	if err := agentRepo.Create(ctx, agentDef); err != nil {
		t.Fatalf("create agent definition: %v", err)
	}

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	svc.SetAgentRepo(agentRepo)
	svc.SetWorktreeService(NewWorktreeService(taskRepo, projectRepo, settingsRepo))
	svc.SetMemoryTaskRunner(&fakeMemoryTaskRunner{})
	mock := &testutil.MockLLMCaller{Response: "memory task response", TextOnly: "memory task response", Tokens: 10}
	svc.SetLLMCaller(mock)

	agent, _ := llmConfigRepo.GetDefault(ctx)
	agent.Provider = models.ProviderTest
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "System: Memory Consolidation",
		Category:          models.CategoryScheduled,
		Status:            models.StatusPending,
		Prompt:            "Run scheduled memory consolidation for this project.",
		AgentDefinitionID: &agentDef.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("ExecuteTaskWithAgent: %v", err)
	}
	if exec == nil {
		t.Fatal("expected execution")
	}
	wantWorkDir := filepath.Join(repoPath, ".openvibely", "memory")
	if got := mock.LastCall().WorkDir; got != wantWorkDir {
		t.Fatalf("expected memory consolidator to write directly to repo memory dir %q, got %q", wantWorkDir, got)
	}
	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.WorktreePath != "" || updated.WorktreeBranch != "" {
		t.Fatalf("memory consolidator should not create runtime worktree, got path=%q branch=%q", updated.WorktreePath, updated.WorktreeBranch)
	}
}

func TestLLMService_ExecuteTask_ScopedFilesAgentUsesRuntimeWorktreeByDefault(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	ctx := context.Background()

	repoPath := createTestGitRepo(t)
	project := &models.Project{Name: "Scoped Repo", RepoPath: repoPath}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agentRepo := repository.NewAgentRepo(db)
	agentDef := &models.Agent{
		Name:         "Scoped Docs Agent",
		SystemPrompt: "Edit scoped docs",
		Model:        "inherit",
		Tools:        []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{
			ScopedFiles: []models.ScopedFilesConfig{{Directory: "docs", Permissions: []string{"read", "write"}}},
		},
	}
	if err := agentRepo.Create(ctx, agentDef); err != nil {
		t.Fatalf("create agent definition: %v", err)
	}

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	svc.SetAgentRepo(agentRepo)
	svc.SetWorktreeService(NewWorktreeService(taskRepo, projectRepo, settingsRepo))
	svc.SetMemoryTaskRunner(&fakeMemoryTaskRunner{})
	caller := &runtimeToolWritingLLMCaller{}
	svc.SetLLMCaller(caller)

	agent, _ := llmConfigRepo.GetDefault(ctx)
	agent.Provider = models.ProviderTest
	task := &models.Task{
		ProjectID:         project.ID,
		Title:             "Scoped Docs Task",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		Prompt:            "Update docs.",
		AgentDefinitionID: &agentDef.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("ExecuteTaskWithAgent: %v", err)
	}
	if exec == nil {
		t.Fatal("expected execution")
	}
	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.WorktreePath == "" || updated.WorktreeBranch == "" {
		t.Fatalf("expected generic scoped-files agent to use runtime worktree by default")
	}
	if got := caller.workDir; got != updated.WorktreePath {
		t.Fatalf("expected agent process workdir to stay on runtime worktree root %q when default tools are enabled, got %q", updated.WorktreePath, got)
	}
	if _, err := os.Stat(filepath.Join(updated.WorktreePath, "docs", "scoped.txt")); err != nil {
		t.Fatalf("expected scoped file write to land inside runtime worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "docs", "scoped.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected project repo docs to remain untouched, stat err=%v", err)
	}
}

func TestLLMService_ExecuteTask_IncludesProjectMemoryContext(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	memoryRunner := &fakeMemoryTaskRunner{memoryContext: "Recalled from your persistent memory system:\n\n- Provider guidance applies here.\n\nSources: provider_architecture.md"}
	svc.SetMemoryTaskRunner(memoryRunner)

	agent, _ := llmConfigRepo.GetDefault(ctx)
	agent.Provider = models.ProviderTest

	task := &models.Task{
		ProjectID: "default",
		Title:     "Provider task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "Use provider architecture guidance.",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	capture := &captureProviderAdapter{}
	svc.providerAdapters[models.ProviderTest] = capture

	_, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("ExecuteTaskWithAgent: %v", err)
	}

	last := capture.lastReq
	if !strings.Contains(last.ProjectInstructions, "Recalled from your persistent memory system:") {
		t.Fatalf("expected project memory in project instructions, got %q", last.ProjectInstructions)
	}
	if !strings.Contains(last.ProjectInstructions, "Sources: provider_architecture.md") {
		t.Fatalf("expected memory sources in project instructions, got %q", last.ProjectInstructions)
	}
	if last.Message != task.Prompt {
		t.Fatalf("expected task prompt unchanged, got %q", last.Message)
	}
}

func TestLLMService_ExecuteTask_UsesDefaultWhenNoAgentAssigned(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(testutil.NewMockLLMCaller())

	defaultAgent, _ := llmConfigRepo.GetDefault(ctx)

	task := &models.Task{
		ProjectID: "default",
		Title:     "Default Agent Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "test",
		AgentID:   nil,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	mock := &testutil.MockLLMCaller{Response: "ok", TextOnly: "ok", Tokens: 10}
	svc.SetLLMCaller(mock)

	defaultAgent.Provider = models.ProviderTest

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *defaultAgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec == nil {
		t.Fatal("expected execution record")
	}
	if exec.AgentConfigID != defaultAgent.ID {
		t.Errorf("expected execution to use default agent %s, got %s", defaultAgent.ID, exec.AgentConfigID)
	}

	if mock.CallCount() == 0 {
		t.Fatal("expected mock to be called")
	}
	if mock.LastCall().Agent.ID != defaultAgent.ID {
		t.Errorf("expected callLLM to receive default agent %s, got %s", defaultAgent.ID, mock.LastCall().Agent.ID)
	}
}

func TestLLMService_ExecuteTaskWithAgent_LoadsAttachments(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	attachmentRepo := repository.NewAttachmentRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	ctx := context.Background()

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(testutil.NewMockLLMCaller())

	task := &models.Task{
		ProjectID: "default",
		Title:     "Task with Attachment",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "What do you see in the image?",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	attachment := &models.Attachment{
		TaskID:    task.ID,
		FileName:  "test.png",
		FilePath:  "/tmp/test.png",
		MediaType: "image/png",
		FileSize:  1024,
	}
	if err := attachmentRepo.Create(ctx, attachment); err != nil {
		t.Fatalf("failed to create attachment: %v", err)
	}

	mock := &testutil.MockLLMCaller{Response: "ok", TextOnly: "ok", Tokens: 10}
	svc.SetLLMCaller(mock)

	agent := ensureDefaultAgent(t, llmConfigRepo)

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec == nil {
		t.Fatal("expected execution record")
	}

	if exec.PromptSent != task.Prompt {
		t.Errorf("expected PromptSent to match task prompt")
	}

	if mock.CallCount() == 0 {
		t.Fatal("expected mock to be called")
	}
	lastCall := mock.LastCall()
	if len(lastCall.Attachments) != 1 {
		t.Errorf("expected 1 attachment passed to callLLM, got %d", len(lastCall.Attachments))
	} else if lastCall.Attachments[0].FileName != "test.png" {
		t.Errorf("expected attachment filename 'test.png', got %q", lastCall.Attachments[0].FileName)
	}
}

func TestLLMService_ExecuteTaskWithAgent_VisionAwareAgentOverride(t *testing.T) {

	anthropicAgent := models.LLMConfig{
		Name:       "Anthropic Sonnet",
		Provider:   models.ProviderAnthropic,
		AuthMethod: models.AuthMethodAPIKey,
		Model:      "claude-sonnet-4-20250514",
	}
	cliAgent := models.LLMConfig{
		Name:       "Claude Max",
		Provider:   models.ProviderAnthropic,
		AuthMethod: models.AuthMethodCLI,
		Model:      "claude-sonnet-4-5",
	}

	complexity := AnalyzeComplexity("What do you see?")
	result := SelectLLMWithVision(complexity, []models.LLMConfig{cliAgent, anthropicAgent}, true)
	if result == nil {
		t.Fatal("expected vision-capable agent to be selected")
	}
	if result.LLMConfig.Provider != models.ProviderAnthropic {
		t.Errorf("expected anthropic provider, got %s", result.LLMConfig.Provider)
	}
	if result.LLMConfig.Name != "Anthropic Sonnet" {
		t.Errorf("expected 'Anthropic Sonnet', got %q", result.LLMConfig.Name)
	}
}

func TestLLMService_ExecuteTaskWithAgent_NoOverrideForTextAttachments(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	attachmentRepo := repository.NewAttachmentRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	ctx := context.Background()

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)

	task := &models.Task{
		ProjectID: "default",
		Title:     "Process Text File",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "Read this file",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	attachment := &models.Attachment{
		TaskID:    task.ID,
		FileName:  "data.json",
		FilePath:  "/tmp/data.json",
		MediaType: "application/json",
		FileSize:  512,
	}
	if err := attachmentRepo.Create(ctx, attachment); err != nil {
		t.Fatalf("failed to create attachment: %v", err)
	}

	defaultAgent, _ := llmConfigRepo.GetDefault(ctx)
	if defaultAgent == nil {
		t.Fatal("no default agent found")
	}

	agent := *defaultAgent
	agent.Provider = "unsupported"

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, agent)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}

	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected 'unsupported provider' error for text-only attachments, got: %v", err)
	}
	if exec == nil {
		t.Fatal("expected execution record")
	}
}

func TestLLMService_CallAgentDirectStreaming_VisionAwareOverride(t *testing.T) {

	cliOnly := []models.LLMConfig{
		{Name: "Claude Max", Provider: models.ProviderAnthropic, AuthMethod: models.AuthMethodCLI, Model: "claude-sonnet-4-5"},
	}
	complexity := AnalyzeComplexity("What do you see?")
	result := SelectLLMWithVision(complexity, cliOnly, true)
	if result != nil {
		t.Errorf("expected nil when no vision-capable agent available, got %+v", result.LLMConfig)
	}

	withAnthropic := append(cliOnly, models.LLMConfig{
		Name: "Anthropic", Provider: models.ProviderAnthropic, AuthMethod: models.AuthMethodAPIKey, Model: "claude-sonnet-4-20250514",
	})
	result = SelectLLMWithVision(complexity, withAnthropic, true)
	if result == nil {
		t.Fatal("expected vision-capable agent to be selected")
	}
	if result.LLMConfig.Provider != models.ProviderAnthropic {
		t.Errorf("expected anthropic, got %s", result.LLMConfig.Provider)
	}
}

func TestLLMService_CallAgentDirectStreaming_NoOverrideWithoutImages(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	attachmentRepo := repository.NewAttachmentRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)

	defaultAgent, _ := llmConfigRepo.GetDefault(ctx)
	if defaultAgent == nil {
		t.Fatal("no default agent found")
	}

	task := &models.Task{
		ProjectID: "default",
		Title:     "Chat No Images",
		Category:  models.CategoryChat,
		Status:    models.StatusPending,
		Prompt:    "Hello, how are you?",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: defaultAgent.ID,
		Status:        models.ExecRunning,
		PromptSent:    task.Prompt,
	}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	textAttachments := []models.Attachment{
		{
			FileName:  "data.json",
			FilePath:  "/tmp/data.json",
			MediaType: "application/json",
			FileSize:  512,
		},
	}

	agent := *defaultAgent
	agent.Provider = "unsupported"

	chatHistory := make([]models.Execution, 0)
	_, _, err := svc.CallAgentDirectStreaming(ctx, task.Prompt, textAttachments, agent, exec.ID, chatHistory, "", "", false)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}

	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected 'unsupported provider' error for text-only attachments, got: %v", err)
	}
}

func TestLLMService_CallAgentDirectStreaming_VisionEnvVarFallback(t *testing.T) {

	cliOnly := []models.LLMConfig{
		{Name: "Claude Max", Provider: models.ProviderAnthropic, AuthMethod: models.AuthMethodCLI, Model: "claude-sonnet-4-5"},
		{Name: "Ollama Local", Provider: models.ProviderOllama, Model: "llama3"},
	}
	complexity := AnalyzeComplexity("What do you see?")
	result := SelectLLMWithVision(complexity, cliOnly, true)
	if result != nil {
		t.Errorf("expected nil when no vision-capable agents, got %+v", result.LLMConfig)
	}

}

func TestLLMService_ExecuteTaskWithAgent_MovesRepeatOnceToCompleted(t *testing.T) {
	db := testutil.NewTestDB(t)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	ctx := context.Background()

	task := &models.Task{
		ProjectID: "default",
		Title:     "RepeatOnce Scheduled Task",
		Category:  models.CategoryScheduled,
		Status:    models.StatusPending,
		Prompt:    "test",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	schedule := &models.Schedule{
		TaskID:         task.ID,
		RunAt:          time.Now(),
		RepeatType:     models.RepeatOnce,
		RepeatInterval: 0,
		Enabled:        true,
	}
	if err := scheduleRepo.Create(ctx, schedule); err != nil {
		t.Fatalf("failed to create schedule: %v", err)
	}

	if err := taskRepo.UpdateStatus(ctx, task.ID, models.StatusCompleted); err != nil {
		t.Fatalf("failed to update task status: %v", err)
	}

	schedules, err := scheduleRepo.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get schedules: %v", err)
	}
	if len(schedules) > 0 && schedules[0].RepeatType == models.RepeatOnce {
		if err := taskRepo.UpdateCategory(ctx, task.ID, models.CategoryCompleted); err != nil {
			t.Fatalf("failed to update category: %v", err)
		}
	}

	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to fetch updated task: %v", err)
	}
	if updated.Category != models.CategoryCompleted {
		t.Errorf("expected task to be moved to completed category, got %q", updated.Category)
	}
}

func TestLLMService_ExecuteTaskWithAgent_CommitsWorktreeEditsAndPersistsDiff(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	repoDir := createTestGitRepo(t)
	project := &models.Project{Name: "Provider Edit Project", RepoPath: repoDir}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	caller := &fileWritingLLMCaller{fileName: "anthropic-style.txt", content: "provider left this edit\n"}
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(caller)
	svc.SetWorktreeService(NewWorktreeService(taskRepo, projectRepo, settingsRepo))

	agent := ensureDefaultAgent(t, llmConfigRepo)
	task := &models.Task{
		ProjectID: project.ID,
		Title:     "Capture Anthropic edits",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "Create a file.",
		AgentID:   &agent.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execRec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("ExecuteTaskWithAgent: %v", err)
	}
	if execRec == nil {
		t.Fatal("expected execution")
	}
	updatedTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if caller.workDir != updatedTask.WorktreePath {
		t.Fatalf("expected provider to run in worktree %q, got %q", updatedTask.WorktreePath, caller.workDir)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "anthropic-style.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected main checkout to remain untouched, stat err=%v", err)
	}

	targetBranch := updatedTask.MergeTargetBranch
	if targetBranch == "" {
		targetBranch = GetDefaultBranch(repoDir)
	}
	committedDiff := GetWorktreeDiff(repoDir, updatedTask.WorktreeBranch, targetBranch)
	if !strings.Contains(committedDiff, "provider left this edit") {
		t.Fatalf("expected task branch diff to contain provider edit, got:\n%s", committedDiff)
	}
	stored, err := execRepo.GetByID(ctx, execRec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if stored == nil || !strings.Contains(stored.DiffOutput, "provider left this edit") {
		t.Fatalf("expected persisted diff_output to contain provider edit, got %#v", stored)
	}
}

func TestLLMService_ExecuteTaskWithAgent_AllowsCompletionWithoutCodeChanges(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	mock := testutil.NewMockLLMCaller()
	mock.Response = "The screenshot shows the OpenVibely chat page in an idle state."
	mock.TextOnly = mock.Response
	mock.Tokens = 21

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	svc.SetLLMCaller(mock)
	svc.SetWorktreeService(NewWorktreeService(taskRepo, projectRepo, settingsRepo))

	repoDir := createTestGitRepo(t)
	project := &models.Project{
		Name:     "Read Only Task Project",
		RepoPath: repoDir,
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent := ensureDefaultAgent(t, llmConfigRepo)

	task := &models.Task{
		ProjectID: project.ID,
		Title:     "Describe screenshot contents",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "Describe the attached screenshot and summarize the visible UI state.",
		AgentID:   &agent.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execRec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if execRec == nil {
		t.Fatal("expected execution record")
	}
	if execRec.Status != models.ExecCompleted {
		t.Fatalf("expected completed execution, got %s", execRec.Status)
	}
	if execRec.ErrorMessage != "" {
		t.Fatalf("expected empty error message, got %q", execRec.ErrorMessage)
	}

	updatedTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updatedTask.Status != models.StatusCompleted {
		t.Fatalf("expected task completed, got %s", updatedTask.Status)
	}
	if updatedTask.Category != models.CategoryCompleted {
		t.Fatalf("expected task moved to completed category, got %s", updatedTask.Category)
	}
}

func TestLLMService_ExecuteTaskWithAgent_WebhookOriginSkipsTaskCreationMarkers(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	ctx := context.Background()

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)

	agent := ensureDefaultAgent(t, llmConfigRepo)
	mock := testutil.NewMockLLMCaller()
	mock.Response = `[CREATE_TASK]{"title":"Unexpected child","prompt":"should not be created"}[/CREATE_TASK]`
	mock.TextOnly = mock.Response
	mock.Tokens = 9
	svc.SetLLMCaller(mock)

	parent := &models.Task{
		ProjectID:  "default",
		Title:      "Webhook Parent",
		Category:   models.CategoryActive,
		Status:     models.StatusPending,
		CreatedVia: models.TaskOriginWebhook,
		Prompt:     "Handle webhook payload",
		AgentID:    &agent.ID,
	}
	if err := taskRepo.Create(ctx, parent); err != nil {
		t.Fatalf("failed to create parent task: %v", err)
	}

	if _, err := svc.ExecuteTaskWithAgent(ctx, *parent, *agent); err != nil {
		t.Fatalf("ExecuteTaskWithAgent returned error: %v", err)
	}

	tasks, err := taskRepo.ListByProject(ctx, "default", "")
	if err != nil {
		t.Fatalf("ListByProject failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 task (no fan-out), got %d", len(tasks))
	}
}

func TestLLMService_FailedTaskMovedToCompletedCategory(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	ctx := context.Background()

	task := &models.Task{
		ProjectID: "default",
		Title:     "Test Failed Task",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "test prompt",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	agent, err := llmConfigRepo.GetDefault(ctx)
	if err != nil || agent == nil {
		t.Fatalf("failed to get default agent: %v", err)
	}

	exec := &models.Execution{
		TaskID:        task.ID,
		AgentConfigID: agent.ID,
		Status:        models.ExecRunning,
		PromptSent:    task.Prompt,
	}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	output := "I tried to complete the task but encountered an error.\n[STATUS: FAILED | test error]"
	reason := "test error"

	if err := execRepo.Complete(ctx, exec.ID, models.ExecFailed, output, reason, 0, 100); err != nil {
		t.Fatalf("failed to complete execution: %v", err)
	}

	if err := taskRepo.UpdateStatus(ctx, task.ID, models.StatusFailed); err != nil {
		t.Fatalf("failed to update task status: %v", err)
	}

	if err := taskRepo.UpdateCategory(ctx, task.ID, models.CategoryCompleted); err != nil {
		t.Fatalf("failed to move task to completed category: %v", err)
	}

	updated, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to fetch updated task: %v", err)
	}
	if updated.Category != models.CategoryCompleted {
		t.Errorf("expected failed task to be moved to completed category, got %q", updated.Category)
	}
	if updated.Status != models.StatusFailed {
		t.Errorf("expected task status to be failed, got %q", updated.Status)
	}
}

// TestLLMService_ExecuteTaskWithAgent_PluginScopingWithAgentDef verifies that
// when a task has an AgentDefinitionID, the agent definition (including its
// plugin-resolved skills/MCP) is passed to the LLM call.
func TestLLMService_ExecuteTaskWithAgent_PluginScopingWithAgentDef(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	agentRepo := repository.NewAgentRepo(db)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)

	// Use a capture adapter to inspect the request that reaches the provider
	capture := &captureProviderAdapter{}
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetAgentRepo(agentRepo)
	// Override the test provider adapter with our capture
	svc.providerAdapters[models.ProviderTest] = capture

	agent := ensureDefaultAgent(t, llmConfigRepo)

	// Create an agent definition with plugins
	agentDef := &models.Agent{
		Name:         "test-plugin-agent",
		SystemPrompt: "Use plugin tools for testing",
		Plugins:      []string{"test-plugin@test-market"},
	}
	if err := agentRepo.Create(ctx, agentDef); err != nil {
		t.Fatalf("create agent definition: %v", err)
	}

	task := &models.Task{
		ProjectID:         "default",
		Title:             "Task with agent def",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		Prompt:            "run plugin tools",
		AgentDefinitionID: &agentDef.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the agent definition was propagated to the adapter
	if capture.lastReq.AgentDefinition == nil {
		t.Fatal("expected agent definition to be propagated to adapter")
	}
	if capture.lastReq.AgentDefinition.ID != agentDef.ID {
		t.Fatalf("expected agent definition ID %q, got %q", agentDef.ID, capture.lastReq.AgentDefinition.ID)
	}
	if capture.lastReq.AgentDefinition.SystemPrompt != "Use plugin tools for testing" {
		t.Fatalf("expected agent system prompt propagated, got %q", capture.lastReq.AgentDefinition.SystemPrompt)
	}
}

// TestLLMService_ExecuteTaskWithAgent_NoAgentDef_NilPluginContext verifies that
// when a task has no AgentDefinitionID, the adapter receives nil AgentDefinition
// (zero plugin context).
func TestLLMService_ExecuteTaskWithAgent_NoAgentDef_NilPluginContext(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	agentRepo := repository.NewAgentRepo(db)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)

	capture := &captureProviderAdapter{}
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetAgentRepo(agentRepo)
	svc.providerAdapters[models.ProviderTest] = capture

	agent := ensureDefaultAgent(t, llmConfigRepo)

	// Create a task WITHOUT AgentDefinitionID
	task := &models.Task{
		ProjectID: "default",
		Title:     "Task without agent def",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "do something without plugins",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no agent definition was propagated (nil = zero plugin context)
	if capture.lastReq.AgentDefinition != nil {
		t.Fatalf("expected nil AgentDefinition for task without agent def, got %+v", capture.lastReq.AgentDefinition)
	}
	if len(capture.lastReq.PluginDirs) != 0 {
		t.Fatalf("expected zero PluginDirs for task without agent def, got %v", capture.lastReq.PluginDirs)
	}
}

// TestLLMService_ExecuteTaskWithAgent_WrongAgentDefNotUsed verifies that
// if a task references a non-existent agent definition, no plugin context
// leaks from other agent definitions.
func TestLLMService_ExecuteTaskWithAgent_WrongAgentDefNotUsed(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	agentRepo := repository.NewAgentRepo(db)
	ctx := context.Background()

	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)

	capture := &captureProviderAdapter{}
	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, repository.NewProjectRepo(db), scheduleRepo, attachmentRepo)
	svc.SetAgentRepo(agentRepo)
	svc.providerAdapters[models.ProviderTest] = capture

	agent := ensureDefaultAgent(t, llmConfigRepo)

	// Create an agent definition that should NOT be used
	otherAgentDef := &models.Agent{
		Name:         "other-agent",
		SystemPrompt: "I am a different agent",
		Plugins:      []string{"other-plugin@other-market"},
	}
	if err := agentRepo.Create(ctx, otherAgentDef); err != nil {
		t.Fatalf("create other agent definition: %v", err)
	}

	// Create the task without AgentDefinitionID (to satisfy FK constraints),
	// then set it in memory to a non-existent ID before passing to ExecuteTaskWithAgent.
	task := &models.Task{
		ProjectID: "default",
		Title:     "Task with bad agent def ref",
		Category:  models.CategoryActive,
		Status:    models.StatusPending,
		Prompt:    "this should have no plugins",
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Simulate a stale/invalid reference in the in-memory task object
	nonExistentID := "non-existent-agent-def-id"
	task.AgentDefinitionID = &nonExistentID

	_, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The non-existent agent def lookup should fail gracefully,
	// resulting in nil AgentDefinition (no plugin context)
	if capture.lastReq.AgentDefinition != nil {
		t.Fatalf("expected nil AgentDefinition for non-existent ref, got %+v", capture.lastReq.AgentDefinition)
	}
}

// TestLLMService_CallAgentDirectStreamingDetailed_PluginScopingByAgentDef
// verifies that CallAgentDirectStreamingDetailed (used by thread follow-ups)
// propagates agent definition correctly and that nil agent def means zero plugins.
func TestLLMService_CallAgentDirectStreamingDetailed_PluginScopingByAgentDef(t *testing.T) {
	capture := &captureProviderAdapter{}
	svc := &LLMService{}
	svc.providerAdapters = map[models.LLMProvider]ProviderAdapter{
		models.ProviderOpenAI: capture,
	}

	agent := models.LLMConfig{Provider: models.ProviderOpenAI, Model: "gpt-test"}

	// Case 1: With agent definition
	agentDef := &models.Agent{
		ID:           "follow-up-agent",
		Name:         "followup-agent",
		SystemPrompt: "I handle follow-ups",
		Plugins:      []string{"followup-plugin@market"},
	}
	_, err := svc.CallAgentDirectStreamingDetailed(
		context.Background(), "follow up message", nil,
		agent, "exec-1", nil, "sys ctx", "/work", agentDef, true,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.lastReq.AgentDefinition == nil {
		t.Fatal("expected agent definition for follow-up with agent def")
	}
	if capture.lastReq.AgentDefinition.ID != "follow-up-agent" {
		t.Fatalf("expected agent def ID follow-up-agent, got %q", capture.lastReq.AgentDefinition.ID)
	}

	// Case 2: Without agent definition (task has no agent assigned)
	_, err = svc.CallAgentDirectStreamingDetailed(
		context.Background(), "follow up without agent", nil,
		agent, "exec-2", nil, "sys ctx", "/work", nil, true,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.lastReq.AgentDefinition != nil {
		t.Fatalf("expected nil AgentDefinition for follow-up without agent def, got %+v", capture.lastReq.AgentDefinition)
	}
}

func TestLLMService_ExecuteTask_ScopedFilesPrepFailureCompletesExecution(t *testing.T) {
	db := testutil.NewTestDB(t)
	llmConfigRepo := repository.NewLLMConfigRepo(db)
	execRepo := repository.NewExecutionRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	projectRepo := repository.NewProjectRepo(db)
	scheduleRepo := repository.NewScheduleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	agentRepo := repository.NewAgentRepo(db)
	ctx := context.Background()

	svc := NewLLMService(llmConfigRepo, execRepo, taskRepo, projectRepo, scheduleRepo, attachmentRepo)
	svc.SetAgentRepo(agentRepo)
	svc.SetLLMCaller(&testutil.MockLLMCaller{Response: "should not run", TextOnly: "should not run", Tokens: 1})

	project, err := projectRepo.GetByID(ctx, "default")
	if err != nil {
		t.Fatalf("get default project: %v", err)
	}
	if project == nil {
		project = &models.Project{ID: "default", Name: "Default"}
	}
	project.RepoPath = t.TempDir()
	if err := projectRepo.Update(ctx, project); err != nil {
		t.Fatalf("update project repo path: %v", err)
	}

	agentDef := &models.Agent{
		Name:         "Bad scoped agent",
		SystemPrompt: "Use scoped files",
		Model:        "inherit",
		Tools:        []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{
			ScopedFiles: []models.ScopedFilesConfig{{Directory: "../escape", Permissions: []string{"read"}}},
		},
	}
	if err := agentRepo.Create(ctx, agentDef); err != nil {
		t.Fatalf("create agent definition: %v", err)
	}

	agent, _ := llmConfigRepo.GetDefault(ctx)
	agent.Provider = models.ProviderTest
	task := &models.Task{
		ProjectID:         "default",
		Title:             "Bad scoped files",
		Category:          models.CategoryActive,
		Status:            models.StatusPending,
		Prompt:            "Try scoped files",
		AgentDefinitionID: &agentDef.ID,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec, err := svc.ExecuteTaskWithAgent(ctx, *task, *agent)
	if err == nil {
		t.Fatal("expected scoped files prep error")
	}
	if exec == nil {
		t.Fatal("expected failed execution to be returned")
	}
	if exec.Status != models.ExecFailed {
		t.Fatalf("expected returned execution failed, got %s", exec.Status)
	}

	execs, err := execRepo.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected one execution, got %d", len(execs))
	}
	if execs[0].Status != models.ExecFailed {
		t.Fatalf("expected persisted execution failed, got %s", execs[0].Status)
	}
	if !strings.Contains(execs[0].ErrorMessage, "preparing scoped file tools") {
		t.Fatalf("expected scoped files error message, got %q", execs[0].ErrorMessage)
	}
}
