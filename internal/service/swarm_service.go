package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

type CreateSwarmTaskRequest struct {
	ProjectID         string
	Title             string
	Prompt            string
	Goal              string
	Category          models.TaskCategory
	Priority          int
	AgentID           *string
	AgentDefinitionID *string
	Tag               models.TaskTag
	MaxWorkers        int
	WorkerIsolation   string
	ReviewerEnabled   bool
	IntegratorEnabled bool
	StartImmediately  bool
	MergeTargetBranch string
}

type PlannerOutput struct {
	Workers          []PlannerWorker `json:"workers"`
	ReviewerPrompt   string          `json:"reviewer_prompt"`
	IntegratorPrompt string          `json:"integrator_prompt"`
	Notes            string          `json:"notes"`
}

type PlannerWorker struct {
	Title        string   `json:"title"`
	Prompt       string   `json:"prompt"`
	WorkerKind   string   `json:"worker_kind"`
	Ownership    []string `json:"ownership"`
	Isolation    string   `json:"isolation"`
	WriteScope   []string `json:"write_scope"`
	ReadScope    []string `json:"read_scope"`
	Required     bool     `json:"required"`
	ReadOnly     bool     `json:"read_only"`
	AllowOverlap bool     `json:"allow_overlap"`
}

type SwarmService struct {
	taskSvc   *TaskService
	taskRepo  *repository.TaskRepo
	execRepo  *repository.ExecutionRepo
	workerSvc *WorkerService
}

func NewSwarmService(taskSvc *TaskService, taskRepo *repository.TaskRepo, execRepo *repository.ExecutionRepo, workerSvc *WorkerService) *SwarmService {
	return &SwarmService{taskSvc: taskSvc, taskRepo: taskRepo, execRepo: execRepo, workerSvc: workerSvc}
}

func (s *SwarmService) CreateSwarmTask(ctx context.Context, req CreateSwarmTaskRequest) (*models.Task, error) {
	if s == nil || s.taskSvc == nil || s.taskRepo == nil {
		return nil, errors.New("swarm service unavailable")
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("project_id, title, and prompt are required")
	}
	if req.Category == "" {
		req.Category = models.CategoryActive
	}
	if req.Priority == 0 {
		req.Priority = 2
	}
	if req.MaxWorkers <= 0 {
		req.MaxWorkers = 3
	}
	if req.MaxWorkers > 8 {
		req.MaxWorkers = 8
	}
	if req.WorkerIsolation == "" {
		req.WorkerIsolation = "worktree"
	}
	cfg := models.SwarmConfig{
		Mode:                             "autonomous",
		DefaultWorkerIsolation:           req.WorkerIsolation,
		MaxWorkers:                       req.MaxWorkers,
		ReviewerEnabled:                  req.ReviewerEnabled,
		IntegratorEnabled:                req.IntegratorEnabled,
		RerunReviewerAfterWorkerFollowup: true,
		RerunIntegratorAfterReviewer:     true,
		Generation:                       1,
		MergeStrategy:                    "patch_apply",
	}
	cfgJSON, err := cfg.JSON()
	if err != nil {
		return nil, err
	}
	parent := &models.Task{
		ProjectID:         req.ProjectID,
		Title:             req.Title,
		Prompt:            req.Prompt,
		Category:          req.Category,
		Priority:          req.Priority,
		Status:            models.StatusBlocked,
		AgentID:           req.AgentID,
		AgentDefinitionID: req.AgentDefinitionID,
		Tag:               req.Tag,
		MergeTargetBranch: req.MergeTargetBranch,
		SwarmRole:         models.SwarmRoleParent,
		SwarmStatus:       "planning",
		SwarmConfig:       cfgJSON,
	}
	if err := s.taskSvc.CreateWithGoal(ctx, parent, req.Goal); err != nil {
		return nil, err
	}
	if err := s.ensureIntegrationWorktreeMetadata(ctx, parent); err != nil {
		return nil, err
	}
	if err := s.StartPlanner(ctx, parent.ID); err != nil {
		return nil, err
	}
	return parent, nil
}

