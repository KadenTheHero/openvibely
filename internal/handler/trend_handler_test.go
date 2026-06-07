package handler

import (
	"net/http"
	"net/url"
	"testing"
)

// trendSvc is nil in NewTestContext.
// All endpoints check `projectID == "" || h.trendSvc == nil` → 400 "missing project_id".

func TestSaveXCredentials_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Put("/api/autonomous/x-credentials").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestSaveXCredentials_NilService(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{
		"api_key":    []string{"key"},
		"api_secret": []string{"secret"},
	}
	rec := tc.HTTP().Put("/api/autonomous/x-credentials?project_id=proj-1").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest) // nil trendSvc
}

func TestAddTrendSource_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{"source_type": []string{"hashtag"}, "value": []string{"#golang"}}
	rec := tc.HTTP().Post("/api/autonomous/trends/sources").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestAddTrendSource_NilService(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{"source_type": []string{"hashtag"}, "value": []string{"#golang"}}
	rec := tc.HTTP().Post("/api/autonomous/trends/sources?project_id=proj-1").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestDeleteTrendSource_MissingParams(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Delete("/api/autonomous/trends/sources/src-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestDeleteTrendSource_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Delete("/api/autonomous/trends/sources/src-1?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestToggleTrendSource_NilService(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{"enabled": []string{"true"}}
	rec := tc.HTTP().Patch("/api/autonomous/trends/sources/src-1/toggle?project_id=proj-1").WithForm(form).Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestCollectTrends_MissingProjectID(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/api/autonomous/trends/collect").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestCollectTrends_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/api/autonomous/trends/collect?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestAnalyzeTrends_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/api/autonomous/trends/analyze?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestAnalyzeCompetitors_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Post("/api/autonomous/trends/competitors?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestUpdateTrendPatternStatus_InvalidStatus(t *testing.T) {
	tc := NewTestContext(t)
	form := url.Values{"status": []string{"bad"}}
	rec := tc.HTTP().Patch("/api/autonomous/trends/patterns/p-1/status?project_id=proj-1").WithForm(form).Execute()
	// nil trendSvc → 400 before status check
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}

func TestGetTrendDashboard_NilService(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/autonomous/trends/dashboard?project_id=proj-1").Execute()
	tc.Assert(rec).StatusCode(http.StatusBadRequest)
}
