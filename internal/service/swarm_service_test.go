package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func TestSwarmServiceCreateAndApplyPlannerOutput(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)

	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 3, WorkerIsolation: "worktree", ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatalf("CreateSwarmTask: %v", err)
	}
	if parent.SwarmRole != models.SwarmRoleParent {
		t.Fatalf("parent role=%q", parent.SwarmRole)
	}
	children, err := repo.ListSwarmChildren(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("ListSwarmChildren: %v", err)
	}
	if len(children) != 1 || children[0].SwarmRole != models.SwarmRolePlanner {
		t.Fatalf("planner child not created: %#v", children)
	}

	output := PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}}, ReviewerPrompt: "Review workers", MergerPrompt: "Integrate workers"}
	if err := svc.ApplyPlannerOutput(context.Background(), children[0].ID, output); err != nil {
		t.Fatalf("ApplyPlannerOutput: %v", err)
	}
	children, err = repo.ListSwarmChildren(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("ListSwarmChildren after planner: %v", err)
	}
	counts := map[models.SwarmRole]int{}
	for _, child := range children {
		counts[child.SwarmRole]++
	}
	if counts[models.SwarmRoleWorker] != 1 || counts[models.SwarmRoleReviewer] != 1 || counts[models.SwarmRoleMerger] != 1 {
		t.Fatalf("unexpected children: %#v", counts)
	}
	var workerChild *models.Task
	for i := range children {
		if children[i].SwarmRole == models.SwarmRoleWorker {
			workerChild = &children[i]
		}
	}
	if workerChild == nil || workerChild.ParentTaskID == nil || *workerChild.ParentTaskID != parent.ID {
		t.Fatalf("worker parent not set: %#v", workerChild)
	}
	if workerChild.WorktreePath == "" || workerChild.WorktreeBranch == "" {
		t.Fatalf("worker worktree metadata missing: %#v", workerChild)
	}
}

func TestSwarmServiceApplyPlannerOutputAllowsOverlappingWorktreeScopes(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)

	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Fix email switching", Prompt: "Fix email switching", MaxWorkers: 4, WorkerIsolation: "worktree", ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatalf("CreateSwarmTask: %v", err)
	}
	planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: planner=%#v err=%v", planner, err)
	}

	output := PlannerOutput{
		Workers: []PlannerWorker{
			{Title: "Email runtime fixer", Prompt: "Fix email runtime switch_project", WorkerKind: "backend", Ownership: []string{"internal/service/email_service.go"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, ReadScope: []string{"."}, Required: true},
			{Title: "Cross-channel comparison", Prompt: "Compare channel switch_project behavior", WorkerKind: "backend", Ownership: []string{"internal/service/chat_action_runtime.go"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, ReadScope: []string{"."}, Required: true},
		},
		ReviewerPrompt: "Review overlapping service changes and conflicts",
		MergerPrompt:   "Integrate accepted service changes",
	}
	if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, output); err != nil {
		t.Fatalf("ApplyPlannerOutput should allow overlapping isolated worktree scopes: %v", err)
	}
	children, err := repo.ListSwarmChildren(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("ListSwarmChildren: %v", err)
	}
	workers := 0
	for _, child := range children {
		if child.SwarmRole == models.SwarmRoleWorker {
			workers++
		}
	}
	if workers != 2 {
		t.Fatalf("expected 2 workers from overlapping scopes, got %d children=%#v", workers, children)
	}
}

func TestSwarmServiceCreateSwarmTaskStartsPlannerForActiveCategory(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	startImmediately := false

	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Active swarm", Prompt: "Build export", Category: models.CategoryActive, MaxWorkers: 3, WorkerIsolation: "worktree", ReviewerEnabled: true, MergerEnabled: true, StartImmediately: &startImmediately})
	if err != nil {
		t.Fatalf("CreateSwarmTask: %v", err)
	}
	planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("active swarm should create planner regardless of start_immediately flag: planner=%#v err=%v", planner, err)
	}
	if planner.Status != models.StatusPending || planner.Category != models.CategoryActive {
		t.Fatalf("planner not runnable after active swarm creation: category=%s status=%s", planner.Category, planner.Status)
	}
}

func TestSwarmServiceCreateSwarmTaskDefersPlannerForBacklogCategory(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	startImmediately := true

	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Backlog swarm", Prompt: "Build export", Category: models.CategoryBacklog, MaxWorkers: 3, WorkerIsolation: "worktree", ReviewerEnabled: true, MergerEnabled: true, StartImmediately: &startImmediately})
	if err != nil {
		t.Fatalf("CreateSwarmTask: %v", err)
	}
	if parent.Category != models.CategoryBacklog {
		t.Fatalf("parent category=%s, want backlog", parent.Category)
	}
	if planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner); err != nil {
		t.Fatalf("FindSwarmChildByRole: %v", err)
	} else if planner != nil {
		t.Fatalf("backlog swarm must not start planner even when start_immediately is true, got %#v", planner)
	}

	if err := svc.StartPlanner(context.Background(), parent.ID); err != nil {
		t.Fatalf("StartPlanner: %v", err)
	}
	planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner not created on explicit start: planner=%#v err=%v", planner, err)
	}
	storedParent, err := repo.GetByID(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedParent.Category != models.CategoryActive {
		t.Fatalf("parent not activated by explicit start: category=%s", storedParent.Category)
	}
	if planner.Status != models.StatusPending || planner.Category != models.CategoryActive {
		t.Fatalf("planner not runnable after explicit start: category=%s status=%s", planner.Category, planner.Status)
	}
}