func (s *SwarmService) StartPlanner(ctx context.Context, parentTaskID string) error {
	parent, err := s.taskRepo.GetByID(ctx, parentTaskID)
	if err != nil || parent == nil {
		return fmt.Errorf("loading swarm parent: %w", err)
	}
	if existing, err := s.taskRepo.FindSwarmChildByRole(ctx, parent.ID, models.SwarmRolePlanner); err != nil {
		return err
	} else if existing != nil {
		return s.submitIfRunnable(ctx, existing)
	}
	prompt := plannerPrompt(parent.Prompt, maxWorkers(parent))
	child := &models.Task{
		ProjectID:         parent.ProjectID,
		Title:             parent.Title + " · Planner",
		Prompt:            prompt,
		Category:          models.CategoryActive,
		Priority:          parent.Priority,
		Status:            models.StatusPending,
		AgentID:           parent.AgentID,
		AgentDefinitionID: parent.AgentDefinitionID,
		ParentTaskID:      &parent.ID,
		SwarmRole:         models.SwarmRolePlanner,
		SwarmStatus:       "planning",
		SwarmConfig:       `{"isolation":"read_only","rerun_generation":1,"required":true}`,
		SwarmSequence:     0,
	}
	if err := s.taskSvc.Create(ctx, child); err != nil {
		return err
	}
	return nil
}

func (s *SwarmService) ApplyPlannerOutput(ctx context.Context, plannerTaskID string, output PlannerOutput) error {
	planner, err := s.taskRepo.GetByID(ctx, plannerTaskID)
	if err != nil || planner == nil {
		return fmt.Errorf("loading planner task: %w", err)
	}
	if planner.ParentTaskID == nil {
		return errors.New("planner has no swarm parent")
	}
	parent, err := s.taskRepo.GetByID(ctx, *planner.ParentTaskID)
	if err != nil || parent == nil {
		return fmt.Errorf("loading swarm parent: %w", err)
	}
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	if err := validatePlannerOutput(output, parentCfg); err != nil {
		planner.SwarmStatus = "invalid_plan"
		cfg, _ := models.ParseSwarmConfig(planner.SwarmConfig)
		cfg.LastError = err.Error()
		planner.SwarmConfig, _ = cfg.JSON()
		_ = s.taskRepo.UpdateSwarmFields(ctx, planner.ID, planner.SwarmRole, planner.SwarmStatus, planner.SwarmConfig, planner.SwarmSequence)
		_ = s.taskRepo.UpdateStatus(ctx, planner.ID, models.StatusFailed)
		parent.SwarmStatus = "blocked"
		parentCfg.LastError = err.Error()
		parent.SwarmConfig, _ = parentCfg.JSON()
		_ = s.taskRepo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, parent.SwarmStatus, parent.SwarmConfig, parent.SwarmSequence)
		_ = s.taskRepo.UpdateStatus(ctx, parent.ID, models.StatusBlocked)
		return err
	}
	existing, err := s.taskRepo.ListSwarmChildren(ctx, parent.ID)
	if err != nil {
		return err
	}
	for _, child := range existing {
		if child.SwarmRole == models.SwarmRoleWorker || child.SwarmRole == models.SwarmRoleReviewer || child.SwarmRole == models.SwarmRoleIntegrator {
			return nil
		}
	}
	parentCfg.ReviewerPrompt = output.ReviewerPrompt
	parentCfg.IntegratorPrompt = output.IntegratorPrompt
	parentCfg.PlannerNotes = output.Notes
	parentCfgJSON, _ := parentCfg.JSON()
	if err := s.taskRepo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "workers_running", parentCfgJSON, parent.SwarmSequence); err != nil {
		return err
	}
	for i, worker := range output.Workers {
		cfg := models.SwarmConfig{WorkerKind: worker.WorkerKind, Ownership: worker.Ownership, Isolation: workerIsolation(worker, parentCfg), DependsOnRoles: []string{"planner"}, RerunGeneration: parentCfg.Generation, Required: worker.Required || !worker.ReadOnly, WriteScope: worker.WriteScope, ReadScope: worker.ReadScope}
		cfgJSON, _ := cfg.JSON()
		child := &models.Task{ProjectID: parent.ProjectID, Title: worker.Title, Prompt: workerPrompt(parent.Prompt, worker, ""), Category: models.CategoryActive, Priority: parent.Priority, Status: models.StatusPending, AgentID: parent.AgentID, AgentDefinitionID: parent.AgentDefinitionID, ParentTaskID: &parent.ID, SwarmRole: models.SwarmRoleWorker, SwarmStatus: "running", SwarmConfig: cfgJSON, SwarmSequence: 10 + i}
		if cfg.Isolation == "worktree" {
			child.WorktreeBranch = swarmBranch(parent.Title, worker.WorkerKind, "pending")
			child.WorktreePath = filepath.Join(".worktrees", child.WorktreeBranch)
		}
		if err := s.taskSvc.Create(ctx, child); err != nil {
			return err
		}
		if cfg.Isolation == "worktree" {
			branch := swarmBranch(parent.Title, worker.WorkerKind, child.ID)
			path := filepath.Join(".worktrees", branch)
			_ = s.taskRepo.UpdateWorktreeInfo(ctx, child.ID, path, branch)
		}
	}
	if parentCfg.ReviewerEnabled {
		cfg := models.SwarmConfig{Isolation: "read_only", DependsOnRoles: []string{"worker"}, RerunGeneration: parentCfg.Generation, Required: true}
		cfgJSON, _ := cfg.JSON()
		child := &models.Task{ProjectID: parent.ProjectID, Title: parent.Title + " · Reviewer", Prompt: reviewerPrompt(parent.Prompt, output.ReviewerPrompt), Category: models.CategoryActive, Priority: parent.Priority, Status: models.StatusBlocked, AgentID: parent.AgentID, AgentDefinitionID: parent.AgentDefinitionID, ParentTaskID: &parent.ID, SwarmRole: models.SwarmRoleReviewer, SwarmStatus: "pending", SwarmConfig: cfgJSON, SwarmSequence: 900}
		if err := s.taskSvc.Create(ctx, child); err != nil {
			return err
		}
	}
	if parentCfg.IntegratorEnabled {
		cfg := models.SwarmConfig{Isolation: "worktree", DependsOnRoles: []string{"reviewer"}, RerunGeneration: parentCfg.Generation, Required: true}
		cfgJSON, _ := cfg.JSON()
		child := &models.Task{ProjectID: parent.ProjectID, Title: parent.Title + " · Integrator", Prompt: integratorPrompt(parent.Prompt, output.IntegratorPrompt, parent.WorktreePath), Category: models.CategoryActive, Priority: parent.Priority, Status: models.StatusBlocked, AgentID: parent.AgentID, AgentDefinitionID: parent.AgentDefinitionID, ParentTaskID: &parent.ID, SwarmRole: models.SwarmRoleIntegrator, SwarmStatus: "pending", SwarmConfig: cfgJSON, SwarmSequence: 1000, WorktreePath: parent.WorktreePath, WorktreeBranch: parent.WorktreeBranch}
		if err := s.taskSvc.Create(ctx, child); err != nil {
			return err
		}
	}
	_ = s.taskRepo.UpdateStatus(ctx, planner.ID, models.StatusCompleted)
	return s.StartReadyWorkers(ctx, parent.ID)
}

