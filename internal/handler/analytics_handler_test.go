package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- Analytics page ---

func TestAnalytics_NoProjects(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/analytics").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestAnalytics_WithProject(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().WithName("My Project").Build()
	rec := tc.HTTP().Get("/analytics").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK).Contains("My Project")
}

func TestAnalytics_WithProjectID(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().WithName("Selected").Build()
	rec := tc.HTTP().Get("/analytics?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK).Contains("Selected")
}

func TestAnalytics_HTMX(t *testing.T) {
	tc := NewTestContext(t)
	tc.CreateProject().WithName("HTMX Project").Build()
	rec := tc.HTMX().Get("/analytics").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

// --- API endpoints backed by execRepo (wired in NewTestContext) ---

func TestGetAnalyticsUsage_Default(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/usage").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAnalyticsUsage_Range7d(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/usage?range=7d").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAnalyticsUsage_RangeAll(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/usage?range=all").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAnalyticsUsage_RangeMonth(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/usage?range=month&group_by=week").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAnalyticsUsage_WithProjectFilter(t *testing.T) {
	tc := NewTestContext(t)
	p := tc.CreateProject().Build()
	rec := tc.HTTP().Get("/api/analytics/usage?project_id=" + p.ID).Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAnalyticsUsage_WithDateRange(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/usage?date_from=2024-01-01T00:00:00Z&date_to=2024-12-31T23:59:59Z").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetSuccessFailureRates(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/success-failure-rates").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetSuccessFailureRates_GroupByWeek(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/success-failure-rates?group_by=week").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAvgExecutionTimeByTask(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/avg-execution-time-by-task").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAvgExecutionTimeByTask_WithLimit(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/avg-execution-time-by-task?limit=5").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAvgExecutionTimeByAgent(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/avg-execution-time-by-agent").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetExecutionTrendsByHour(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/execution-trends-by-hour").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetAgentUsageByProject(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/agent-usage-by-project").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetMostFrequentTasks(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/most-frequent-tasks").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetMostFrequentTasks_WithLimit(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/most-frequent-tasks?limit=3").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

func TestGetFailedTaskPatterns(t *testing.T) {
	tc := NewTestContext(t)
	rec := tc.HTTP().Get("/api/analytics/failed-task-patterns").Execute()
	tc.Assert(rec).StatusCode(http.StatusOK)
}

// --- Pure function unit tests for parseAnalyticsTime ---

func TestParseAnalyticsTime_Empty(t *testing.T) {
	if got := parseAnalyticsTime(""); !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}

func TestParseAnalyticsTime_RFC3339(t *testing.T) {
	got := parseAnalyticsTime("2024-01-15T10:00:00Z")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if got.Year() != 2024 || got.Month() != 1 || got.Day() != 15 {
		t.Errorf("unexpected date: %v", got)
	}
}

func TestParseAnalyticsTime_DateOnly(t *testing.T) {
	got := parseAnalyticsTime("2024-06-01")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if got.Year() != 2024 || got.Month() != 6 {
		t.Errorf("unexpected date: %v", got)
	}
}

func TestParseAnalyticsTime_Invalid(t *testing.T) {
	if got := parseAnalyticsTime("not-a-date"); !got.IsZero() {
		t.Errorf("expected zero time for invalid input, got %v", got)
	}
}

// --- Pure function unit tests for parseUsageFilter ---

func echoContext(rawURL string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec)
}

func TestParseUsageFilter_Defaults(t *testing.T) {
	filter := parseUsageFilter(echoContext("/api/analytics/usage"))
	if filter.GroupBy != "day" {
		t.Errorf("expected default group_by=day, got %q", filter.GroupBy)
	}
	if filter.DateFrom.IsZero() {
		t.Error("expected DateFrom to be set for default 30d range")
	}
}


func TestParseUsageFilter_ExplicitDates(t *testing.T) {
	filter := parseUsageFilter(echoContext("/api/analytics/usage?date_from=2024-01-01&date_to=2024-01-31"))
	if filter.DateFrom.IsZero() {
		t.Error("expected DateFrom to be set")
	}
	if filter.DateTo.IsZero() {
		t.Error("expected DateTo to be set")
	}
}

func TestParseUsageFilter_Refresh(t *testing.T) {
	filter := parseUsageFilter(echoContext("/api/analytics/usage?refresh=true"))
	if !filter.Refresh {
		t.Error("expected Refresh=true")
	}
}

func TestParseUsageFilter_RefreshNumeric(t *testing.T) {
	filter := parseUsageFilter(echoContext("/api/analytics/usage?refresh=1"))
	if !filter.Refresh {
		t.Error("expected Refresh=true for refresh=1")
	}
}

func TestParseUsageFilter_GroupByHour(t *testing.T) {
	filter := parseUsageFilter(echoContext("/api/analytics/usage?group_by=hour&range=7d"))
	if filter.GroupBy != "hour" {
		t.Errorf("expected group_by=hour, got %q", filter.GroupBy)
	}
	diff := filter.DateTo.Sub(filter.DateFrom)
	if diff < 6*24*time.Hour || diff > 8*24*time.Hour {
		t.Errorf("expected ~7d date range, got %v", diff)
	}
}

func TestParseUsageFilter_MonthRange(t *testing.T) {
	filter := parseUsageFilter(echoContext("/api/analytics/usage?range=month"))
	if filter.DateFrom.Day() != 1 {
		t.Errorf("expected DateFrom day=1 for month range, got day=%d", filter.DateFrom.Day())
	}
}
