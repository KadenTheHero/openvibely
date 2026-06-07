package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/models"
)

// ---- CreateSchedule ----

func TestCreateSchedule_InvalidDate(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()

	rec := tc.HTMX().Post("/tasks/"+task.ID+"/schedule").WithForm(url.Values{
		"run_at":          {"not-a-date"},
		"repeat_type":     {"once"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date, got %d", rec.Code)
	}
}

func TestCreateSchedule_Success_Redirect(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()

	runAt := time.Now().Add(time.Hour).Format("2006-01-02T15:04")
	rec := tc.HTTP().Post("/tasks/"+task.ID+"/schedule").WithForm(url.Values{
		"run_at":          {runAt},
		"repeat_type":     {"once"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rec.Code)
	}

	schedules, err := tc.scheduleRepo.ListByTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}
}

func TestCreateSchedule_HTMX_Success(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()

	runAt := time.Now().Add(time.Hour).Format("2006-01-02T15:04")
	rec := tc.HTMX().Post("/tasks/"+task.ID+"/schedule").WithForm(url.Values{
		"run_at":          {runAt},
		"repeat_type":     {"daily"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX create, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateSchedule_DefaultRepeatType(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()

	runAt := time.Now().Add(time.Hour).Format("2006-01-02T15:04")
	rec := tc.HTTP().Post("/tasks/"+task.ID+"/schedule").WithForm(url.Values{
		"run_at":          {runAt},
		"repeat_interval": {"1"},
		// omit repeat_type — should default to "once"
	}).Execute()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}

	schedules, err := tc.scheduleRepo.ListByTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}
	if schedules[0].RepeatType != models.RepeatOnce {
		t.Errorf("expected RepeatOnce default, got %q", schedules[0].RepeatType)
	}
}

// ---- UpdateSchedule ----

func TestUpdateSchedule_NotFound(t *testing.T) {
	tc := NewTestContext(t)

	runAt := time.Now().Add(time.Hour).Format("2006-01-02T15:04")
	rec := tc.HTTP().Put("/schedules/nonexistent-id").WithForm(url.Values{
		"run_at":          {runAt},
		"repeat_type":     {"once"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateSchedule_InvalidDate(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	rec := tc.HTTP().Put("/schedules/"+schedule.ID).WithForm(url.Values{
		"run_at":          {"bad-date"},
		"repeat_type":     {"once"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateSchedule_Success_Redirect(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	runAt := time.Now().Add(2 * time.Hour).Format("2006-01-02T15:04")
	rec := tc.HTTP().Put("/schedules/"+schedule.ID).WithForm(url.Values{
		"run_at":          {runAt},
		"repeat_type":     {"once"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
}

func TestUpdateSchedule_HTMX_Success(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	runAt := time.Now().Add(2 * time.Hour).Format("2006-01-02T15:04")
	rec := tc.HTMX().Put("/schedules/"+schedule.ID).WithForm(url.Values{
		"run_at":          {runAt},
		"repeat_type":     {"daily"},
		"repeat_interval": {"1"},
	}).Execute()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX update, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- DeleteSchedule ----

func TestDeleteSchedule_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	rec := tc.HTMX().Delete("/schedules/" + schedule.ID).Execute()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX delete, got %d", rec.Code)
	}
}

func TestDeleteSchedule_Redirect(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	rec := tc.HTTP().Delete("/schedules/" + schedule.ID).Execute()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 for non-HTMX delete, got %d", rec.Code)
	}
}

// ---- RescheduleTask ----

func TestRescheduleTask_InvalidDate(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	rec := tc.HTMX().Patch("/schedules/"+schedule.ID+"/reschedule").WithForm(url.Values{
		"new_date": {"not-a-date"},
		"hour":     {"10"},
	}).Execute()

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date, got %d", rec.Code)
	}
}

func TestRescheduleTask_InvalidHour(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).Build()

	rec := tc.HTMX().Patch("/schedules/"+schedule.ID+"/reschedule").WithForm(url.Values{
		"new_date": {time.Now().AddDate(0, 0, 1).Format("2006-01-02")},
		"hour":     {"99"},
	}).Execute()

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid hour, got %d", rec.Code)
	}
}

func TestRescheduleTask_NotFound(t *testing.T) {
	tc := NewTestContext(t)

	rec := tc.HTMX().Patch("/schedules/nonexistent/reschedule").WithForm(url.Values{
		"new_date": {time.Now().AddDate(0, 0, 1).Format("2006-01-02")},
		"hour":     {"10"},
	}).Execute()

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRescheduleTask_Success_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).WithRepeatType(models.RepeatDaily).Build()

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	rec := tc.HTMX().Patch("/schedules/"+schedule.ID+"/reschedule").WithForm(url.Values{
		"new_date": {tomorrow},
		"hour":     {"10"},
	}).Execute()

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRescheduleTask_Success_Redirect(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	schedule := tc.CreateSchedule(task.ID).WithRepeatType(models.RepeatDaily).Build()

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	rec := tc.HTTP().Patch("/schedules/"+schedule.ID+"/reschedule").WithForm(url.Values{
		"new_date": {tomorrow},
		"hour":     {"10"},
	}).Execute()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
}

// ---- GetExecution ----

func TestGetExecution_NotFound(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/executions/nonexistent").Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetExecution_Success(t *testing.T) {
	tc := NewTestContext(t)
	agent := tc.CreateLLMConfig().Build()
	project := tc.CreateProject().Build()
	task := tc.CreateTask(project.ID).Build()
	exec := tc.CreateExecution(task.ID, agent.ID).WithStatus(models.ExecCompleted).WithOutput("done").Build()

	rec := tc.HTTP().Get("/executions/" + exec.ID).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---- WorkerSettings ----

func TestWorkerSettings_NonHTMX(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/workers").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	tc.Assert(rec).Contains("max_workers")
}

func TestWorkerSettings_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTMX().Get("/workers").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- UpdateWorkerSettings ----

func TestUpdateWorkerSettings_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTMX().Post("/workers").WithForm(url.Values{
		"max_workers": {"3"},
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX update, got %d", rec.Code)
	}
}

func TestUpdateWorkerSettings_Redirect(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/workers").WithForm(url.Values{
		"max_workers": {"2"},
	}).Execute()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 for non-HTMX update, got %d", rec.Code)
	}
}

func TestUpdateWorkerSettings_Clamps(t *testing.T) {
	tc := NewTestContext(t)
	// Value of 99 should be clamped to 10
	rec := tc.HTTP().Post("/workers").WithForm(url.Values{
		"max_workers": {"99"},
	}).Execute()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
}

// ---- UpdateProjectWorkerLimit ----

func TestUpdateProjectWorkerLimit_NotFound(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTMX().Post("/workers/projects/nonexistent/limit").WithForm(url.Values{
		"max_workers": {"2"},
	}).Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateProjectWorkerLimit_Success(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()

	rec := tc.HTMX().Post("/workers/projects/"+project.ID+"/limit").WithForm(url.Values{
		"max_workers": {"3"},
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProjectWorkerLimit_Zero_RemovesLimit(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()

	rec := tc.HTMX().Post("/workers/projects/"+project.ID+"/limit").WithForm(url.Values{
		"max_workers": {"0"},
	}).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- GlobalWorkerStats / ProjectWorkerStats / ModelWorkerStats ----

func TestGlobalWorkerStats(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/workers/stats/global").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestProjectWorkerStats(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().Build()
	rec := tc.HTTP().Get("/workers/stats/projects").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestModelWorkerStats(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/workers/stats/models").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- Capacity API ----

func TestGetGlobalCapacity(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/capacity/global").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp GlobalCapacityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestGetProjectCapacities(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().WithName("Cap Test Project").Build()

	rec := tc.HTTP().Get("/api/capacity/projects").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp []ProjectCapacityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) == 0 {
		t.Errorf("expected at least 1 project in capacity response")
	}
}

func TestGetProjectCapacity_NotFound(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/capacity/projects/nonexistent").Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetProjectCapacity_Success(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().Build()

	rec := tc.HTTP().Get("/api/capacity/projects/" + project.ID).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp ProjectCapacityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != project.ID {
		t.Errorf("expected project ID %q, got %q", project.ID, resp.ID)
	}
}

func TestGetModelCapacities(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/capacity/models").Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestGetModelCapacity_NotFound(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/capacity/models/nonexistent").Execute()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
