package handler

import (
	"net/http"
	"net/url"
	"testing"
)

// AutonomousBuilds always renders the page (no nil-service guard at the top level).
func TestAutonomousBuilds_NoProjects(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/autonomous").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestAutonomousBuilds_WithProject(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().WithName("Auto Project").Build()
	rec := tc.HTTP().Get("/autonomous").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK).Contains("Auto Project")
}

func TestAutonomousBuilds_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().Build()
	rec := tc.HTMX().Get("/autonomous").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

// autonomousTriggerSvc is nil in NewTestContext → 400 for all action endpoints.

func TestTriggerAutonomousBuild_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/api/autonomous/trigger").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestTriggerAutonomousBuild_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/api/autonomous/trigger?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestUpdateAutonomousConfig_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{"enabled": []string{"true"}}
	rec := tc.HTTP().Put("/api/autonomous/config").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestUpdateAutonomousConfig_NilService(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{
		"enabled":             []string{"true"},
		"max_execution_hours": []string{"4"},
		"schedule_hour":       []string{"23"},
	}
	rec := tc.HTTP().Put("/api/autonomous/config?project_id=proj-1").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestGetBuildSummary_MissingTaskID(t *testing.T) {
	tc := NewTestContext(t)
	// Route is /api/autonomous/summary/:taskId — no path param means 404 from router.
	// Test with a task ID; service nil → 400.
	rec := tc.HTTP().Get("/api/autonomous/summary/task-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestGetBuildChain_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/autonomous/chain/task-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}
