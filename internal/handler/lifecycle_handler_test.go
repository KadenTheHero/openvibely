package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/viewmodels"
)

func TestHandler_GetAgentLifecycleHooks_ReturnsAgentScopedHooks(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	h.SetAgentRepo(agentRepo)
	h.SetLifecycleRepo(lifecycleRepo)

	agent := &models.Agent{Name: "test-agent", SystemPrompt: "you do work"}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	hook := &models.AgentLifecycleHook{
		AgentID:        agent.ID,
		When:           models.LifecycleBeforeRun,
		SkillKey:       "recall",
		Blocking:       true,
		Enabled:        true,
		OutputContract: models.OutputContractContextBlock,
	}
	if err := lifecycleRepo.CreateHook(t.Context(), hook); err != nil {
		t.Fatalf("create hook: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/agents/"+agent.ID+"/lifecycle-hooks", nil)
	req.Header.Set(echo.HeaderAccept, "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []models.AgentLifecycleHook
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].SkillKey != "recall" {
		t.Fatalf("expected one recall hook, got %+v", got)
	}
}

func TestHandler_SaveAgentLifecycleHooks_ReconcilesSet(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	h.SetAgentRepo(agentRepo)
	h.SetLifecycleRepo(lifecycleRepo)

	agent := &models.Agent{Name: "save-hooks", SystemPrompt: "x"}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// Seed: one existing hook that the save payload omits — should be deleted.
	existing := &models.AgentLifecycleHook{
		AgentID:  agent.ID,
		When:     models.LifecycleAfterComplete,
		SkillKey: "old-skill",
		Enabled:  true,
	}
	if err := lifecycleRepo.CreateHook(t.Context(), existing); err != nil {
		t.Fatalf("seed: %v", err)
	}

	payload := []hookSavePayload{
		{When: "before_run", SkillKey: "load_project_context", Blocking: true, Enabled: true, OutputContract: "context_block"},
		{When: "after_complete", SkillKey: "summarize_activity", Blocking: false, Enabled: true, OutputContract: "activity_summary"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/agents/"+agent.ID+"/lifecycle-hooks", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	hooks, err := lifecycleRepo.HooksByAgent(t.Context(), agent.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks after save, got %d", len(hooks))
	}
	have := map[string]bool{}
	for _, h := range hooks {
		have[string(h.When)+"/"+h.SkillKey] = true
	}
	if !have["before_run/load_project_context"] || !have["after_complete/summarize_activity"] {
		t.Fatalf("unexpected hooks after save: %+v", hooks)
	}
	if have["after_complete/old-skill"] {
		t.Fatalf("stale hook should have been deleted")
	}
}

func TestHandler_SaveAgentLifecycleHooks_RejectsInvalidWhenAndTaskMode(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	h.SetAgentRepo(agentRepo)
	h.SetLifecycleRepo(lifecycleRepo)

	agent := &models.Agent{Name: "bad-when", SystemPrompt: "x"}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	body, _ := json.Marshal([]hookSavePayload{{When: "task_mode", SkillKey: "x", Enabled: true}})
	req := httptest.NewRequest(http.MethodPut, "/agents/"+agent.ID+"/lifecycle-hooks", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad when, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_SaveAgentLifecycleHooks_RejectsProtectedAgent(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	h.SetAgentRepo(agentRepo)
	h.SetLifecycleRepo(lifecycleRepo)

	// Use the seeded built-in agent from migration 078.
	all, err := agentRepo.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var protectedID string
	for _, a := range all {
		if a.GeneratedStatus == models.AgentStatusProtected {
			protectedID = a.ID
			break
		}
	}
	if protectedID == "" {
		t.Fatalf("expected at least one protected built-in agent from seed")
	}

	body, _ := json.Marshal([]hookSavePayload{{When: "before_run", SkillKey: "x", Enabled: true}})
	req := httptest.NewRequest(http.MethodPut, "/agents/"+protectedID+"/lifecycle-hooks", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_GetLifecycleExecutionEvents_ReturnsTraceEvents(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	h.SetLifecycleRepo(lifecycleRepo)

	task := &models.Task{ProjectID: "default", Title: "Trace Events", Category: models.CategoryActive, Status: models.StatusPending, Prompt: "p"}
	if err := taskRepo.Create(t.Context(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	agent := &models.Agent{Name: "trace-agent", SystemPrompt: "x"}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	exec := &models.LifecycleExecution{TaskID: task.ID, AgentID: agent.ID, When: models.LifecycleAfterComplete, Status: models.LifecycleExecCompleted}
	if err := lifecycleRepo.CreateExecution(t.Context(), exec); err != nil {
		t.Fatalf("create exec: %v", err)
	}
	if err := lifecycleRepo.AppendExecutionEvent(t.Context(), &models.LifecycleExecutionEvent{LifecycleExecutionID: exec.ID, EventType: "tool_call", PayloadJSON: `{"name":"skills_list"}`}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/lifecycle-executions/"+exec.ID+"/events", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []viewmodels.LifecycleExecutionEventView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].EventType != "tool_call" || got[0].Payload["name"] != "skills_list" {
		t.Fatalf("unexpected trace events: %+v", got)
	}
}

func TestHandler_GetTaskLifecycleExecutions_ReturnsPromptSafeView(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	h.SetAgentRepo(agentRepo)
	h.SetLifecycleRepo(lifecycleRepo)

	// Create a task and agent to back the FKs.
	project := createProject(t, h, "lifecycle-activity")
	task := &models.Task{ProjectID: project.ID, Title: "demo", Status: models.StatusPending, Category: "active"}
	if err := h.taskRepo.Create(t.Context(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	agent := &models.Agent{Name: "activity-stub", SystemPrompt: "x"}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	exec := &models.LifecycleExecution{
		TaskID:         task.ID,
		AgentID:        agent.ID,
		When:           models.LifecycleAfterComplete,
		SkillKey:       "summarize_activity",
		OutputContract: models.OutputContractActivitySummary,
		Status:         models.LifecycleExecCompleted,
		OutputJSON:     `{"summary":"Recorded user preference","skipped":false}`,
	}
	if err := lifecycleRepo.CreateExecution(t.Context(), exec); err != nil {
		t.Fatalf("create exec: %v", err)
	}
	routeExec := &models.LifecycleExecution{
		TaskID:         task.ID,
		AgentID:        agent.ID,
		When:           models.LifecycleRouteTask,
		SkillKey:       "route_task",
		OutputContract: models.OutputContractSelectedSkills,
		Status:         models.LifecycleExecCompleted,
		OutputJSON:     `{"skills":["openvibely_agent_skill_architecture","debug_go_tests"],"confidence":0.9,"reason":"matches prompt"}`,
	}
	if err := lifecycleRepo.CreateExecution(t.Context(), routeExec); err != nil {
		t.Fatalf("create route exec: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/lifecycle-executions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []viewmodels.LifecycleExecutionView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 views, got %d", len(got))
	}
	summaries := map[string]string{}
	for _, row := range got {
		summaries[row.When+"/"+row.SkillKey] = row.Summary
	}
	if summaries["after_complete/summarize_activity"] != "Recorded user preference" {
		t.Fatalf("expected human summary extracted from activity_summary contract, got %+v", summaries)
	}
	if summaries["route_task/route_task"] != "Selected skills: openvibely_agent_skill_architecture, debug_go_tests" {
		t.Fatalf("expected selected skill summary, got %+v", summaries)
	}
	// Prompt-safe view must not leak the raw OutputJSON or any internal fields
	// that the dialog should never display.
	if bytes.Contains(rec.Body.Bytes(), []byte("skipped")) {
		t.Fatalf("raw output JSON must not leak through the prompt-safe view")
	}
}