func TestSwarmServiceAppliesPlannerOutputOnPlannerCompletion(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	exec := &models.Execution{TaskID: planner.ID, Status: models.ExecRunning, PromptSent: planner.Prompt}
	if err := execRepo.Create(context.Background(), exec); err != nil {
		t.Fatalf("create planner execution: %v", err)
	}
	plannerJSON := `{"workers":[{"title":"Backend worker","prompt":"Do backend","worker_kind":"backend","ownership":["internal/service"],"isolation":"worktree","write_scope":["internal/service"],"required":true}],"reviewer_prompt":"Review workers","merger_prompt":"Integrate workers"}`
	if err := execRepo.Complete(context.Background(), exec.ID, models.ExecCompleted, plannerJSON, "", 0, 1); err != nil {
		t.Fatalf("complete planner execution: %v", err)
	}
	if err := repo.UpdateStatus(context.Background(), planner.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), planner.ID); err != nil {
		t.Fatalf("OnChildCompleted planner: %v", err)
	}
	children, err := repo.ListSwarmChildren(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[models.SwarmRole]int{}
	for _, child := range children {
		counts[child.SwarmRole]++
	}
	if counts[models.SwarmRoleWorker] != 1 || counts[models.SwarmRoleReviewer] != 1 || counts[models.SwarmRoleMerger] != 1 {
		t.Fatalf("planner completion did not create swarm children: %#v", counts)
	}
	if err := svc.OnChildCompleted(context.Background(), planner.ID); err != nil {
		t.Fatalf("duplicate OnChildCompleted planner: %v", err)
	}
	children, err = repo.ListSwarmChildren(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts = map[models.SwarmRole]int{}
	for _, child := range children {
		counts[child.SwarmRole]++
	}
	if counts[models.SwarmRoleWorker] != 1 || counts[models.SwarmRoleReviewer] != 1 || counts[models.SwarmRoleMerger] != 1 {
		t.Fatalf("duplicate planner completion created extra children: %#v", counts)
	}
}

func TestSwarmServiceTerminalizesChildSwarmStatusOnCompletion(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	ctx := context.Background()

	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}); err != nil {
		t.Fatal(err)
	}
	planner, err = repo.GetByID(ctx, planner.ID)
	if err != nil || planner == nil {
		t.Fatalf("reload planner: %v", err)
	}
	if planner.Status != models.StatusCompleted || planner.SwarmStatus != "planned" {
		t.Fatalf("planner status not terminalized: status=%s swarm_status=%s", planner.Status, planner.SwarmStatus)
	}

	worker, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleWorker)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, worker.ID); err != nil {
		t.Fatal(err)
	}
	worker, _ = repo.GetByID(ctx, worker.ID)
	if worker.SwarmStatus != "completed" {
		t.Fatalf("worker swarm_status not terminalized: %s", worker.SwarmStatus)
	}

	reviewer, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleReviewer)
	if err != nil || reviewer == nil {
		t.Fatalf("reviewer missing: %v", err)
	}
	if reviewer.Status != models.StatusPending || reviewer.SwarmStatus != "ready" {
		t.Fatalf("reviewer not started after worker completion: status=%s swarm_status=%s", reviewer.Status, reviewer.SwarmStatus)
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	reviewer, _ = repo.GetByID(ctx, reviewer.ID)
	if reviewer.SwarmStatus != "reviewed" {
		t.Fatalf("reviewer swarm_status not terminalized: %s", reviewer.SwarmStatus)
	}

	merger, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleMerger)
	if err != nil || merger == nil {
		t.Fatalf("merger missing: %v", err)
	}
	if merger.Status != models.StatusPending || merger.SwarmStatus != "ready" {
		t.Fatalf("merger not started after reviewer completion: status=%s swarm_status=%s", merger.Status, merger.SwarmStatus)
	}
	exec := &models.Execution{TaskID: merger.ID, Status: models.ExecRunning, PromptSent: merger.Prompt}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.Complete(ctx, exec.ID, models.ExecCompleted, "Final integrated output", "", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, merger.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, merger.ID); err != nil {
		t.Fatal(err)
	}
	merger, _ = repo.GetByID(ctx, merger.ID)
	if merger.SwarmStatus != "integrated" {
		t.Fatalf("merger swarm_status not terminalized: %s", merger.SwarmStatus)
	}
}

func TestSwarmServiceCompletesReviewerOnlySwarmWithoutMerger(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	ctx := context.Background()

	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}}, ReviewerPrompt: "Review"}); err != nil {
		t.Fatal(err)
	}
	if merger, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleMerger); err != nil || merger != nil {
		t.Fatalf("merger should not exist when disabled, merger=%#v err=%v", merger, err)
	}
	worker, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleWorker)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, worker.ID); err != nil {
		t.Fatal(err)
	}
	reviewer, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleReviewer)
	if err != nil || reviewer == nil {
		t.Fatalf("reviewer missing: %v", err)
	}
	if reviewer.Status != models.StatusPending {
		t.Fatalf("reviewer not started after worker completion: %#v", reviewer)
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, reviewer.ID); err != nil {
		t.Fatal(err)
	}

	parent, err = repo.GetByID(ctx, parent.ID)
	if err != nil || parent == nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.Status != models.StatusCompleted || parent.Category != models.CategoryCompleted || parent.SwarmStatus != "current" {
		t.Fatalf("parent not completed without merger: status=%s category=%s swarm_status=%s", parent.Status, parent.Category, parent.SwarmStatus)
	}
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	if parentCfg.MergedGeneration != parentCfg.Generation || parentCfg.Generation != 1 {
		t.Fatalf("parent freshness not marked complete without merger: %#v", parentCfg)
	}
	parentExecs, err := execRepo.ListByTask(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentExecs) != 0 {
		t.Fatalf("merger-disabled swarm should not fabricate parent execution, got %#v", parentExecs)
	}
}

func TestSwarmServiceCompletesWorkerOnlySwarmWithoutReviewerOrMerger(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	ctx := context.Background()

	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: false, MergerEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}}}); err != nil {
		t.Fatal(err)
	}
	if reviewer, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleReviewer); err != nil || reviewer != nil {
		t.Fatalf("reviewer should not exist when disabled, reviewer=%#v err=%v", reviewer, err)
	}
	if merger, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleMerger); err != nil || merger != nil {
		t.Fatalf("merger should not exist when disabled, merger=%#v err=%v", merger, err)
	}
	worker, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleWorker)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, worker.ID); err != nil {
		t.Fatal(err)
	}

	parent, err = repo.GetByID(ctx, parent.ID)
	if err != nil || parent == nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.Status != models.StatusCompleted || parent.Category != models.CategoryCompleted || parent.SwarmStatus != "current" {
		t.Fatalf("worker-only parent not completed: status=%s category=%s swarm_status=%s", parent.Status, parent.Category, parent.SwarmStatus)
	}
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	if parentCfg.MergedGeneration != parentCfg.Generation || parentCfg.Generation != 1 {
		t.Fatalf("worker-only parent freshness not marked complete: %#v", parentCfg)
	}
}

func TestSwarmServiceStartsMergerAfterWorkersWhenReviewerDisabled(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	workerSvc := newTestWorkerService(t)
	svc := NewSwarmService(taskSvc, repo, nil, workerSvc)
	ctx := context.Background()

	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: false, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}}, MergerPrompt: "Integrate"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerSvc.Submitted():
	default:
		t.Fatal("expected initial worker submission")
	}
	if reviewer, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleReviewer); err != nil || reviewer != nil {
		t.Fatalf("reviewer should not exist when disabled, reviewer=%#v err=%v", reviewer, err)
	}
	worker, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleWorker)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	if err := svc.OnChildCompleted(ctx, worker.ID); err != nil {
		t.Fatal(err)
	}

	merger, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleMerger)
	if err != nil || merger == nil {
		t.Fatalf("merger missing: %v", err)
	}
	if merger.Status != models.StatusPending || merger.Category != models.CategoryActive || merger.SwarmStatus != "ready" {
		t.Fatalf("merger not started after worker completion without reviewer: status=%s category=%s swarm_status=%s", merger.Status, merger.Category, merger.SwarmStatus)
	}
	intCfg, _ := models.ParseSwarmConfig(merger.SwarmConfig)
	if intCfg.RerunGeneration != 1 {
		t.Fatalf("merger target generation=%d, want 1", intCfg.RerunGeneration)
	}
	select {
	case submitted := <-workerSvc.Submitted():
		if submitted.ID != merger.ID {
			t.Fatalf("submitted task ID=%s, want merger %s", submitted.ID, merger.ID)
		}
		if submitted.Status != models.StatusPending || submitted.Category != models.CategoryActive {
			t.Fatalf("submitted merger not runnable: status=%s category=%s", submitted.Status, submitted.Category)
		}
	default:
		t.Fatal("expected merger to be submitted")
	}
}