func (s *SwarmService) StartReadyWorkers(ctx context.Context, parentTaskID string) error {
	children, err := s.taskRepo.ListSwarmChildren(ctx, parentTaskID)
	if err != nil {
		return err
	}
	for i := range children {
		if children[i].SwarmRole == models.SwarmRoleWorker && children[i].Status == models.StatusPending && s.workerSvc != nil {
			s.workerSvc.Submit(children[i])
		}
	}
	return nil
}

func (s *SwarmService) OnChildCompleted(ctx context.Context, childTaskID string) error {
	child, err := s.taskRepo.GetByID(ctx, childTaskID)
	if err != nil || child == nil || child.ParentTaskID == nil || !models.IsSwarmChildRole(child.SwarmRole) {
		return err
	}
	if child.SwarmRole == models.SwarmRolePlanner && child.Status == models.StatusCompleted {
		return s.applyCompletedPlannerExecution(ctx, child)
	}
	parent, err := s.taskRepo.GetByID(ctx, *child.ParentTaskID)
	if err != nil || parent == nil {
		return err
	}
	children, err := s.taskRepo.ListSwarmChildren(ctx, parent.ID)
	if err != nil {
		return err
	}
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	if child.SwarmRole == models.SwarmRoleWorker && child.Status == models.StatusCompleted {
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		if cfg.CompletedGeneration < parentCfg.Generation {
			cfg.CompletedGeneration = parentCfg.Generation
			child.SwarmConfig, _ = cfg.JSON()
			_ = s.taskRepo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence)
		}
	}
	if allRequiredWorkersCompleted(children, parentCfg.Generation) && parentCfg.ReviewerEnabled {
		if reviewer := findChild(children, models.SwarmRoleReviewer); reviewer != nil {
			revCfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
			if revCfg.ReviewedGeneration < parentCfg.Generation && reviewer.Status != models.StatusRunning && reviewer.Status != models.StatusPending && child.Status != models.StatusFailed {
				revCfg.RerunGeneration = parentCfg.Generation
				reviewer.SwarmConfig, _ = revCfg.JSON()
				_ = s.taskRepo.UpdateSwarmFields(ctx, reviewer.ID, reviewer.SwarmRole, "ready", reviewer.SwarmConfig, reviewer.SwarmSequence)
				_ = s.taskRepo.UpdateStatus(ctx, reviewer.ID, models.StatusPending)
				if s.workerSvc != nil {
					s.workerSvc.Submit(*reviewer)
				}
			}
		}
	}
	if child.SwarmRole == models.SwarmRoleReviewer && child.Status == models.StatusCompleted {
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		if cfg.ReviewedGeneration < parentCfg.Generation {
			cfg.ReviewedGeneration = parentCfg.Generation
			child.SwarmConfig, _ = cfg.JSON()
			_ = s.taskRepo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence)
		}
	}
	children, _ = s.taskRepo.ListSwarmChildren(ctx, parent.ID)
	if reviewer := findChild(children, models.SwarmRoleReviewer); reviewer != nil && parentCfg.IntegratorEnabled {
		revCfg, _ := models.ParseSwarmConfig(reviewer.SwarmConfig)
		if revCfg.ReviewedGeneration > parentCfg.IntegratedGeneration {
			if integrator := findChild(children, models.SwarmRoleIntegrator); integrator != nil && integrator.Status != models.StatusRunning && integrator.Status != models.StatusPending {
				intCfg, _ := models.ParseSwarmConfig(integrator.SwarmConfig)
				intCfg.RerunGeneration = revCfg.ReviewedGeneration
				integrator.SwarmConfig, _ = intCfg.JSON()
				_ = s.taskRepo.UpdateSwarmFields(ctx, integrator.ID, integrator.SwarmRole, "ready", integrator.SwarmConfig, integrator.SwarmSequence)
				_ = s.taskRepo.UpdateStatus(ctx, integrator.ID, models.StatusPending)
				if s.workerSvc != nil {
					s.workerSvc.Submit(*integrator)
				}
			}
		}
	}
	if child.SwarmRole == models.SwarmRoleIntegrator && child.Status == models.StatusCompleted {
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		if cfg.IntegratedGeneration < parentCfg.Generation {
			cfg.IntegratedGeneration = parentCfg.Generation
			child.SwarmConfig, _ = cfg.JSON()
			_ = s.taskRepo.UpdateSwarmFields(ctx, child.ID, child.SwarmRole, child.SwarmStatus, child.SwarmConfig, child.SwarmSequence)
		}
		parentCfg.IntegratedGeneration = max(parentCfg.IntegratedGeneration, parentCfg.Generation)
		parent.SwarmConfig, _ = parentCfg.JSON()
		_ = s.taskRepo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "current", parent.SwarmConfig, parent.SwarmSequence)
		_ = s.taskRepo.UpdateStatus(ctx, parent.ID, models.StatusCompleted)
		_ = s.taskRepo.UpdateCategory(ctx, parent.ID, models.CategoryCompleted)
	}
	return s.RecomputeParentStatus(ctx, parent.ID)
}

