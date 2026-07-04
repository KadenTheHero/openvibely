package service

import (
	"context"
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
	if counts[models.SwarmRoleWorker] != 1 || counts[models.SwarmRoleReviewer] != 1 || counts[models.SwarmRoleIntegrator] != 1 {
		t.Fatalf("duplicate planner completion created extra children: %#v", counts)
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

func TestTaskServiceUpdateCategoryNotifiesSwarmChildCancellation(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	taskSvc.SetSwarmService(svc)
	ctx := context.Background()
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Swarm parent", Prompt: "Build result", Category: models.CategoryActive, MaxWorkers: 1, WorkerIsolation: "worktree", ReviewerEnabled: true, IntegratorEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if err != nil || planner == nil {
		t.Fatalf("planner missing: %v", err)
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{
		Workers:          []PlannerWorker{{Title: "API worker", Prompt: "Update API", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", WriteScope: []string{"internal/service"}, Required: true}},
		ReviewerPrompt:   "Review worker",
		IntegratorPrompt: "Integrate worker",
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

func TestSwarmServiceChildCancellationSetsRoleSpecificParentState(t *testing.T) {
	roles := []struct {
		name       string
		role       models.SwarmRole
		wantStatus string
	}{
		{name: "worker", role: models.SwarmRoleWorker, wantStatus: "needs_coordination"},
		{name: "reviewer", role: models.SwarmRoleReviewer, wantStatus: "needs_review"},
		{name: "integrator", role: models.SwarmRoleIntegrator, wantStatus: "needs_integration"},
	}
	for _, tc := range roles {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.NewTestDB(t)
			repo := repository.NewTaskRepo(db, nil)
			taskSvc := NewTaskService(repo, nil, nil)
			svc := NewSwarmService(taskSvc, repo, nil, nil)
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
	parent, err := svc.CreateSwarmTask(context.Background(), CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 3, ReviewerEnabled: true, IntegratorEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	initialOutput := PlannerOutput{Workers: []PlannerWorker{{Title: "Backend worker", Prompt: "Do backend", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}, {Title: "Frontend worker", Prompt: "Do frontend", WorkerKind: "frontend", Ownership: []string{"web/templates"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", IntegratorPrompt: "Integrate"}
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
		case models.SwarmRoleIntegrator:
			cfg.IntegratedGeneration = 1
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
	parentCfg.IntegratedGeneration = 1
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
	followupOutput := PlannerOutput{Workers: []PlannerWorker{{TaskID: backend.ID, Title: "Backend worker", Prompt: "Update backend only", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review backend update", IntegratorPrompt: "Integrate backend update"}
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
	integrator, _ := repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleIntegrator)
	if reviewer == nil || reviewer.Status != models.StatusBlocked || integrator == nil || integrator.Status != models.StatusBlocked {
		t.Fatalf("reviewer/integrator should wait for affected worker: reviewer=%#v integrator=%#v", reviewer, integrator)
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
	integrator, _ = repo.FindSwarmChildByRole(context.Background(), parent.ID, models.SwarmRoleIntegrator)
	if integrator == nil || integrator.Status != models.StatusPending {
		t.Fatalf("integrator not rerun after reviewer completed: %#v", integrator)
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

func TestSwarmServiceRerunReviewerStartsIntegratorAfterReviewerCompletes(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewTaskRepo(db, nil)
	taskSvc := NewTaskService(repo, nil, nil)
	svc := NewSwarmService(taskSvc, repo, nil, nil)
	ctx := context.Background()

	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, IntegratorEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", IntegratorPrompt: "Integrate"}); err != nil {
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
		case models.SwarmRoleIntegrator:
			cfg.IntegratedGeneration = 1
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
	parentCfg.IntegratedGeneration = 1
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
	if rerunReviewer == nil || rerunReviewer.Status != models.StatusPending {
		t.Fatalf("reviewer not queued for rerun: %#v", rerunReviewer)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	parentCfg, _ = models.ParseSwarmConfig(parent.SwarmConfig)
	if parent.SwarmStatus != "needs_review" || parentCfg.IntegratedGeneration >= parentCfg.Generation {
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
	integrator, err := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRoleIntegrator)
	if err != nil || integrator == nil {
		t.Fatalf("integrator missing: %v", err)
	}
	if integrator.Status != models.StatusPending {
		t.Fatalf("integrator not rerun after reviewer retry completed: %#v", integrator)
	}
	if err := repo.UpdateStatus(ctx, parent.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateCategory(ctx, parent.ID, models.CategoryCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, integrator.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	rerunIntegrator, err := svc.RerunRole(ctx, parent.ID, models.SwarmRoleIntegrator)
	if err != nil {
		t.Fatalf("RerunRole integrator: %v", err)
	}
	if rerunIntegrator == nil || rerunIntegrator.Status != models.StatusPending {
		t.Fatalf("integrator not queued for rerun: %#v", rerunIntegrator)
	}
	parent, _ = repo.GetByID(ctx, parent.ID)
	if parent.SwarmStatus != "needs_integration" || parent.Status != models.StatusRunning || parent.Category != models.CategoryActive {
		t.Fatalf("parent not reactivated for integrator rerun: swarm=%s status=%s category=%s", parent.SwarmStatus, parent.Status, parent.Category)
	}
}

func TestSwarmServiceRecomputeParentStatusDoesNotLetStaleIntegratorOverrideActiveWork(t *testing.T) {
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
		t.Fatalf("pending reviewer should keep parent running despite old completed integrator, got %s", updatedParent.Status)
	}

	if err := repo.UpdateStatus(ctx, reviewer.ID, models.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	parentCfg, _ := models.ParseSwarmConfig(updatedParent.SwarmConfig)
	parentCfg.Generation = 2
	parentCfg.IntegratedGeneration = 1
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
		{name: "integrator", role: models.SwarmRoleIntegrator, wantParentStatus: "needs_integration", wantGeneration: 1, wantReviewed: 1, wantIntegrated: 0, wantChildIntegrated: 0, wantChildRerunAtLeast: 1},
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
			if parentCfg.Generation != tc.wantGeneration || parentCfg.ReviewedGeneration != tc.wantReviewed || parentCfg.IntegratedGeneration != tc.wantIntegrated {
				t.Fatalf("parent cfg after %s follow-up: %#v", tc.role, parentCfg)
			}
			updatedChild, err := repo.GetByID(ctx, child.ID)
			if err != nil || updatedChild == nil {
				t.Fatalf("get child: %v", err)
			}
			childCfg, _ := models.ParseSwarmConfig(updatedChild.SwarmConfig)
			if childCfg.CompletedGeneration != tc.wantChildCompleted || childCfg.ReviewedGeneration != tc.wantChildReviewed || childCfg.IntegratedGeneration != tc.wantChildIntegrated || childCfg.RerunGeneration < tc.wantChildRerunAtLeast {
				t.Fatalf("child cfg after %s follow-up: %#v", tc.role, childCfg)
			}
			if tc.role == models.SwarmRoleIntegrator {
				reviewer := children[models.SwarmRoleReviewer]
				updatedReviewer, _ := repo.GetByID(ctx, reviewer.ID)
				if updatedReviewer.Status != models.StatusCompleted {
					t.Fatalf("integrator follow-up should not rerun reviewer: %#v", updatedReviewer)
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
	parent, err := svc.CreateSwarmTask(ctx, CreateSwarmTaskRequest{ProjectID: "default", Title: "Build export", Prompt: "Build export", MaxWorkers: 1, ReviewerEnabled: true, IntegratorEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := repo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner)
	if planner == nil {
		t.Fatal("planner missing")
	}
	if err := svc.ApplyPlannerOutput(ctx, planner.ID, PlannerOutput{Workers: []PlannerWorker{{Title: "Worker", Prompt: "Do work", WorkerKind: "backend", Ownership: []string{"internal/service"}, Isolation: "worktree", Required: true}}, ReviewerPrompt: "Review", IntegratorPrompt: "Integrate"}); err != nil {
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
		case models.SwarmRoleIntegrator:
			cfg.IntegratedGeneration = 1
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
	parentCfg.IntegratedGeneration = 1
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