func TestPlannerPromptRoleBoundsPlannerToDelegationOnly(t *testing.T) {
	prompt := plannerPrompt("Fix the bug", 3)

	required := []string{
		"Your only job is to decompose the goal into worker tasks and handoff instructions.",
		"You are not a worker, reviewer, or merger.",
		"Do not implement the requested feature or bug fix yourself.",
		"Do not modify, create, delete, format, or regenerate files.",
		"Do not run build, test, formatter, generator, git, or shell commands.",
		"Return exactly one raw JSON object and nothing else.",
		"Do not wrap the JSON in Markdown fences.",
		"\"workers\"",
		"\"reviewer_prompt\"",
		"\"merger_prompt\"",
		"\"notes\"",
	}
	for _, want := range required {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planner prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestCoordinatorFollowupPromptRoleBoundsPlannerToDelegationOnly(t *testing.T) {
	prompt := coordinatorFollowupPrompt("Parent goal", "Follow-up", 2)

	required := []string{
		"Your only job is to decide which workers need new delegated work.",
		"You are not a worker, reviewer, or merger.",
		"Do not implement the follow-up yourself.",
		"Do not modify, create, delete, format, or regenerate files.",
		"Do not run build, test, formatter, generator, git, or shell commands.",
		"Return exactly one raw JSON object and nothing else.",
		"Do not wrap the JSON in Markdown fences.",
		"For existing affected workers, include their existing task_id.",
		"\"workers\"",
		"\"reviewer_prompt\"",
		"\"merger_prompt\"",
		"\"notes\"",
	}
	for _, want := range required {
		if !strings.Contains(prompt, want) {
			t.Fatalf("coordinator prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestParsePlannerOutputJSONExtractsPlannerObjectFromTranscript(t *testing.T) {
	raw := `I’ll produce the swarm task JSON directly and first load context.
[Using tool: memory_view]
{"handle":"chat_thread_system.md","body":"not the planner"}

Here is the bounded plan:
{"workers":[{"title":"Email runtime fixer","prompt":"Fix email runtime switch project","worker_kind":"backend","ownership":["internal/service"],"isolation":"worktree","write_scope":["internal/service"],"read_scope":["."],"required":true}],"reviewer_prompt":"Review email runtime fix","merger_prompt":"Integrate email runtime fix","notes":"One worker is enough."}`

	out, err := ParsePlannerOutputJSON(raw)
	if err != nil {
		t.Fatalf("ParsePlannerOutputJSON: %v", err)
	}
	if len(out.Workers) != 1 || out.Workers[0].Title != "Email runtime fixer" {
		t.Fatalf("parsed wrong planner workers: %#v", out.Workers)
	}
	if out.ReviewerPrompt == "" || out.MergerPrompt == "" {
		t.Fatalf("expected reviewer and merger prompts: %#v", out)
	}
}

func TestParsePlannerOutputJSONExtractsFencedPlannerObject(t *testing.T) {
	raw := "Planner output:\n```json\n{" + `"workers":[{"title":"Backend worker","prompt":"Do backend","worker_kind":"backend","ownership":["internal/service"],"isolation":"worktree","write_scope":["internal/service"],"required":true}],"reviewer_prompt":"Review","merger_prompt":"Integrate"` + "}\n```"

	out, err := ParsePlannerOutputJSON(raw)
	if err != nil {
		t.Fatalf("ParsePlannerOutputJSON: %v", err)
	}
	if len(out.Workers) != 1 || out.Workers[0].Title != "Backend worker" {
		t.Fatalf("parsed wrong planner output: %#v", out)
	}
}

func TestParsePlannerOutputJSONPrefersFinalPlannerObject(t *testing.T) {
	raw := "Earlier transcript contained a stale candidate:\n" +
		`{"workers":[{"title":"Stale worker","prompt":"Do the old plan","worker_kind":"backend","ownership":["old"],"isolation":"worktree","write_scope":["old"],"required":true}],"reviewer_prompt":"Review stale","merger_prompt":"Integrate stale"}` +
		"\n\nAfter considering the follow-up, use this final planner JSON:\n```json\n" +
		`{"workers":[{"title":"Final worker","prompt":"Do the final plan","worker_kind":"backend","ownership":["internal/service"],"isolation":"worktree","write_scope":["internal/service"],"required":true}],"reviewer_prompt":"Review final","merger_prompt":"Integrate final"}` +
		"\n```"

	out, err := ParsePlannerOutputJSON(raw)
	if err != nil {
		t.Fatalf("ParsePlannerOutputJSON: %v", err)
	}
	if len(out.Workers) != 1 || out.Workers[0].Title != "Final worker" {
		t.Fatalf("parsed stale planner output: %#v", out.Workers)
	}
	if out.ReviewerPrompt != "Review final" || out.MergerPrompt != "Integrate final" {
		t.Fatalf("parsed stale prompts: %#v", out)
	}
}

func TestSwarmServiceStartPlannerReactivatesExistingPlannerBeforeSubmit(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	workerSvc := newTestWorkerService(t)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := repo.UpdateCategory(context.Background(), planner.ID, models.CategoryBacklog); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), planner.ID, models.StatusFailed); err != nil {
		t.Fatal(err)
	}
	svc.workerSvc = workerSvc

	if err := svc.StartPlanner(context.Background(), parent.ID); err != nil {
		t.Fatalf("StartPlanner: %v", err)
	}

	select {
	case submitted := <-workerSvc.Submitted():
		if submitted.ID != planner.ID {
			t.Fatalf("submitted task ID=%s, want planner %s", submitted.ID, planner.ID)
		}
		if submitted.Category != models.CategoryActive || submitted.Status != models.StatusPending {
			t.Fatalf("submitted planner not runnable: category=%s status=%s", submitted.Category, submitted.Status)
		}
	default:
		t.Fatal("expected planner to be submitted")
	}
	planner, err = repo.GetByID(context.Background(), planner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planner.Category != models.CategoryActive || planner.Status != models.StatusPending {
		t.Fatalf("persisted planner not runnable: category=%s status=%s", planner.Category, planner.Status)
	}
}

func TestSwarmServiceInvalidPlannerExecutionBlocksParent(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	exec := &models.Execution{TaskID: planner.ID, Status: models.ExecRunning, PromptSent: planner.Prompt}
	if err := execRepo.Create(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.Complete(context.Background(), exec.ID, models.ExecCompleted, "not json", "", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), planner.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), planner.ID); err == nil {
		t.Fatal("expected invalid planner JSON error")
	}
	planner, _ = repo.GetByID(context.Background(), planner.ID)
	if planner.Status != models.StatusFailed || planner.SwarmStatus != "invalid_plan" {
		t.Fatalf("planner not marked invalid: status=%s swarm_status=%s", planner.Status, planner.SwarmStatus)
	}
	parent, _ = repo.GetByID(context.Background(), parent.ID)
	if parent.Status != models.StatusBlocked || parent.SwarmStatus != "blocked" {
		t.Fatalf("parent not blocked: status=%s swarm_status=%s", parent.Status, parent.SwarmStatus)
	}
}

func TestSwarmServiceMergerCompletionPersistsParentResult(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	output := PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}
	if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, output); err != nil {
		t.Fatal(err)
	}
	children, err := repo.ListSwarmChildren(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		switch child.SwarmRole {
		case models.SwarmRoleWorker:
			cfg.CompletedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(context.Background(), child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleReviewer:
			cfg.ReviewedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(context.Background(), child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		}
	}
	merger, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleMerger)
	if merger == nil {
		t.Fatal("merger missing")
	}
	exec := &models.Execution{TaskID: merger.ID, Status: models.ExecRunning, PromptSent: merger.Prompt}
	if err := execRepo.Create(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.UpdateDiffOutput(context.Background(), exec.ID, "diff --git a/final.go b/final.go"); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.Complete(context.Background(), exec.ID, models.ExecCompleted, "Final integrated summary", "", 12, 34); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), merger.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), merger.ID); err != nil {
		t.Fatal(err)
	}
	updatedParent, err := repo.GetByID(context.Background(), parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("get parent: %v", err)
	}
	if updatedParent.Status != models.StatusCompleted || updatedParent.Category != models.CategoryCompleted || updatedParent.MergeStatus != models.MergeStatusPending {
		t.Fatalf("parent not finalized with pending merge: status=%s category=%s merge=%s", updatedParent.Status, updatedParent.Category, updatedParent.MergeStatus)
	}
	parentExecs, err := execRepo.ListByTask(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentExecs) != 1 || parentExecs[0].Output != "Final integrated summary" || parentExecs[0].DiffOutput != "" {
		t.Fatalf("parent execution summary not stored through light list: %#v", parentExecs)
	}
	parentExec, err := execRepo.GetByID(context.Background(), parentExecs[0].ID)
	if err != nil || parentExec == nil {
		t.Fatalf("get parent execution: %v", err)
	}
	if parentExec.Output != "Final integrated summary" || parentExec.DiffOutput != "diff --git a/final.go b/final.go" {
		t.Fatalf("parent result execution mismatch: output=%q diff=%q", parentExec.Output, parentExec.DiffOutput)
	}
	if err := svc.OnChildCompleted(context.Background(), merger.ID); err != nil {
		t.Fatal(err)
	}
	parentExecs, err = execRepo.ListByTask(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentExecs) != 1 {
		t.Fatalf("merger completion should be idempotent, got %d parent executions", len(parentExecs))
	}
}

