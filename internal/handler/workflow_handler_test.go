package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
)

// setupWorkflowHandler wires a real WorkflowService backed by the test DB into the
// TestContext. All workflow handler tests use this instead of bare NewTestContext
// so that h.workflowSvc is non-nil.
func setupWorkflowHandler(t *testing.T) *TestContext {
	t.Helper()
	tc := NewTestContext(t)
	workflowRepo := repository.NewWorkflowRepo(tc.db)
	workflowSvc := service.NewWorkflowService(workflowRepo, tc.llmConfigRepo, tc.taskRepo, tc.handler.llmSvc)
	tc.handler.workflowSvc = workflowSvc
	return tc
}

// ---- ListWorkflows ----

func TestListWorkflows(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	rec := tc.HTTP().Get("/workflows?project_id=" + project.ID).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var data map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := data["Workflows"]; !ok {
		t.Error("expected Workflows key in response")
	}
}

func TestListWorkflows_DefaultProjectID(t *testing.T) {
	tc := setupWorkflowHandler(t)
	// No project_id → falls back to "default"
	rec := tc.HTTP().Get("/workflows").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- CreateWorkflow ----

func TestCreateWorkflow_MissingName(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	rec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		// name omitted
	}).Execute()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestCreateWorkflow_EmptyWorkflow(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	rec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"My Workflow"},
		"strategy":   {"sequential"},
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var wf models.Workflow
	if err := json.NewDecoder(rec.Body).Decode(&wf); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	if wf.Name != "My Workflow" {
		t.Errorf("expected workflow name 'My Workflow', got %q", wf.Name)
	}
}

func TestCreateWorkflow_DefaultStrategy(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	rec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Default Strategy Workflow"},
		// strategy omitted → should default to sequential
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var wf models.Workflow
	if err := json.NewDecoder(rec.Body).Decode(&wf); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wf.Strategy != models.StrategySequential {
		t.Errorf("expected sequential strategy, got %q", wf.Strategy)
	}
}

// ---- GetWorkflow ----

func TestGetWorkflow_NotFound(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/workflows/nonexistent").Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetWorkflow_Success(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	// Create workflow via HTTP
	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Test Workflow"},
	}).Execute()
	if createRec.Code != http.StatusOK {
		t.Fatalf("create workflow failed: %d", createRec.Code)
	}
	var created models.Workflow
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec := tc.HTTP().Get("/workflows/" + created.ID).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var data map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := data["Workflow"]; !ok {
		t.Error("expected Workflow key in response")
	}
}

// ---- UpdateWorkflow ----

func TestUpdateWorkflow_NotFound(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Put("/workflows/nonexistent").WithForm(url.Values{
		"name": {"Updated"},
	}).Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateWorkflow_Success(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Original"},
	}).Execute()
	var created models.Workflow
	json.NewDecoder(createRec.Body).Decode(&created)

	rec := tc.HTTP().Put("/workflows/"+created.ID).WithForm(url.Values{
		"name":        {"Updated Name"},
		"description": {"New description"},
		"strategy":    {"parallel"},
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- DeleteWorkflow ----

func TestDeleteWorkflow_NonExistent_OK(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Delete("/workflows/nonexistent").Execute()
	// SQLite DELETE on a missing row returns no error; handler returns 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for delete of nonexistent workflow (no-op), got %d", rec.Code)
	}
}

func TestDeleteWorkflow_Success(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"To Delete"},
	}).Execute()
	var created models.Workflow
	json.NewDecoder(createRec.Body).Decode(&created)

	rec := tc.HTTP().Delete("/workflows/" + created.ID).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- AddWorkflowStep ----

func TestAddWorkflowStep_MissingName(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Workflow"},
	}).Execute()
	var wf models.Workflow
	json.NewDecoder(createRec.Body).Decode(&wf)

	rec := tc.HTTP().Post("/workflows/"+wf.ID+"/steps").WithForm(url.Values{
		// name omitted
		"step_type": {"execute"},
		"prompt":    {"do something"},
	}).Execute()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing step name, got %d", rec.Code)
	}
}

