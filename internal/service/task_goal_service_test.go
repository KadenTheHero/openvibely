package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func createServiceGoalTestProject(t *testing.T, ctx context.Context, db *sql.DB) *models.Project {
	t.Helper()
	project := &models.Project{Name: "Goal Service Project", RepoPath: t.TempDir()}
	if err := repository.NewProjectRepo(db).Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return project
}

func TestTaskGoalService_ValidationPauseResumeClear(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	project := createServiceGoalTestProject(t, ctx, db)
	taskRepo := repository.NewTaskRepo(db, nil)
	task := &models.Task{ProjectID: project.ID, Title: "Goal svc", Category: models.CategoryBacklog, Status: models.StatusPending, Prompt: "prompt", Priority: 2}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskGoalService(repository.NewTaskGoalRepo(db), taskRepo, nil)

	if _, err := svc.SetGoal(ctx, task.ID, "   ", GoalOptions{}); !errors.Is(err, ErrTaskGoalEmpty) {
		t.Fatalf("empty goal error = %v", err)
	}
	if _, err := svc.SetGoal(ctx, task.ID, strings.Repeat("x", MaxTaskGoalLength+1), GoalOptions{}); !errors.Is(err, ErrTaskGoalTooLong) {
		t.Fatalf("long goal error = %v", err)
	}
	goal, err := svc.SetGoal(ctx, task.ID, " All checks pass ", GoalOptions{Actor: "test"})
	if err != nil {
		t.Fatalf("set goal: %v", err)
	}
	if goal.Objective != "All checks pass" || goal.Status != models.TaskGoalStatusActive {
		t.Fatalf("goal = %+v", goal)
	}
	if err := svc.PauseGoal(ctx, task.ID, "test"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	paused, _ := svc.GetGoal(ctx, task.ID)
	if paused.Status != models.TaskGoalStatusPaused || paused.Objective != goal.Objective {
		t.Fatalf("paused goal = %+v", paused)
	}
	if _, err := svc.RecordBlockedReport(ctx, task.ID, paused.GoalID, "blocked", "blocked"); !errors.Is(err, ErrTaskGoalStaleUpdate) {
		t.Fatalf("blocked report on paused goal error = %v", err)
	}
	if _, err := svc.MarkAchieved(ctx, task.ID, paused.GoalID, "done"); !errors.Is(err, ErrTaskGoalStaleUpdate) {
		t.Fatalf("mark achieved on paused goal error = %v", err)
	}
	if err := svc.ResumeGoal(ctx, task.ID, "test"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := svc.RecordBlockedReport(ctx, task.ID, paused.GoalID, "blocked", "blocked"); err != nil {
		t.Fatalf("blocked report: %v", err)
	}
	if err := svc.PauseGoal(ctx, task.ID, "test"); err != nil {
		t.Fatalf("pause with audit: %v", err)
	}
	paused, _ = svc.GetGoal(ctx, task.ID)
	if paused.BlockerCount == 0 {
		t.Fatalf("expected blocker audit before resume, got %+v", paused)
	}
	if err := svc.ResumeGoal(ctx, task.ID, "test"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	resumed, _ := svc.GetGoal(ctx, task.ID)
	if resumed.Status != models.TaskGoalStatusActive || resumed.BlockerCount != 0 || resumed.GoalID != paused.GoalID {
		t.Fatalf("resumed goal = %+v", resumed)
	}
	if err := svc.ResumeGoal(ctx, task.ID, "test"); !errors.Is(err, ErrTaskGoalNotPaused) {
		t.Fatalf("resume active error = %v", err)
	}
	if _, err := svc.MarkAchieved(ctx, task.ID, "stale-goal-id", "done"); !errors.Is(err, ErrTaskGoalStaleUpdate) {
		t.Fatalf("stale achieved error = %v", err)
	}
	achieved, err := svc.MarkAchieved(ctx, task.ID, resumed.GoalID, "done")
	if err != nil {
		t.Fatalf("mark achieved before reactivation: %v", err)
	}
	if achieved.Status != models.TaskGoalStatusAchieved || achieved.AchievedAt == nil {
		t.Fatalf("achieved goal = %+v", achieved)
	}
	reactivated, err := svc.ReactivateAchievedGoal(ctx, task.ID, "web")
	if err != nil {
		t.Fatalf("reactivate achieved: %v", err)
	}
	if reactivated == nil || reactivated.Status != models.TaskGoalStatusActive || reactivated.GoalID != achieved.GoalID || reactivated.AchievedAt != nil {
		t.Fatalf("reactivated goal = %+v", reactivated)
	}
	reactivatedAgain, err := svc.ReactivateAchievedGoal(ctx, task.ID, "web")
	if err != nil {
		t.Fatalf("reactivate active goal: %v", err)
	}
	if reactivatedAgain != nil {
		t.Fatalf("expected no-op reactivating non-achieved goal, got %+v", reactivatedAgain)
	}
	if err := svc.ClearGoal(ctx, task.ID, "test"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	cleared, _ := svc.GetGoal(ctx, task.ID)
	if cleared.Status != models.TaskGoalStatusCleared {
		t.Fatalf("cleared goal = %+v", cleared)
	}
	if _, err := svc.MarkAchieved(ctx, task.ID, cleared.GoalID, "done"); !errors.Is(err, ErrTaskGoalStaleUpdate) {
		t.Fatalf("mark achieved on cleared goal error = %v", err)
	}
	if _, err := svc.RecordBlockedReport(ctx, task.ID, cleared.GoalID, "blocked", "blocked"); !errors.Is(err, ErrTaskGoalStaleUpdate) {
		t.Fatalf("blocked report on cleared goal error = %v", err)
	}
}