func TestTaskServiceUpdateCategoryNotifiesSwarmChildCancellation(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	taskSvc.SetSwarmService(svc)
	ctx := context.Background()
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Swarm parent", Prompt: "Build result", Category: models.CategoryActive, MaxWorkers: 1, WorkerIsolation: "worktree", ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{
		Workers:        []PlannerWorker{{Title: "API worker", Prompt: "Update API", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}},
		ReviewerPrompt: "Review worker",
		MergerPrompt:   "Integrate worker",
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleWorker)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateCategory(ctx, worker.ID, models.CategoryActive); err != nil {
		t.Fatal(err)
	}

	if err := taskSvc.UpdateCategory(ctx, worker.ID, models.CategoryCompleted); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}

	updatedWorker, err := repo.GetByID(ctx, worker.ID)
	if err != nil || updatedWorker == nil {
		t.Fatalf("updated worker missing: %v", err)
	}
	if updatedWorker.Status != models.StatusCancelled || updatedWorker.Category != models.CategoryCompleted {
		t.Fatalf("worker status/category = %s/%s", updatedWorker.Status, updatedWorker.Category)
	}
	updatedParent, err := repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("updated parent missing: %v", err)
	}
	if updatedParent.Status != models.StatusBlocked || updatedParent.SwarmStatus != "needs_coordination" {
		t.Fatalf("parent status/swarm_status = %s/%s", updatedParent.Status, updatedParent.SwarmStatus)
	}
}

func TestTaskServiceUpdateCategoryNotifiesPendingSwarmChildCancellation(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	taskSvc.SetSwarmService(svc)
	ctx := context.Background()
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Swarm parent", Prompt: "Build result", Category: models.CategoryActive, MaxWorkers: 1, WorkerIsolation: "worktree", ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{
		Workers:        []PlannerWorker{{Title: "API worker", Prompt: "Update API", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}},
		ReviewerPrompt: "Review worker",
		MergerPrompt:   "Integrate worker",
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleWorker)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusPending); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateCategory(ctx, worker.ID, models.CategoryActive); err != nil {
		t.Fatal(err)
	}

	if err := taskSvc.UpdateCategory(ctx, worker.ID, models.CategoryCompleted); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}

	updatedWorker, err := repo.GetByID(ctx, worker.ID)
	if err != nil || updatedWorker == nil {
		t.Fatalf("updated worker missing: %v", err)
	}
	if updatedWorker.Status != models.StatusCancelled || updatedWorker.Category != models.CategoryCompleted {
		t.Fatalf("worker status/category = %s/%s", updatedWorker.Status, updatedWorker.Category)
	}
	updatedParent, err := repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("updated parent missing: %v", err)
	}
	if updatedParent.Status != models.StatusBlocked || updatedParent.SwarmStatus != "needs_coordination" {
		t.Fatalf("parent status/swarm_status = %s/%s", updatedParent.Status, updatedParent.SwarmStatus)
	}
}

func TestSwarmServiceChildCancellationSetsRoleSpecificParentState(t *testing.T) {
	roles := []struct {
		name       string
		role       models.SwarmRole
		wantStatus string
	}{
		{name: "worker", role: models.SwarmRoleWorker, wantStatus: "needs_coordination"},
		{name: "reviewer", role: models.SwarmRoleReviewer, wantStatus: "needs_review"},
		{name: "merger", role: models.SwarmRoleMerger, wantStatus: "needs_integration"},
	}
	for _, tc := range roles {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.NewTestDB(t)
			repo := repository.NewTaskRepo(db, nil)
			taskSvc := NewTaskService(repo, nil, nil)
			svc := NewSwarmService(taskSvc, repo, nil, nil)
			parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
			if planner == nil {
				t.Fatal("planner missing")
			}
			output := PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}
			if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, output); err != nil {
				t.Fatal(err)
			}
			child, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, tc.role)
			if child == nil {
				t.Fatalf("%s child missing", tc.role)
			}
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCancelled); err != nil {
				t.Fatal(err)
			}
			if err := svc.OnChildCompleted(context.Background(), child.ID); err != nil {
				t.Fatal(err)
			}
			updatedParent, err := repo.GetByID(context.Background(), parent.ID)
			if err != nil || updatedParent == nil {
				t.Fatalf("get parent: %v", err)
			}
			if updatedParent.Status != models.StatusBlocked || updatedParent.SwarmStatus != tc.wantStatus {
				t.Fatalf("parent after %s cancel: status=%s swarm_status=%s", tc.role, updatedParent.Status, updatedParent.SwarmStatus)
			}
		})
	}
}

