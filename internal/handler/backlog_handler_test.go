package handler

import (
	"net/http"
	"net/url"
	"testing"
)

// BacklogManagement always renders (no nil-service guard upfront).
func TestBacklogManagement_NoProjects(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/backlog").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestBacklogManagement_WithProject(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().WithName("Backlog Project").Build()
	rec := tc.HTTP().Get("/backlog").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK).Contains("Backlog Project")
}

func TestBacklogManagement_WithProjectID(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	rec := tc.HTTP().Get("/backlog?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestBacklogManagement_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().Build()
	rec := tc.HTMX().Get("/backlog").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

// backlogSvc is nil in NewTestContext → 400 for all action endpoints.

func TestRunBacklogAnalysis_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/backlog/analyze").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestRunBacklogAnalysis_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/backlog/analyze?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestUpdateBacklogSuggestionStatus_NilService(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{"status": []string{"approved"}}
	rec := tc.HTTP().Patch("/backlog/suggestions/s-1/status").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestApplyBacklogSuggestion_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/backlog/suggestions/s-1/apply").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestDeleteBacklogSuggestion_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Delete("/backlog/suggestions/s-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestSnapshotBacklogHealth_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/backlog/health").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestSnapshotBacklogHealth_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/backlog/health?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestListBacklogReports_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/backlog/reports").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestListBacklogReports_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/backlog/reports?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}
