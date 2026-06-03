package repository

import (
	"context"
	"testing"
	"time"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
)

func TestUsageRepo_RecordUsageEventAndAggregate(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()

	projectID := "proj-usage"
	taskID := "task-usage"
	execID := "exec-usage"
	agentID := "agent-usage"
	_, err := db.ExecContext(ctx, `INSERT INTO projects (id, name) VALUES (?, ?)`, projectID, "Usage Project")
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO agent_configs (id, name, provider, model, auth_method) VALUES (?, ?, ?, ?, ?)`, agentID, "OpenAI", "openai", "gpt-5.3-codex", "oauth")
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO tasks (id, project_id, title, category, status) VALUES (?, ?, ?, ?, ?)`, taskID, projectID, "Usage Task", "active", "running")
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO executions (id, task_id, agent_config_id, status, started_at) VALUES (?, ?, ?, ?, datetime('now'))`, execID, taskID, agentID, "completed")
	if err != nil {
		t.Fatalf("insert execution: %v", err)
	}

	cost := 1.25
	latency := int64(321)
	event := &models.LLMUsageEvent{
		Provider:                 "openai",
		AccountID:                "acct-1",
		ProjectID:                projectID,
		TaskID:                   taskID,
		ExecutionID:              execID,
		TurnID:                   execID,
		AgentConfigID:            agentID,
		Model:                    "gpt-5.3-codex",
		Operation:                "streaming",
		Status:                   "completed",
		InputTokens:              100,
		OutputTokens:             40,
		CachedInputTokens:        20,
		CacheCreationInputTokens: 5,
		CacheReadInputTokens:     15,
		ReasoningOutputTokens:    7,
		CostUSD:                  &cost,
		LatencyMs:                &latency,
		RawUsageJSON:             `{"input_tokens":100}`,
		OccurredAt:               time.Now().UTC(),
	}
	if err := repo.RecordUsageEvent(ctx, event); err != nil {
		t.Fatalf("RecordUsageEvent: %v", err)
	}
	if err := repo.RecordUsageEvent(ctx, event); err != nil {
		t.Fatalf("RecordUsageEvent duplicate: %v", err)
	}

	totals, err := repo.GetUsageTotals(ctx, UsageFilter{ProjectID: projectID})
	if err != nil {
		t.Fatalf("GetUsageTotals: %v", err)
	}
	if totals.CallCount != 1 || totals.InputTokens != 100 || totals.OutputTokens != 40 || totals.TotalTokens != 140 {
		t.Fatalf("unexpected totals: %+v", totals)
	}
	if totals.CachedInputTokens != 20 || totals.CacheCreationInputTokens != 5 || totals.CacheReadInputTokens != 15 || totals.ReasoningOutputTokens != 7 {
		t.Fatalf("unexpected detailed totals: %+v", totals)
	}
	if !totals.CostAvailable || totals.CostUSD == nil || *totals.CostUSD != cost {
		t.Fatalf("expected provider cost %v, got %+v", cost, totals)
	}

	breakdown, err := repo.GetModelUsageBreakdown(ctx, UsageFilter{ProjectID: projectID})
	if err != nil {
		t.Fatalf("GetModelUsageBreakdown: %v", err)
	}
	if len(breakdown) != 1 || breakdown[0].Provider != "openai" || breakdown[0].Model != "gpt-5.3-codex" || breakdown[0].Percent != 100 {
		t.Fatalf("unexpected breakdown: %+v", breakdown)
	}

	anthropicEvent := &models.LLMUsageEvent{
		Provider:     "anthropic",
		ProjectID:    projectID,
		Model:        "claude-sonnet",
		Operation:    "task",
		Status:       "completed",
		InputTokens:  25,
		OutputTokens: 15,
		OccurredAt:   time.Now().UTC().Add(2 * time.Hour),
	}
	if err := repo.RecordUsageEvent(ctx, anthropicEvent); err != nil {
		t.Fatalf("RecordUsageEvent anthropic: %v", err)
	}
	dailyByModel, err := repo.GetDailyUsageByModel(ctx, UsageFilter{ProjectID: projectID})
	if err != nil {
		t.Fatalf("GetDailyUsageByModel: %v", err)
	}
	if len(dailyByModel) != 2 {
		t.Fatalf("expected two daily model points, got %+v", dailyByModel)
	}
	rateByModel, err := repo.GetUsageRateBucketsByModel(ctx, UsageFilter{ProjectID: projectID, GroupBy: "day"})
	if err != nil {
		t.Fatalf("GetUsageRateBucketsByModel: %v", err)
	}
	if len(rateByModel) != 2 {
		t.Fatalf("expected two rate model points, got %+v", rateByModel)
	}
}

func TestUsageRepo_CostUnavailableWhenProviderCostMissing(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()

	if err := repo.RecordUsageEvent(ctx, &models.LLMUsageEvent{Provider: "anthropic", Model: "claude-sonnet", Operation: "task", InputTokens: 10, OutputTokens: 5}); err != nil {
		t.Fatalf("RecordUsageEvent: %v", err)
	}
	totals, err := repo.GetUsageTotals(ctx, UsageFilter{})
	if err != nil {
		t.Fatalf("GetUsageTotals: %v", err)
	}
	if totals.CostAvailable || totals.CostUSD != nil {
		t.Fatalf("expected cost unavailable, got %+v", totals)
	}
}

func TestUsageRepo_CreateAccountUsageSnapshotPersistsExtraLimits(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO agent_configs (id, name, provider, model, auth_method) VALUES (?, ?, ?, ?, ?)`, "agent-extra", "OpenAI", "openai", "gpt-test", "oauth"); err != nil {
		t.Fatalf("insert agent config: %v", err)
	}
	first := 12.5
	second := 44.0
	reset := "2026-06-08T05:00:00Z"
	snapshot := &models.AccountUsageSnapshot{
		Provider:      "openai",
		AccountID:     "acct-extra",
		AgentConfigID: "agent-extra",
		PrimaryLabel:  "5-hour session",
		ExtraLimits: []models.AccountUsageExtraLimit{
			{LimitKey: "gpt-5.3-codex-spark", Label: "GPT-5.3-Codex-Spark limit", UsedPercent: &first, ResetAt: &reset, RawJSON: `{"metered_feature":"gpt-5.3-codex-spark"}`},
			{LimitKey: "gpt-5.3-codex-pro", Label: "GPT-5.3-Codex-Pro limit", UsedPercent: &second, RawJSON: `{"metered_feature":"gpt-5.3-codex-pro"}`},
		},
	}
	if err := repo.CreateAccountUsageSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("CreateAccountUsageSnapshot: %v", err)
	}

	snapshots, err := repo.GetLatestAccountUsageSnapshots(ctx, "openai")
	if err != nil {
		t.Fatalf("GetLatestAccountUsageSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %+v", snapshots)
	}
	if len(snapshots[0].ExtraLimits) != 2 {
		t.Fatalf("expected two extra limits, got %+v", snapshots[0].ExtraLimits)
	}
	if snapshots[0].ExtraLimits[0].LimitKey != "gpt-5.3-codex-spark" || snapshots[0].ExtraLimits[1].LimitKey != "gpt-5.3-codex-pro" {
		t.Fatalf("unexpected extra limits: %+v", snapshots[0].ExtraLimits)
	}
}