func TestSwarmServiceParentFollowupCoordinatesAffectedWorkers(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 3, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	initialOutput := PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}, {Title: "Frontend worker", Prompt: "Do frontend", WorkerKind: "frontend", Ownership: []string{"web/templates"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}
	if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, initialOutput); err != nil {
		t.Fatal(err)
	}
	children, _ := repo.ListSwarmChildren(context.Background(), parent.ID)
	var backend, frontend *models.Task
	for i := range children {
		child := children[i]
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		switch child.SwarmRole {
		case models.SwarmRoleWorker:
			cfg.CompletedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(context.Background(), child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
			if cfg.WorkerKind == "backend" {
				backend = &child
			} else if cfg.WorkerKind == "frontend" {
				frontend = &child
			}
		case models.SwarmRoleReviewer:
			cfg.ReviewedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(context.Background(), child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleMerger:
			cfg.MergedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(context.Background(), child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		}
	}
	if backend == nil || frontend == nil {
		t.Fatalf("workers missing: backend=%#v frontend=%#v", backend, frontend)
	}
	parent, _ = repo.GetByID(context.Background(), parent.ID)
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.MergedGeneration = 1
	parent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(context.Background(), parent.ID, parent.SwarmRole, "current", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		t.Fatal(err)
	}

	if err := svc.HandleParentFollowup(context.Background(), parent.ID, "Only update backend behavior"); err != nil {
		t.Fatal(err)
	}
	planner, _ = repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil || planner.Status != models.StatusPending || !strings.Contains(planner.Prompt, "Only update backend behavior") {
		t.Fatalf("planner was not prepared for coordination follow-up: %#v", planner)
	}
	followupOutput := PlannerOutput{Workers: []PlannerWorker{{TaskID: backend.ID, Title: "Backend worker", Prompt: "Update backend only", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review backend update", MergerPrompt: "Integrate backend update"}
	if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, followupOutput); err != nil {
		t.Fatal(err)
	}
	backendAfter, _ := repo.GetByID(context.Background(), backend.ID)
	frontendAfter, _ := repo.GetByID(context.Background(), frontend.ID)
	backendCfg, _ := models.ParseSwarmConfig(backendAfter.SwarmConfig)
	frontendCfg, _ := models.ParseSwarmConfig(frontendAfter.SwarmConfig)
	if backendAfter.Status != models.StatusPending || backendCfg.RerunGeneration != 2 || backendCfg.CompletedGeneration >= 2 {
		t.Fatalf("affected backend not queued for generation 2: status=%s cfg=%#v", backendAfter.Status, backendCfg)
	}
	if frontendAfter.Status != models.StatusCompleted || frontendCfg.CompletedGeneration != 2 {
		t.Fatalf("unaffected frontend not carried forward: status=%s cfg=%#v", frontendAfter.Status, frontendCfg)
	}
	reviewer, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleReviewer)
	merger, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleMerger)
	if reviewer == nil || reviewer.Status != models.StatusBlocked || merger == nil || merger.Status != models.StatusBlocked {
		t.Fatalf("reviewer/merger should wait for affected worker: reviewer=%#v merger=%#v", reviewer, merger)
	}
	backendCfg.CompletedGeneration = 2
	backendAfter.SwarmConfig, _ = backendCfg.JSON()
	if err := repo.UpdateSwarmFields(context.Background(), backendAfter.ID, backendAfter.SwarmRole, backendAfter.SwarmStatus, backendAfter.SwarmConfig, backendAfter.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), backendAfter.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), backendAfter.ID); err != nil {
		t.Fatal(err)
	}
	reviewer, _ = repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleReviewer)
	if reviewer == nil || reviewer.Status != models.StatusPending {
		t.Fatalf("reviewer not rerun after affected worker completed: %#v", reviewer)
	}
	revCfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
	revCfg.ReviewedGeneration = 2
	reviewer.SwarmConfig, _ = revCfg.JSON()
	if err := repo.UpdateSwarmFields(context.Background(), reviewer.ID, reviewer.SwarmRole, reviewer.SwarmStatus, reviewer.SwarmConfig, reviewer.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), reviewer.ID); err != nil {
		t.Fatal(err)
	}
	merger, _ = repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleMerger)
	if merger == nil || merger.Status != models.StatusPending {
		t.Fatalf("merger not rerun after reviewer completed: %#v", merger)
	}
}

func TestSwarmServiceStartsReviewerAndMergerOnce(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	output := PlannerOutput{Workers: []PlannerWorker{{Title: "Worker A", Prompt: "A", WorkerKind: "backend", Ownership: []string{"a"}, Isolation: "worktree", Required: true}, {Title: "Worker B", Prompt: "B", WorkerKind: "frontend", Ownership: []string{"b"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}
	if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, output); err != nil {
		t.Fatal(err)
	}
	children, _ := repo.ListSwarmChildren(context.Background(), parent.ID)
	workerID := ""
	reviewerID := ""
	for _, child := range children {
		if child.SwarmRole == models.SwarmRoleWorker {
			workerID = child.ID
			if err := repo.UpdateStatus(context.Background(), child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
			cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
			cfg.CompletedGeneration = 1
			b, _ := cfg.JSON()
			child.SwarmConfig = b
			if err := repo.UpdateSwarmFields(context.Background(), child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
		}
		if child.SwarmRole == models.SwarmRoleReviewer {
			reviewerID = child.ID
			if err := repo.UpdateCategory(context.Background(), child.ID, models.CategoryCompleted); err != nil {
				t.Fatal(err)
			}
		}
	}
	if workerID == "" {
		t.Fatal("worker missing")
	}
	if reviewerID == "" {
		t.Fatal("reviewer missing")
	}
	if err := svc.OnChildCompleted(context.Background(), workerID); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), workerID); err != nil {
		t.Fatal(err)
	}
	reviewer, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleReviewer)
	if reviewer == nil || reviewer.Status != models.StatusPending || reviewer.Category != models.CategoryActive {
		t.Fatalf("reviewer not pending and active once: %#v", reviewer)
	}
	if err := repo.UpdateStatus(context.Background(), reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	cfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
	cfg.ReviewedGeneration = 1
	b, _ := cfg.JSON()
	reviewer.SwarmConfig = b
	if err := repo.UpdateSwarmFields(context.Background(), reviewer.ID, reviewer.SwarmRole, reviewer.SwarmStatus, reviewer.SwarmConfig, reviewer.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), reviewer.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), reviewer.ID); err != nil {
		t.Fatal(err)
	}
	merger, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleMerger)
	if merger == nil || merger.Status != models.StatusPending {
		t.Fatalf("merger not pending once: %#v", merger)
	}
}