func (s *SwarmService) HandleParentFollowup(ctx context.Context, parentTaskID string, message string) error {
	parent, err := s.taskRepo.GetByID(ctx, parentTaskID)
	if err != nil || parent == nil {
		return err
	}
	cfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	cfg.Generation++
	if cfg.Generation == 0 {
		cfg.Generation = 1
	}
	parent.SwarmConfig, _ = cfg.JSON()
	if err := s.taskRepo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "needs_coordination", parent.SwarmConfig, parent.SwarmSequence); err != nil {
		return err
	}
	return s.StartPlanner(ctx, parent.ID)
}

func (s *SwarmService) HandleChildFollowup(ctx context.Context, childTaskID string, message string) error {
	child, err := s.taskRepo.GetByID(ctx, childTaskID)
	if err != nil || child == nil || child.ParentTaskID == nil {
		return err
	}
	parent, err := s.taskRepo.GetByID(ctx, *child.ParentTaskID)
	if err != nil || parent == nil {
		return err
	}
	cfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	cfg.Generation++
	parent.SwarmConfig, _ = cfg.JSON()
	return s.taskRepo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "needs_review", parent.SwarmConfig, parent.SwarmSequence)
}

func (s *SwarmService) RecomputeParentStatus(ctx context.Context, parentTaskID string) error {
	parent, err := s.taskRepo.GetByID(ctx, parentTaskID)
	if err != nil || parent == nil || parent.SwarmRole != models.SwarmRoleParent {
		return err
	}
	children, err := s.taskRepo.ListSwarmChildren(ctx, parent.ID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return nil
	}
	status := models.StatusPending
	for _, child := range children {
		if child.Status == models.StatusRunning || child.Status == models.StatusPending || child.Status == models.StatusQueued {
			status = models.StatusRunning
			break
		}
		if child.Status == models.StatusFailed {
			status = models.StatusFailed
		}
		if child.Status == models.StatusBlocked && status != models.StatusFailed {
			status = models.StatusBlocked
		}
	}
	if integrator := findChild(children, models.SwarmRoleIntegrator); integrator != nil && integrator.Status == models.StatusCompleted {
		status = models.StatusCompleted
	}
	if parent.Status != status {
		return s.taskRepo.UpdateStatus(ctx, parent.ID, status)
	}
	return nil
}