func TestUsageRepo_GetLatestAccountUsageSnapshotsBreaksTimestampTiesByInsertOrder(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO agent_configs (id, name, provider, model, auth_method) VALUES (?, ?, ?, ?, ?)`, "agent", "OpenAI", "openai", "gpt-test", "oauth"); err != nil {
		t.Fatalf("insert agent config: %v", err)
	}
	fetchedAt := time.Date(2026, 6, 3, 17, 0, 0, 0, time.UTC)
	if err := repo.CreateAccountUsageSnapshot(ctx, &models.AccountUsageSnapshot{Provider: "openai", AccountID: "acct", AgentConfigID: "agent", PrimaryLabel: "5-hour session", FetchedAt: fetchedAt}); err != nil {
		t.Fatalf("create initial snapshot: %v", err)
	}
	if err := repo.CreateAccountUsageSnapshot(ctx, &models.AccountUsageSnapshot{Provider: "openai", AccountID: "acct", AgentConfigID: "agent", RateLimitReachedType: "refresh_failed_forbidden", RawJSON: `{"refresh_error":"refresh_failed_forbidden"}`, FetchedAt: fetchedAt}); err != nil {
		t.Fatalf("create failure snapshot: %v", err)
	}

	snapshots, err := repo.GetLatestAccountUsageSnapshots(ctx, "openai")
	if err != nil {
		t.Fatalf("GetLatestAccountUsageSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected one latest snapshot, got %+v", snapshots)
	}
	if snapshots[0].RateLimitReachedType != "refresh_failed_forbidden" {
		t.Fatalf("expected latest inserted snapshot to win timestamp tie, got %+v", snapshots[0])
	}
}