func TestSwarmServiceRerunReviewerStartsMergerAfterReviewerCompletes(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	ctx := context.Background()

	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}); err != nil {
		t.Fatal(err)
	}
	children, err := repo.ListSwarmChildren(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		switch child.SwarmRole {
		case models.SwarmRoleWorker:
			cfg.CompletedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleReviewer:
			cfg.ReviewedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleMerger:
			cfg.MergedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		}
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.Generation = 1
	parentCfg.MergedGeneration = 1
	parent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "current", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateCategory(ctx, parent.ID, models.CategoryCompleted); err != nil {
		t.Fatal(err)
	}

	rerunReviewer, err := svc.RerunRole(ctx, parent.ID, models.SwarmRoleReviewer)
	if err != nil {
		t.Fatalf("RerunRole reviewer: %v", err)
	}
	if rerunReviewer == nil || rerunReviewer.Status != models.StatusPending || rerunReviewer.Category != models.CategoryActive {
		t.Fatalf("reviewer not queued for active rerun: %#v", rerunReviewer)
	}
	persistedReviewer, err := repo.GetByID(ctx, rerunReviewer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedReviewer.Category != models.CategoryActive {
		t.Fatalf("reviewer rerun category = %s, want active", persistedReviewer.Category)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ = models.ParseSwarmConfig(parent.SwarmConfig)
	if parent.SwarmStatus != "needs_review" || parentCfg.MergedGeneration >= parentCfg.Generation {
		t.Fatalf("parent integration freshness not invalidated: status=%s cfg=%#v", parent.SwarmStatus, parentCfg)
	}
	if parent.Status != models.StatusRunning || parent.Category != models.CategoryActive {
		t.Fatalf("parent not reactivated for reviewer rerun: status=%s category=%s", parent.Status, parent.Category)
	}
	if err := repo.UpdateStatus(ctx, rerunReviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, rerunReviewer.ID); err != nil {
		t.Fatal(err)
	}
	merger, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleMerger)
	if err != nil || merger == nil {
		t.Fatalf("merger missing: %v", err)
	}
	if merger.Status != models.StatusPending || merger.Category != models.CategoryActive {
		t.Fatalf("merger not rerun as active task after reviewer retry completed: %#v", merger)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateCategory(ctx, parent.ID, models.CategoryCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, merger.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	rerunMerger, err := svc.RerunRole(ctx, parent.ID, models.SwarmRoleMerger)
	if err != nil {
		t.Fatalf("RerunRole merger: %v", err)
	}
	if rerunMerger == nil || rerunMerger.Status != models.StatusPending || rerunMerger.Category != models.CategoryActive {
		t.Fatalf("merger not queued for active rerun: %#v", rerunMerger)
	}
	persistedMerger, err := repo.GetByID(ctx, rerunMerger.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedMerger.Category != models.CategoryActive {
		t.Fatalf("merger rerun category = %s, want active", persistedMerger.Category)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ = models.ParseSwarmConfig(parent.SwarmConfig)
	if parent.SwarmStatus != "needs_integration" || parent.Status != models.StatusRunning || parent.Category != models.CategoryActive {
		t.Fatalf("parent not reactivated for merger rerun: swarm=%s status=%s category=%s", parent.SwarmStatus, parent.Status, parent.Category)
	}
	mergerCfg, _ := models.ParseSwarmConfig(rerunMerger.SwarmConfig)
	if parentCfg.MergedGeneration >= mergerCfg.RerunGeneration {
		t.Fatalf("parent integration freshness not invalidated for merger rerun: parent=%#v merger=%#v", parentCfg, mergerCfg)
	}
	integrationRun := &models.Execution{TaskID: rerunMerger.ID, Status: models.ExecRunning, PromptSent: "Integrate again"}
	if err := execRepo.Create(ctx, integrationRun); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.Complete(ctx, integrationRun.ID, models.ExecCompleted, "Final rerun result", "", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, rerunMerger.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(ctx, rerunMerger.ID); err != nil {
		t.Fatal(err)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ = models.ParseSwarmConfig(parent.SwarmConfig)
	if parent.SwarmStatus != "current" || parent.Status != models.StatusCompleted || parentCfg.MergedGeneration < mergerCfg.RerunGeneration {
		t.Fatalf("merger rerun completion did not refresh parent result: swarm=%s status=%s cfg=%#v", parent.SwarmStatus, parent.Status, parentCfg)
	}
}

func TestSwarmServiceOnChildCompletedIgnoresStaleMergerCompletion(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}); err != nil {
		t.Fatal(err)
	}
	children, err := repo.ListSwarmChildren(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reviewer, merger *models.Task
	for i := range children {
		child := children[i]
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		switch child.SwarmRole {
		case models.SwarmRoleWorker:
			cfg.CompletedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleReviewer:
			cfg.ReviewedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
			reviewer, _ = repo.GetByID(ctx, child.ID)
		case models.SwarmRoleMerger:
			cfg.MergedGeneration = 1
			cfg.RerunGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
			merger, _ = repo.GetByID(ctx, child.ID)
		}
	}
	if reviewer == nil || merger == nil {
		t.Fatal("reviewer/merger missing")
	}
	exec := &models.Execution{TaskID: merger.ID, Status: models.ExecRunning, PromptSent: merger.Prompt}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.Complete(ctx, exec.ID, models.ExecCompleted, "stale final summary", "", 1, 1); err != nil {
		t.Fatal(err)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.Generation = 2
	parentCfg.ReviewedGeneration = 1
	parentCfg.MergedGeneration = 1
	parent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "needs_review", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusPending); err != nil {
		t.Fatal(err)
	}

	if err := svc.OnChildCompleted(ctx, merger.ID); err != nil {
		t.Fatal(err)
	}
	updatedParent, err := repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("get parent: %v", err)
	}
	updatedCfg, _ := models.ParseSwarmConfig(updatedParent.SwarmConfig)
	if updatedParent.Status == models.StatusCompleted || updatedParent.SwarmStatus == "current" || updatedCfg.MergedGeneration != 1 {
		t.Fatalf("stale merger completion updated parent: status=%s swarm=%s cfg=%#v", updatedParent.Status, updatedParent.SwarmStatus, updatedCfg)
	}
	parentExecs, err := execRepo.ListByTask(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentExecs) != 0 {
		t.Fatalf("stale merger completion copied parent result: %#v", parentExecs)
	}
}

func TestSwarmServiceOnChildCompletedIgnoresStaleWorkerCompletion(t *testing.T) {
	ctx := context.Background()
	repo, svc, parent, children := newCompletedSwarmForServiceTest(t, ctx)
	worker := children[models.SwarmRoleWorker]
	reviewer := children[models.SwarmRoleReviewer]
	merger := children[models.SwarmRoleMerger]
	if worker == nil || reviewer == nil || merger == nil {
		t.Fatal("required children missing")
	}

	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.Generation = 2
	parentCfg.ReviewedGeneration = 1
	parentCfg.MergedGeneration = 1
	parent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "needs_review", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusRunning); err != nil {
		t.Fatal(err)
	}

	workerCfg, _ := models.ParseSwarmConfig(worker.SwarmConfig)
	workerCfg.RerunGeneration = 1
	workerCfg.CompletedGeneration = 1
	worker.SwarmConfig, _ = workerCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, worker.ID, worker.SwarmRole, "completed", worker.SwarmConfig, worker.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	reviewerCfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
	reviewerCfg.ReviewedGeneration = 1
	reviewer.SwarmConfig, _ = reviewerCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, reviewer.ID, reviewer.SwarmRole, "completed", reviewer.SwarmConfig, reviewer.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	mergerCfg, _ := models.ParseSwarmConfig(merger.SwarmConfig)
	mergerCfg.MergedGeneration = 1
	merger.SwarmConfig, _ = mergerCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, merger.ID, merger.SwarmRole, "completed", merger.SwarmConfig, merger.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, merger.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	if err := svc.OnChildCompleted(ctx, worker.ID); err != nil {
		t.Fatal(err)
	}

	updatedWorker, err := repo.GetByID(ctx, worker.ID)
	if err != nil || updatedWorker == nil {
		t.Fatalf("get worker: %v", err)
	}
	updatedWorkerCfg, _ := models.ParseSwarmConfig(updatedWorker.SwarmConfig)
	if updatedWorkerCfg.CompletedGeneration != 1 {
		t.Fatalf("stale worker completion advanced worker freshness: %#v", updatedWorkerCfg)
	}
	updatedReviewer, err := repo.GetByID(ctx, reviewer.ID)
	if err != nil || updatedReviewer == nil {
		t.Fatalf("get reviewer: %v", err)
	}
	if updatedReviewer.Status == models.StatusPending {
		t.Fatalf("stale worker completion started reviewer: %#v", updatedReviewer)
	}
	updatedParent, err := repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("get parent: %v", err)
	}
	if updatedParent.Status == models.StatusCompleted || updatedParent.SwarmStatus == "current" {
		t.Fatalf("stale worker completion finalized parent: status=%s swarm=%s", updatedParent.Status, updatedParent.SwarmStatus)
	}
}

func TestSwarmServiceOnChildCompletedIgnoresStaleReviewerCompletion(t *testing.T) {
	ctx := context.Background()
	repo, svc, parent, children := newCompletedSwarmForServiceTest(t, ctx)
	worker := children[models.SwarmRoleWorker]
	reviewer := children[models.SwarmRoleReviewer]
	merger := children[models.SwarmRoleMerger]
	if worker == nil || reviewer == nil || merger == nil {
		t.Fatal("required children missing")
	}

	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.Generation = 2
	parentCfg.ReviewedGeneration = 1
	parentCfg.MergedGeneration = 1
	parent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "needs_review", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusRunning); err != nil {
		t.Fatal(err)
	}

	workerCfg, _ := models.ParseSwarmConfig(worker.SwarmConfig)
	workerCfg.RerunGeneration = 2
	workerCfg.CompletedGeneration = 1
	worker.SwarmConfig, _ = workerCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, worker.ID, worker.SwarmRole, "pending", worker.SwarmConfig, worker.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, worker.ID, models.StatusPending); err != nil {
		t.Fatal(err)
	}

	reviewerCfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
	reviewerCfg.RerunGeneration = 1
	reviewerCfg.ReviewedGeneration = 1
	reviewer.SwarmConfig, _ = reviewerCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, reviewer.ID, reviewer.SwarmRole, "completed", reviewer.SwarmConfig, reviewer.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	mergerCfg, _ := models.ParseSwarmConfig(merger.SwarmConfig)
	mergerCfg.MergedGeneration = 1
	mergerCfg.RerunGeneration = 1
	merger.SwarmConfig, _ = mergerCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, merger.ID, merger.SwarmRole, "completed", merger.SwarmConfig, merger.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, merger.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}

	if err := svc.OnChildCompleted(ctx, reviewer.ID); err != nil {
		t.Fatal(err)
	}

	updatedReviewer, err := repo.GetByID(ctx, reviewer.ID)
	if err != nil || updatedReviewer == nil {
		t.Fatalf("get reviewer: %v", err)
	}
	updatedReviewerCfg, _ := models.ParseSwarmConfig(updatedReviewer.SwarmConfig)
	if updatedReviewerCfg.ReviewedGeneration != 1 {
		t.Fatalf("stale reviewer completion advanced review freshness: %#v", updatedReviewerCfg)
	}
	updatedMerger, err := repo.GetByID(ctx, merger.ID)
	if err != nil || updatedMerger == nil {
		t.Fatalf("get merger: %v", err)
	}
	if updatedMerger.Status == models.StatusPending {
		t.Fatalf("stale reviewer completion started merger: %#v", updatedMerger)
	}
	updatedParent, err := repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("get parent: %v", err)
	}
	if updatedParent.Status == models.StatusCompleted || updatedParent.SwarmStatus == "current" {
		t.Fatalf("stale reviewer completion finalized parent: status=%s swarm=%s", updatedParent.Status, updatedParent.SwarmStatus)
	}
}

