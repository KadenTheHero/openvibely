package service

import (
	"context"
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

	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 3, WorkerIsolation: "worktree", ReviewerEnabled: true, IntegratorEnabled: true})
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

	output := PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}}, ReviewerPrompt: "Review workers", IntegratorPrompt: "Integrate workers"}
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
	if counts[models.SwarmRoleWorker] != 1 || counts[models.SwarmRoleReviewer] != 1 || counts[models.SwarmRoleIntegrator] != 1 {
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

func TestSwarmServiceAppliesPlannerOutputOnPlannerCompletion(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, IntegratorEnabled: true})
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
	plannerJSON := `{"workers":[{"title":"Backend worker","prompt":"Do backend","worker_kind":"backend","ownership":["internal/service"],"isolation":"worktree","write_scope":["internal/service"],"required":true}],"reviewer_prompt":"Review workers","integrator_prompt":"Integrate workers"}`
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
	if counts[models.SwarmRoleWorker] != 1 || counts[models.SwarmRoleReviewer] != 1 || counts[models.SwarmRoleIntegrator] != 1 {
		t.Fatalf("planner completion did not create swarm children: %#v", counts)
	}
}

func TestSwarmServiceInvalidPlannerExecutionBlocksParent(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, IntegratorEnabled: true})
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

func TestSwarmServiceIntegratorCompletionPersistsParentResult(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	execRepo := repository.NewExecutionRepo(db)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, execRepo, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, IntegratorEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	output := PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", IntegratorPrompt: "Integrate"}
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
	integrator, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleIntegrator)
	if integrator == nil {
		t.Fatal("integrator missing")
	}
	exec := &models.Execution{TaskID: integrator.ID, Status: models.ExecRunning, PromptSent: integrator.Prompt}
	if err := execRepo.Create(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.UpdateDiffOutput(context.Background(), exec.ID, "diff --git a/final.go b/final.go"); err != nil {
		t.Fatal(err)
	}
	if err := execRepo.Complete(context.Background(), exec.ID, models.ExecCompleted, "Final integrated summary", "", 12, 34); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), integrator.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), integrator.ID); err != nil {
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
	if err := svc.OnChildCompleted(context.Background(), integrator.ID); err != nil {
		t.Fatal(err)
	}
	parentExecs, err = execRepo.ListByTask(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentExecs) != 1 {
		t.Fatalf("integrator completion should be idempotent, got %d parent executions", len(parentExecs))
	}
}

func TestSwarmServiceStartsReviewerAndIntegratorOnce(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 2, ReviewerEnabled: true, IntegratorEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	output := PlannerOutput{Workers: []PlannerWorker{{Title: "Worker A", Prompt: "A", WorkerKind: "backend", Ownership: []string{"a"}, Isolation: "worktree", Required: true}, {Title: "Worker B", Prompt: "B", WorkerKind: "frontend", Ownership: []string{"b"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", IntegratorPrompt: "Integrate"}
	if err := svc.ApplyPlannerOutput(context.Background(), planner.ID, output); err != nil {
		t.Fatal(err)
	}
	children, _ := repo.ListSwarmChildren(context.Background(), parent.ID)
	workerID := ""
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
	}
	if workerID == "" {
		t.Fatal("worker missing")
	}
	if err := svc.OnChildCompleted(context.Background(), workerID); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnChildCompleted(context.Background(), workerID); err != nil {
		t.Fatal(err)
	}
	reviewer, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleReviewer)
	if reviewer == nil || reviewer.Status != models.StatusPending {
		t.Fatalf("reviewer not pending once: %#v", reviewer)
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
	integrator, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleIntegrator)
	if integrator == nil || integrator.Status != models.StatusPending {
		t.Fatalf("integrator not pending once: %#v", integrator)
	}
}
