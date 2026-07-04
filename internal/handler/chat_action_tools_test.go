package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/chatcontrol"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/service"
)

func TestSupportsChatActionTools(t *testing.T) {
	tests := []struct {
		name  string
		agent models.LLMConfig
		want  bool
	}{
		{
			name: "openai oauth supports tools",
			agent: models.LLMConfig{
				Provider:   models.ProviderOpenAI,
				AuthMethod: models.AuthMethodOAuth,
			},
			want: true,
		},
		{
			name: "openai cli does not support runtime action tools",
			agent: models.LLMConfig{
				Provider:   models.ProviderOpenAI,
				AuthMethod: models.AuthMethodCLI,
			},
			want: false,
		},
		{
			name: "anthropic api key supports tools",
			agent: models.LLMConfig{
				Provider:   models.ProviderAnthropic,
				AuthMethod: models.AuthMethodAPIKey,
			},
			want: true,
		},
		{
			name: "ollama does not support runtime action tools",
			agent: models.LLMConfig{
				Provider: models.ProviderOllama,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportsChatActionTools(tt.agent); got != tt.want {
				t.Fatalf("supportsChatActionTools()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestFormatCapabilitiesIncludesSelectedMemoryHandles(t *testing.T) {
	out := formatCapabilities([]chatcontrol.ActionSummary{{Domain: "memory", Name: "memory_view", Description: "Load selected memory.", Access: "read"}}, []string{"usage_analytics.md"})
	if !strings.Contains(out, "Selected memories for this turn") || !strings.Contains(out, "usage_analytics.md") {
		t.Fatalf("expected selected memory handles in capabilities output, got:\n%s", out)
	}
}

func TestListCapabilitiesExecutorIncludesSelectedMemoryHandles(t *testing.T) {
	h, _, _ := setupTestHandler(t)
	rt := h.buildChatActionToolRuntimeFromDefs(
		streamingResponseParams{IsTaskFollowup: true},
		nil,
		chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, true),
		models.ChatModeOrchestrate,
		chatcontrol.SurfaceWeb,
	)
	ctx := service.WithSelectedMemoryHandles(context.Background(), []string{"chat_memory.md"})
	out, handled, isErr, err := rt.Executor(ctx, "list_capabilities", nil)
	if !handled || isErr || err != nil {
		t.Fatalf("list_capabilities failed handled=%v isErr=%v err=%v out=%q", handled, isErr, err, out)
	}
	for _, want := range []string{"Selected memories for this turn", "chat_memory.md", "memory_view"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected list_capabilities output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestBuildToolMarker_WithBody(t *testing.T) {
	input := json.RawMessage(`{"title":"Fix login","prompt":"Investigate auth flow"}`)
	got, err := buildToolMarker("CREATE_TASK", input, true)
	if err != nil {
		t.Fatalf("buildToolMarker error: %v", err)
	}
	if !strings.Contains(got, "[CREATE_TASK]") || !strings.Contains(got, "[/CREATE_TASK]") {
		t.Fatalf("expected create task marker wrapper, got %q", got)
	}
	if !strings.Contains(got, `"title":"Fix login"`) {
		t.Fatalf("expected normalized JSON body, got %q", got)
	}
}

func TestBuildToolMarker_ChainConfigPreserved(t *testing.T) {
	// Simulate the exact JSON a model sends when using create_task tool with chain config
	input := json.RawMessage(`{"title":"Compute 1+1","prompt":"Compute 1+1 and save to file","category":"active","chain":{"enabled":true,"trigger":"on_completion","child_title":"Compute x+1 from parent output","child_prompt_prefix":"Read x from result.txt and compute x+1","child_category":"active"}}`)

	marker, err := buildToolMarker("CREATE_TASK", input, true)
	if err != nil {
		t.Fatalf("buildToolMarker error: %v", err)
	}

	// Verify marker wrapping
	if !strings.Contains(marker, "[CREATE_TASK]") || !strings.Contains(marker, "[/CREATE_TASK]") {
		t.Fatalf("missing marker wrapper: %q", marker)
	}

	// Parse it back via the same path as processChatTaskCreations
	tasks := service.ParseTaskCreations(marker)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task from roundtrip, got %d", len(tasks))
	}

	req := tasks[0]
	if req.Chain == nil {
		t.Fatal("chain config lost in buildToolMarker → ParseTaskCreations roundtrip")
	}
	if !req.Chain.Enabled {
		t.Error("chain.enabled should be true after roundtrip")
	}
	if req.Chain.Trigger != "on_completion" {
		t.Errorf("chain.trigger = %q after roundtrip", req.Chain.Trigger)
	}
	if req.Chain.ChildTitle != "Compute x+1 from parent output" {
		t.Errorf("chain.child_title = %q after roundtrip", req.Chain.ChildTitle)
	}
	if req.Chain.ChildPromptPrefix != "Read x from result.txt and compute x+1" {
		t.Errorf("chain.child_prompt_prefix lost in roundtrip")
	}
	if req.Chain.ChildCategory != "active" {
		t.Errorf("chain.child_category = %q after roundtrip", req.Chain.ChildCategory)
	}
}

func TestToolSummaryFromMarker(t *testing.T) {
	marker := "[LIST_PROJECTS]"
	updated := marker + "\n\n---\nAvailable Projects:\n- **Default**"
	got := toolSummaryFromMarker(marker, updated)
	if !strings.Contains(got, "Available Projects") {
		t.Fatalf("expected tool summary to keep appended output, got %q", got)
	}
}

func TestChatActionSummaryCollector_AppendsCreatedAndEdited(t *testing.T) {
	collector := newChatActionSummaryCollector()
	collector.addCreated("\n---\nCreated 1 task(s):\n- \"Fix login\" (active) [TASK_ID:abc123]")
	collector.addEdited("\n---\nEdited 1 task(s):\n- \"Fix login\" (backlog, updated: category) [TASK_EDITED:abc123]")

	out := collector.appendToOutput("Done.")
	if !strings.Contains(out, "Created 1 task(s):") {
		t.Fatalf("expected created summary, got %q", out)
	}
	if !strings.Contains(out, "[TASK_ID:abc123]") {
		t.Fatalf("expected task id marker, got %q", out)
	}
	if !strings.Contains(out, "Edited 1 task(s):") {
		t.Fatalf("expected edited summary, got %q", out)
	}
	if !strings.Contains(out, "[TASK_EDITED:abc123]") {
		t.Fatalf("expected task edited marker, got %q", out)
	}
}

func TestChatActionHandlers_CoverageWebAndAPI(t *testing.T) {
	h := &Handler{}
	params := streamingResponseParams{ExecID: "e", ProjectID: "p"}

	webHandlers := h.chatActionHandlers(params, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
	if err := chatcontrol.ValidateHandlerCoverage(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, true, webHandlers); err != nil {
		t.Fatalf("web handler coverage mismatch: %v", err)
	}

	apiHandlers := h.chatActionHandlers(params, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceAPI)
	if err := chatcontrol.ValidateHandlerCoverage(models.ChatModeOrchestrate, chatcontrol.SurfaceAPI, true, apiHandlers); err != nil {
		t.Fatalf("api handler coverage mismatch: %v", err)
	}
}

func TestCreateSwarmTaskRuntimeTool_StartFlagDoesNotDeferActiveSwarm(t *testing.T) {
	h, _, _, _ := setupTestHandlerWithDB(t)
	project := createProject(t, h, "Deferred Swarm Tool Project")
	handlers := h.chatActionHandlers(streamingResponseParams{ExecID: "exec-swarm-deferred", ProjectID: project.ID}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
	createHandler := handlers["create_swarm_task"]
	if createHandler == nil {
		t.Fatal("create_swarm_task handler missing")
	}
	out, err := createHandler(context.Background(), json.RawMessage(`{"title":"Plan export","prompt":"Plan export with workers","max_workers":3,"worker_isolation":"worktree","start_immediately":false}`))
	if err != nil {
		t.Fatalf("create_swarm_task failed: %v", err)
	}
	if !strings.Contains(out, "Planner starts when the swarm parent is Active.") {
		t.Fatalf("expected category-driven planner summary, got %q", out)
	}
	ids := extractTaskIDsFromOutput(out)
	if len(ids) != 1 {
		t.Fatalf("expected one parent task id in output, got %q", out)
	}
	planner, err := h.taskRepo.FindSwarmChildByRole(context.Background(), ids[0], models.SwarmRolePlanner)
	if err != nil {
		t.Fatalf("FindSwarmChildByRole: %v", err)
	}
	if planner == nil {
		t.Fatal("expected planner child for active runtime tool swarm")
	}
	if planner.Category != models.CategoryActive || planner.Status != models.StatusPending {
		t.Fatalf("planner not runnable: category=%s status=%s", planner.Category, planner.Status)
	}
}

func TestCreateSwarmTaskRuntimeTool_BacklogCategoryDefersPlanner(t *testing.T) {
	h, _, _, _ := setupTestHandlerWithDB(t)
	project := createProject(t, h, "Backlog Swarm Tool Project")
	handlers := h.chatActionHandlers(streamingResponseParams{ExecID: "exec-swarm-backlog", ProjectID: project.ID}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
	createHandler := handlers["create_swarm_task"]
	if createHandler == nil {
		t.Fatal("create_swarm_task handler missing")
	}

	out, err := createHandler(context.Background(), json.RawMessage(`{"title":"Backlog plan","prompt":"Plan later","category":"backlog","max_workers":2}`))
	if err != nil {
		t.Fatalf("create_swarm_task failed: %v", err)
	}
	if !strings.Contains(out, `(backlog)`) {
		t.Fatalf("expected backlog summary, got %q", out)
	}
	ids := extractTaskIDsFromOutput(out)
	if len(ids) != 1 {
		t.Fatalf("expected one parent task id in output, got %q", out)
	}
	parent, err := h.taskRepo.GetByID(context.Background(), ids[0])
	if err != nil || parent == nil {
		t.Fatalf("parent not persisted: %v", err)
	}
	if parent.Category != models.CategoryBacklog {
		t.Fatalf("parent category=%s, want backlog", parent.Category)
	}
	planner, err := h.taskRepo.FindSwarmChildByRole(context.Background(), ids[0], models.SwarmRolePlanner)
	if err != nil {
		t.Fatalf("FindSwarmChildByRole: %v", err)
	}
	if planner != nil {
		t.Fatalf("expected backlog runtime swarm to defer planner, got %#v", planner)
	}
}

func TestCreateSwarmTaskRuntimeTool_ChannelSurfaceUsesActiveProject(t *testing.T) {
	h, _, _, _ := setupTestHandlerWithDB(t)
	authorizedProject := createProject(t, h, "Email Authorized Swarm Project")
	foreignProject := createProject(t, h, "Email Foreign Swarm Project")

	handlers := h.chatActionHandlers(streamingResponseParams{ExecID: "exec-email-swarm", ProjectID: authorizedProject.ID, Surface: chatcontrol.SurfaceEmail}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceEmail)
	createHandler := handlers["create_swarm_task"]
	if createHandler == nil {
		t.Fatal("create_swarm_task handler missing")
	}

	payload := `{"title":"Email swarm","prompt":"Split this email work","project_id":"` + foreignProject.ID + `","start_immediately":false}`
	out, err := createHandler(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("create_swarm_task failed: %v", err)
	}
	ids := extractTaskIDsFromOutput(out)
	if len(ids) != 1 {
		t.Fatalf("expected one parent task id in output, got %q", out)
	}
	parent, err := h.taskRepo.GetByID(context.Background(), ids[0])
	if err != nil || parent == nil {
		t.Fatalf("parent not persisted: %v", err)
	}
	if parent.ProjectID != authorizedProject.ID {
		t.Fatalf("channel swarm should use active project %s, got %s", authorizedProject.ID, parent.ProjectID)
	}
	foreignTasks, err := h.taskRepo.ListByProject(context.Background(), foreignProject.ID, "")
	if err != nil {
		t.Fatalf("list foreign tasks: %v", err)
	}
	if len(foreignTasks) != 0 {
		t.Fatalf("expected no tasks in foreign project, got %d", len(foreignTasks))
	}
}

func TestCreateSwarmTaskRuntimeTool_CreatesParentAndPlanner(t *testing.T) {
	h, _, _, _ := setupTestHandlerWithDB(t)
	project := createProject(t, h, "Swarm Tool Project")
	handlers := h.chatActionHandlers(streamingResponseParams{ExecID: "exec-swarm", ProjectID: project.ID}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
	createHandler := handlers["create_swarm_task"]
	if createHandler == nil {
		t.Fatal("create_swarm_task handler missing")
	}
	out, err := createHandler(context.Background(), json.RawMessage(`{"title":"Build export","prompt":"Build export with workers","max_workers":3,"worker_isolation":"worktree","start_immediately":true}`))
	if err != nil {
		t.Fatalf("create_swarm_task failed: %v", err)
	}
	ids := extractTaskIDsFromOutput(out)
	if len(ids) != 1 {
		t.Fatalf("expected one parent task id in output, got %q", out)
	}
	parent, err := h.taskRepo.GetByID(context.Background(), ids[0])
	if err != nil || parent == nil {
		t.Fatalf("parent not persisted: %v", err)
	}
	if parent.SwarmRole != models.SwarmRoleParent {
		t.Fatalf("expected swarm parent role, got %q", parent.SwarmRole)
	}
	planner, err := h.taskRepo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner child not created: %v", err)
	}
}

// TestCreateTaskRuntimeTool_FailsLoudlyOnPersistenceFailure is the regression
// test for the phantom create_task bug: the runtime tool handler used to
// always return (summary, nil), so even if processChatTaskCreations failed to
// persist any task (empty project context, malformed input, or DB error) the
// model would receive isError=false and report a fake successful create_task
// to the user. The fix returns an error when no [TASK_ID:...] markers appear
// in the summary or when a referenced task ID cannot be verified in the
// current project's task store.
func TestCreateTaskRuntimeTool_FailsLoudlyOnPersistenceFailure(t *testing.T) {
	h, _, _, db := setupTestHandlerWithDB(t)
	ctx := context.Background()
	project := createProject(t, h, "Create Task Failure Project")
	input := json.RawMessage(`{"title":"Fix bug","prompt":"do it"}`)

	t.Run("empty project id", func(t *testing.T) {
		handlers := h.chatActionHandlers(streamingResponseParams{ExecID: "exec-empty-project", ProjectID: ""}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
		createHandler := handlers["create_task"]
		if createHandler == nil {
			t.Fatal("create_task handler missing")
		}
		if _, err := createHandler(ctx, input); err == nil {
			t.Fatal("expected create_task with empty project_id to return an error")
		}
	})

	t.Run("summary without persisted task id", func(t *testing.T) {
		handlers := h.chatActionHandlers(streamingResponseParams{ExecID: "exec-db-failure", ProjectID: project.ID}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
		createHandler := handlers["create_task"]
		if createHandler == nil {
			t.Fatal("create_task handler missing")
		}

		if _, err := h.taskRepo.GetByID(ctx, "sanity-check"); err != nil {
			t.Fatalf("task repo should work before closing db: %v", err)
		}
		h.execRepo = nil // Avoid best-effort execution-output updates after the DB is closed.
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		output, err := createHandler(ctx, input)
		if err == nil {
			t.Fatalf("expected create_task to fail when persistence fails, got nil error and output %q", output)
		}
		if !strings.Contains(output, "Failed to create") {
			t.Fatalf("expected failure summary in tool output, got %q", output)
		}
	})
}

// TestWebAPISwitchProject_IsInformationalOnly is the non-regression guard ensuring that
// the web/API switch_project tool never writes to any channel-specific persistence
// table (discord_user_projects, slack_user_projects, telegram_user_projects,
// email_sender_projects). The web/API path is informational: the frontend manages
// the active project_id, so the handler must not touch channel tables.
func TestWebAPISwitchProject_IsInformationalOnly(t *testing.T) {
	h, _, _, db := setupTestHandlerWithDB(t)
	ctx := context.Background()

	project1 := createProject(t, h, "Alpha")
	project2 := createProject(t, h, "Beta")
	_ = project1

	handlers := h.chatActionHandlers(
		streamingResponseParams{ExecID: "e1", ProjectID: project2.ID},
		nil,
		models.ChatModeOrchestrate,
		chatcontrol.SurfaceWeb,
	)

	switchHandler, ok := handlers["switch_project"]
	if !ok {
		t.Fatal("switch_project handler missing from web surface handlers")
	}

	result, err := switchHandler(ctx, json.RawMessage(`{"project":"Alpha"}`))
	if err != nil {
		t.Fatalf("switch_project returned unexpected error: %v", err)
	}
	if !strings.Contains(result, "Alpha") {
		t.Fatalf("expected informational response mentioning Alpha, got: %q", result)
	}

	// Assert no channel-specific persistence rows were written.
	channelTables := []string{
		"discord_user_projects",
		"slack_user_projects",
		"telegram_user_projects",
		"email_sender_projects",
	}
	for _, table := range channelTables {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count(%s): %v", table, err)
		}
		if count != 0 {
			t.Errorf("web/API switch_project must not write to %s: found %d row(s)", table, count)
		}
	}
}