func TestSwarmServiceRecomputeParentStatusDoesNotLetStaleMergerOverrideActiveWork(t *testing.T) {
	ctx := context.Background()
	repo, svc, parent, children := newCompletedSwarmForServiceTest(t, ctx)

	reviewer := children[models.SwarmRoleReviewer]
	if reviewer == nil {
		t.Fatal("reviewer missing")
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusPending); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecomputeParentStatus(ctx, parent.ID); err != nil {
		t.Fatalf("RecomputeParentStatus with pending reviewer: %v", err)
	}
	updatedParent, err := repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("get parent: %v", err)
	}
	if updatedParent.Status != models.StatusRunning {
		t.Fatalf("pending reviewer should keep parent running despite old completed merger, got %s", updatedParent.Status)
	}

	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	parentCfg, _ := models.ParseSwarmConfig(updatedParent.SwarmConfig)
	parentCfg.Generation = 2
	parentCfg.MergedGeneration = 1
	updatedParent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, updatedParent.ID, updatedParent.SwarmRole, "needs_integration", updatedParent.SwarmConfig, updatedParent.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecomputeParentStatus(ctx, parent.ID); err != nil {
		t.Fatalf("RecomputeParentStatus with stale integration: %v", err)
	}
	updatedParent, err = repo.GetByID(ctx, parent.ID)
	if err != nil || updatedParent == nil {
		t.Fatalf("get parent after stale integration: %v", err)
	}
	if updatedParent.Status == models.StatusCompleted {
		t.Fatalf("stale integrated_generation should not complete parent")
	}
}