func (s *SwarmService) CancelSwarm(ctx context.Context, parentTaskID string) error {
	children, err := s.taskRepo.ListSwarmChildren(ctx, parentTaskID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Status == models.StatusRunning && s.workerSvc != nil {
			s.workerSvc.CancelRunningTask(child.ID)
		}
		if child.Status == models.StatusPending || child.Status == models.StatusQueued || child.Status == models.StatusRunning || child.Status == models.StatusBlocked {
			_ = s.taskRepo.UpdateStatus(ctx, child.ID, models.StatusCancelled)
		}
	}
	if parent, err := s.taskRepo.GetByID(ctx, parentTaskID); err == nil && parent != nil {
		return s.taskRepo.UpdateStatus(ctx, parent.ID, models.StatusCancelled)
	}
	return nil
}

func (s *SwarmService) applyCompletedPlannerExecution(ctx context.Context, planner *models.Task) error {
	children, err := s.taskRepo.ListSwarmChildren(ctx, *planner.ParentTaskID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.SwarmRole == models.SwarmRoleWorker || child.SwarmRole == models.SwarmRoleReviewer || child.SwarmRole == models.SwarmRoleIntegrator {
			return s.RecomputeParentStatus(ctx, *planner.ParentTaskID)
		}
	}
	if s.execRepo == nil {
		return errors.New("swarm planner output unavailable: execution repository is not configured")
	}
	exec, err := s.execRepo.GetLatestCompletedByTask(ctx, planner.ID)
	if err != nil {
		return err
	}
	if exec == nil || strings.TrimSpace(exec.Output) == "" {
		return s.markPlannerOutputInvalid(ctx, planner, "planner completed without JSON output")
	}
	output, err := ParsePlannerOutputJSON(exec.Output)
	if err != nil {
		return s.markPlannerOutputInvalid(ctx, planner, fmt.Sprintf("invalid planner JSON: %v", err))
	}
	return s.ApplyPlannerOutput(ctx, planner.ID, output)
}

func (s *SwarmService) markPlannerOutputInvalid(ctx context.Context, planner *models.Task, message string) error {
	if planner == nil || planner.ParentTaskID == nil {
		return errors.New(message)
	}
	cfg, _ := models.ParseSwarmConfig(planner.SwarmConfig)
	cfg.LastError = message
	planner.SwarmConfig, _ = cfg.JSON()
	_ = s.taskRepo.UpdateSwarmFields(ctx, planner.ID, planner.SwarmRole, "invalid_plan", planner.SwarmConfig, planner.SwarmSequence)
	_ = s.taskRepo.UpdateStatus(ctx, planner.ID, models.StatusFailed)
	parent, err := s.taskRepo.GetByID(ctx, *planner.ParentTaskID)
	if err != nil || parent == nil {
		return errors.New(message)
	}
	parentCfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	parentCfg.LastError = message
	parent.SwarmConfig, _ = parentCfg.JSON()
	_ = s.taskRepo.UpdateSwarmFields(ctx, parent.ID, parent.SwarmRole, "blocked", parent.SwarmConfig, parent.SwarmSequence)
	_ = s.taskRepo.UpdateStatus(ctx, parent.ID, models.StatusBlocked)
	return errors.New(message)
}