func TestAddWorkflowStep_Success(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Workflow With Steps"},
	}).Execute()
	var wf models.Workflow
	json.NewDecoder(createRec.Body).Decode(&wf)

	rec := tc.HTTP().Post("/workflows/"+wf.ID+"/steps").WithForm(url.Values{
		"name":      {"Step One"},
		"step_type": {"execute"},
		"prompt":    {"do something useful"},
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var step models.WorkflowStep
	if err := json.NewDecoder(rec.Body).Decode(&step); err != nil {
		t.Fatalf("decode step: %v", err)
	}
	if step.Name != "Step One" {
		t.Errorf("expected step name 'Step One', got %q", step.Name)
	}
	if step.StepType != models.StepTypeExecute {
		t.Errorf("expected step_type execute, got %q", step.StepType)
	}
}

func TestAddWorkflowStep_DefaultStepType(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Workflow"},
	}).Execute()
	var wf models.Workflow
	json.NewDecoder(createRec.Body).Decode(&wf)

	rec := tc.HTTP().Post("/workflows/"+wf.ID+"/steps").WithForm(url.Values{
		"name": {"Unnamed Step"},
		// step_type omitted → defaults to "execute"
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- DeleteWorkflowStep ----

func TestDeleteWorkflowStep_Success(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Workflow"},
	}).Execute()
	var wf models.Workflow
	json.NewDecoder(createRec.Body).Decode(&wf)

	addRec := tc.HTTP().Post("/workflows/"+wf.ID+"/steps").WithForm(url.Values{
		"name":      {"Step to Delete"},
		"step_type": {"execute"},
	}).Execute()
	var step models.WorkflowStep
	json.NewDecoder(addRec.Body).Decode(&step)

	rec := tc.HTTP().Delete("/workflows/" + wf.ID + "/steps/" + step.ID).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- ExecuteWorkflow ----

func TestExecuteWorkflow_MissingTaskID(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()

	createRec := tc.HTTP().Post("/workflows").WithForm(url.Values{
		"project_id": {project.ID},
		"name":       {"Workflow"},
	}).Execute()
	var wf models.Workflow
	json.NewDecoder(createRec.Body).Decode(&wf)

	rec := tc.HTTP().Post("/workflows/"+wf.ID+"/execute").WithForm(url.Values{
		// task_id omitted
	}).Execute()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing task_id, got %d", rec.Code)
	}
}

// ---- GetWorkflowExecution ----

func TestGetWorkflowExecution_NotFound(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/workflows/executions/nonexistent").Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ---- CancelWorkflowExecution ----

func TestCancelWorkflowExecution_NoRunningEngine(t *testing.T) {
	tc := setupWorkflowHandler(t)
	// Cancel a nonexistent execution — service will try to update DB, which is fine
	rec := tc.HTTP().Post("/workflows/executions/nonexistent/cancel").WithForm(url.Values{}).Execute()
	// Handler returns 200 with {"status":"cancelled"} regardless
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- AnalyzeTaskComplexity ----

func TestAnalyzeTaskComplexity_NotFound(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/tasks/nonexistent-task-id/analyze").Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing task, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAnalyzeTaskComplexity_Success(t *testing.T) {
	tc := setupWorkflowHandler(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).WithPrompt("Implement a new feature with multiple files across frontend and backend").Build()

	rec := tc.HTTP().Get("/tasks/" + task.ID + "/analyze").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- ListWorkflowTemplates ----

func TestListWorkflowTemplates(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/workflows/templates").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- Agent Metrics ----

func TestGetAllAgentMetrics(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/api/workflows/metrics").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAgentMetrics_NoData(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/api/workflows/metrics/nonexistent-agent").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty list), got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetBestAgent_NoData(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/api/workflows/best-agent").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !contains(body, "No performance data available") {
		t.Errorf("expected no-data message, got: %s", body)
	}
}

func TestGetBestAgent_WithTaskType(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/api/workflows/best-agent?task_type=frontend").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestGetCheapestAgent_NoData(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/api/workflows/cheapest-agent").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !contains(body, "No agents meet the quality threshold") {
		t.Errorf("expected no-data message, got: %s", body)
	}
}

// ---- GetVoteRecords ----

func TestGetVoteRecords_Empty(t *testing.T) {
	tc := setupWorkflowHandler(t)
	rec := tc.HTTP().Get("/api/workflows/votes/nonexistent-step-exec").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}