func TestSwarmServiceHandleChildFollowupIsRoleSpecific(t *testing.T) {
	tests := []struct {
		name                  string
		role                  models.SwarmRole
		wantParentStatus      string
		wantGeneration        int
		wantReviewed          int
		wantIntegrated        int
		wantChildCompleted    int
		wantChildReviewed     int
		wantChildIntegrated   int
		wantChildRerunAtLeast int
	}{
		{name: "worker", role: models.SwarmRoleWorker, wantParentStatus: "needs_review", wantGeneration: 2, wantReviewed: 1, wantIntegrated: 1, wantChildCompleted: 1, wantChildRerunAtLeast: 2},
		{name: "reviewer", role: models.SwarmRoleReviewer, wantParentStatus: "needs_review", wantGeneration: 1, wantReviewed: 1, wantIntegrated: 0, wantChildReviewed: 0, wantChildRerunAtLeast: 1},
		{name: "merger", role: models.SwarmRoleMerger, wantParentStatus: "needs_integration", wantGeneration: 1, wantReviewed: 1, wantIntegrated: 0, wantChildIntegrated: 0, wantChildRerunAtLeast: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, svc, parent, children := newCompletedSwarmForServiceTest(t, ctx)
			child := children[tc.role]
			if child == nil {
				t.Fatalf("%s child missing", tc.role)
			}
			if err := svc.HandleChildFollowup(ctx, child.ID, "please adjust this role"); err != nil {
				t.Fatalf("HandleChildFollowup: %v", err)
			}
			updatedParent, err := repo.GetByID(ctx, parent.ID)
			if err != nil || updatedParent == nil {
				t.Fatalf("get parent: %v", err)
			}
			parentCfg, _ := models.ParseSwarmConfig(updatedParent.SwarmConfig)
			if updatedParent.SwarmStatus != tc.wantParentStatus || updatedParent.Status != models.StatusRunning || updatedParent.Category != models.CategoryActive {
				t.Fatalf("parent state after %s follow-up: swarm=%s status=%s category=%s", tc.role, updatedParent.SwarmStatus, updatedParent.Status, updatedParent.Category)
			}
			if parentCfg.Generation != tc.wantGeneration || parentCfg.ReviewedGeneration != tc.wantReviewed || parentCfg.MergedGeneration != tc.wantIntegrated {
				t.Fatalf("parent cfg after %s follow-up: %#v", tc.role, parentCfg)
			}
			updatedChild, err := repo.GetByID(ctx, child.ID)
			if err != nil || updatedChild == nil {
				t.Fatalf("get child: %v", err)
			}
			childCfg, _ := models.ParseSwarmConfig(updatedChild.SwarmConfig)
			if childCfg.CompletedGeneration != tc.wantChildCompleted || childCfg.ReviewedGeneration != tc.wantChildReviewed || childCfg.MergedGeneration != tc.wantChildIntegrated || childCfg.RerunGeneration < tc.wantChildRerunAtLeast {
				t.Fatalf("child cfg after %s follow-up: %#v", tc.role, childCfg)
			}
			if tc.role == models.SwarmRoleMerger {
				reviewer := children[models.SwarmRoleReviewer]
				updatedReviewer, _ := repo.GetByID(ctx, reviewer.ID)
				if updatedReviewer.Status != models.StatusCompleted {
					t.Fatalf("merger follow-up should not rerun reviewer: %#v", updatedReviewer)
				}
			}
		})
	}
}

func newCompletedSwarmForServiceTest(t *testing.T, ctx context.Context) (*repository.TaskRepo, *SwarmService, *models.Task, map[models.SwarmRole]*models.Task) {
	t.Helper()
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}); err != nil {
		t.Fatal(err)
	}
	children, err := repo.ListSwarmChildren(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	byRole := map[models.SwarmRole]*models.Task{}
	for i := range children {
		child := children[i]
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		switch child.SwarmRole {
		case models.SwarmRolePlanner:
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleWorker:
			cfg.CompletedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleReviewer:
			cfg.ReviewedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		case models.SwarmRoleMerger:
			cfg.MergedGeneration = 1
			child.SwarmConfig, _ = cfg.JSON()
			if err := repo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateStatus(ctx, child.ID, models.StatusCompleted); err != nil {
				t.Fatal(err)
			}
		}
		updated, err := repo.GetByID(ctx, child.ID)
		if err != nil || updated == nil {
			t.Fatalf("reload %s child: %v", child.SwarmRole, err)
		}
		byRole[child.SwarmRole] = updated
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.Generation = 1
	parentCfg.ReviewedGeneration = 1
	parentCfg.MergedGeneration = 1
	parent.SwarmConfig, _ = parentCfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "current", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateCategory(ctx, parent.ID, models.CategoryCompleted); err != nil {
		t.Fatal(err)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	return repo, svc, parent, byRole
}

func TestSwarmServiceRerunRoleRejectsActiveRoleExecutionWithoutRetargeting(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, MergerEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", MergerPrompt: "Integrate"}); err != nil {
		t.Fatal(err)
	}
	reviewer, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleReviewer)
	if err != nil || reviewer == nil {
		t.Fatalf("reviewer missing: %v", err)
	}
	cfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
	cfg.RerunGeneration = 1
	cfg.ReviewedGeneration = 0
	reviewer.SwarmConfig, _ = cfg.JSON()
	if err := repo.UpdateSwarmFields(ctx, reviewer.ID, reviewer.SwarmRole, reviewer.SwarmStatus, reviewer.SwarmConfig, reviewer.SwarmSequence); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusRunning); err != nil {
		t.Fatal(err)
	}
	exec := &models.Execution{TaskID: reviewer.ID, Status: models.ExecRunning, PromptSent: "active reviewer run"}
	if err := execRepo.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}
	parentBefore, _ := repo.GetByID(ctx, parent.ID)
	reviewerBefore, _ := repo.GetByID(ctx, reviewer.ID)

	_, err = svc.RerunRole(ctx, parent.ID, models.SwarmRoleReviewer)
	if !errors.Is(err, ErrSwarmRoleActive) {
		t.Fatalf("RerunRole error = %v, want ErrSwarmRoleActive", err)
	}
	parentAfter, _ := repo.GetByID(ctx, parent.ID)
	reviewerAfter, _ := repo.GetByID(ctx, reviewer.ID)
	if parentAfter.SwarmConfig != parentBefore.SwarmConfig || parentAfter.SwarmStatus != parentBefore.SwarmStatus || parentAfter.Status != parentBefore.Status || parentAfter.Category != parentBefore.Category {
		t.Fatalf("parent mutated despite rejected active rerun: before=%#v after=%#v", parentBefore, parentAfter)
	}
	if reviewerAfter.SwarmConfig != reviewerBefore.SwarmConfig || reviewerAfter.SwarmStatus != reviewerBefore.SwarmStatus || reviewerAfter.Status != reviewerBefore.Status {
		t.Fatalf("reviewer mutated despite rejected active rerun: before=%#v after=%#v", reviewerBefore, reviewerAfter)
	}
}