func (s *SwarmService) submitIfRunnable(ctx context.Context, task *models.Task) error {
	if task.Status == models.StatusBlocked || task.Status == models.StatusRunning || task.Status == models.StatusQueued {
		return nil
	}
	if task.Status != models.StatusPending {
		if err := s.taskRepo.UpdateStatus(ctx, task.ID, models.StatusPending); err != nil {
			return err
		}
		task.Status = models.StatusPending
	}
	if s.workerSvc != nil {
		s.workerSvc.Submit(*task)
	}
	return nil
}

func (s *SwarmService) ensureIntegrationWorktreeMetadata(ctx context.Context, parent *models.Task) error {
	if parent.WorktreePath != "" && parent.WorktreeBranch != "" {
		return nil
	}
	branch := swarmBranch(parent.Title, "integration", parent.ID)
	path := filepath.Join(".worktrees", branch)
	parent.WorktreePath = path
	parent.WorktreeBranch = branch
	return s.taskRepo.UpdateWorktreeInfo(ctx, parent.ID, path, branch)
}

func validatePlannerOutput(output PlannerOutput, parentCfg models.SwarmConfig) error {
	if len(output.Workers) == 0 {
		return errors.New("planner output must include at least one worker")
	}
	if parentCfg.MaxWorkers > 0 && len(output.Workers) > parentCfg.MaxWorkers {
		return fmt.Errorf("planner output has %d workers, max is %d", len(output.Workers), parentCfg.MaxWorkers)
	}
	if parentCfg.ReviewerEnabled && strings.TrimSpace(output.ReviewerPrompt) == "" {
		return errors.New("reviewer_prompt is required")
	}
	if parentCfg.IntegratorEnabled && strings.TrimSpace(output.IntegratorPrompt) == "" {
		return errors.New("integrator_prompt is required")
	}
	writeOwners := map[string]int{}
	for i, w := range output.Workers {
		if strings.TrimSpace(w.Title) == "" || strings.TrimSpace(w.Prompt) == "" || strings.TrimSpace(w.WorkerKind) == "" || len(w.Ownership) == 0 {
			return fmt.Errorf("worker %d must include title, prompt, worker_kind, and ownership", i+1)
		}
		iso := workerIsolation(w, parentCfg)
		if iso != "read_only" && iso != "worktree" && iso != "shared" {
			return fmt.Errorf("worker %q has invalid isolation %q", w.Title, iso)
		}
		if iso == "shared" && parentCfg.DefaultWorkerIsolation != "shared" {
			return fmt.Errorf("worker %q requested shared isolation but parent does not allow it", w.Title)
		}
		if iso != "read_only" && iso != "worktree" {
			return fmt.Errorf("code-changing worker %q must use worktree isolation", w.Title)
		}
		if !w.AllowOverlap {
			for _, scope := range w.WriteScope {
				scope = strings.TrimSpace(scope)
				if scope == "" {
					continue
				}
				if prev, ok := writeOwners[scope]; ok && !w.ReadOnly {
					return fmt.Errorf("worker %d write_scope overlaps worker %d at %s", i+1, prev+1, scope)
				}
				writeOwners[scope] = i
			}
		}
	}
	return nil
}

func ParsePlannerOutputJSON(raw string) (PlannerOutput, error) {
	var output PlannerOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &output); err != nil {
		return output, err
	}
	return output, nil
}

func maxWorkers(parent *models.Task) int {
	cfg, _ := models.ParseSwarmConfig(parent.SwarmConfig)
	if cfg.MaxWorkers <= 0 {
		return 3
	}
	return cfg.MaxWorkers
}

func workerIsolation(w PlannerWorker, parentCfg models.SwarmConfig) string {
	if w.Isolation != "" {
		return w.Isolation
	}
	if parentCfg.DefaultWorkerIsolation != "" {
		return parentCfg.DefaultWorkerIsolation
	}
	return "worktree"
}

func allRequiredWorkersCompleted(children []models.Task, generation int) bool {
	workers := 0
	for _, child := range children {
		if child.SwarmRole != models.SwarmRoleWorker {
			continue
		}
		workers++
		cfg, _ := models.ParseSwarmConfig(child.SwarmConfig)
		if cfg.Required && (child.Status != models.StatusCompleted || cfg.CompletedGeneration < generation) {
			return false
		}
	}
	return workers > 0
}

func findChild(children []models.Task, role models.SwarmRole) *models.Task {
	for i := range children {
		if children[i].SwarmRole == role {
			return &children[i]
		}
	}
	return nil
}

func swarmBranch(title, kind, id string) string {
	return "swarm-" + slug(title) + "-" + slug(kind) + "-" + shortID(id)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = nonSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 24 {
		s = strings.Trim(s[:24], "-")
	}
	if s == "" {
		return "task"
	}
	return s
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" || id == "pending" {
		return "pending"
	}
	return id
}

func plannerPrompt(parentPrompt string, maxWorkers int) string {
	return fmt.Sprintf(`You are the planner for an OpenVibely swarm task.

Goal:
%s

Return strict JSON only. Do not edit files.

Create up to %d workers. Each worker must have a bounded objective, clear ownership, and an isolation mode. Prefer non-overlapping write scopes.

JSON schema:
{"workers":[{"title":"Backend worker","prompt":"Implement API/service changes...","worker_kind":"backend","ownership":["internal/service"],"isolation":"worktree","write_scope":["internal/service"],"read_scope":["."],"required":true}],"reviewer_prompt":"Review worker diffs...","integrator_prompt":"Integrate accepted worker outputs...","notes":"Optional short plan summary."}`, parentPrompt, maxWorkers)
}

func workerPrompt(parentPrompt string, worker PlannerWorker, worktreePath string) string {
	return fmt.Sprintf(`You are a worker in an OpenVibely swarm task.

Parent goal:
%s

Your objective:
%s

Ownership:
%s

Worktree:
%s

Stay inside your ownership unless the task is impossible without crossing it. If you cross ownership, say exactly what changed and why. Produce a concise summary, tests run, changed files, and any handoff notes for reviewer/integrator.`, parentPrompt, worker.Prompt, strings.Join(worker.Ownership, ", "), worktreePath)
}

func reviewerPrompt(parentPrompt, prompt string) string {
	return fmt.Sprintf(`You are the reviewer for an OpenVibely swarm task.

Parent goal:
%s

Review instruction:
%s

Review the worker changes before integration. Check correctness, tests, conflicts, missing requirements, ownership violations, and cross-worker incompatibilities. Do not edit source files.

Return a structured report with accepted worker task ids, rejected or blocked worker task ids, required fixes, integration risks, and tests that should be run by the integrator.`, parentPrompt, prompt)
}

func integratorPrompt(parentPrompt, prompt, worktree string) string {
	return fmt.Sprintf(`You are the integrator for an OpenVibely swarm task.

Parent goal:
%s

Integration worktree:
%s

Integration instruction:
%s

Apply accepted worker changes into the parent integration worktree. Do not use a worker worktree as the final destination. If conflicts occur, stop and report the conflict. When integration succeeds, produce the final parent-facing summary, tests run, changed files, and final diff status.`, parentPrompt, worktree, prompt)
}

func AttachSwarmChildren(tasks []models.Task) []models.Task {
	childrenByParent := map[string][]models.Task{}
	out := make([]models.Task, 0, len(tasks))
	for _, task := range tasks {
		if models.IsSwarmChildRole(task.SwarmRole) && task.ParentTaskID != nil {
			childrenByParent[*task.ParentTaskID] = append(childrenByParent[*task.ParentTaskID], task)
			continue
		}
		out = append(out, task)
	}
	for i := range out {
		if out[i].SwarmRole == models.SwarmRoleParent {
			children := childrenByParent[out[i].ID]
			sort.SliceStable(children, func(a, b int) bool {
				if children[a].SwarmSequence != children[b].SwarmSequence {
					return children[a].SwarmSequence < children[b].SwarmSequence
				}
				return children[a].CreatedAt.Before(children[b].CreatedAt)
			})
			out[i].SwarmChildren = children
		}
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
