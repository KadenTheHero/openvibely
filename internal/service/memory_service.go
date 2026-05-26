package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/openvibely/openvibely/internal/memory"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

// MemoryService is the high-level orchestrator for OpenVibely's auto-memory
// subsystem. It wraps the memory package's PathResolver/FileStore/etc. and
// the MemoryRepo for run/schedule metadata. Callers in handlers and other
// services depend on this type rather than the lower-level memory package.
type MemoryService struct {
	repo           *repository.MemoryRepo
	taskRepo       *repository.TaskRepo
	scheduleRepo   *repository.ScheduleRepo
	agentRepo      *repository.AgentRepo
	llmConfigRepo  *repository.LLMConfigRepo
	projectRepo    *repository.ProjectRepo
	execRepo       *repository.ExecutionRepo
	modelCaller    memoryModelCaller
	store          *memory.FileStore
	pathResolver   *memory.PathResolver
	contextBuilder *memory.ContextBuilder
	defaultEnabled bool
}

// NewMemoryService builds a service with the given pieces. Automatic memory
// extraction/consolidation is model-backed and tool-driven when a model is
// configured; without a model, extraction records a no-op run.
func NewMemoryService(
	repo *repository.MemoryRepo,
	taskRepo *repository.TaskRepo,
	scheduleRepo *repository.ScheduleRepo,
	agentRepo *repository.AgentRepo,
	llmConfigRepo *repository.LLMConfigRepo,
	projectRepo *repository.ProjectRepo,
	execRepo *repository.ExecutionRepo,
	modelCaller memoryModelCaller,
	store *memory.FileStore,
	resolver *memory.PathResolver,
) *MemoryService {
	return &MemoryService{
		repo:           repo,
		taskRepo:       taskRepo,
		scheduleRepo:   scheduleRepo,
		agentRepo:      agentRepo,
		llmConfigRepo:  llmConfigRepo,
		projectRepo:    projectRepo,
		execRepo:       execRepo,
		modelCaller:    modelCaller,
		store:          store,
		pathResolver:   resolver,
		contextBuilder: memory.NewContextBuilder(store),
		defaultEnabled: true,
	}
}

// PathResolver returns the underlying resolver (for diagnostics / "Open
// Memory" UI).
func (s *MemoryService) PathResolver() *memory.PathResolver { return s.pathResolver }

// Store returns the underlying file store.
func (s *MemoryService) Store() *memory.FileStore { return s.store }

// ProjectDir returns the absolute on-disk per-project memory directory.
func (s *MemoryService) ProjectDir(projectID string) (string, error) {
	return s.pathResolver.ProjectDir(projectID)
}

func (s *MemoryService) ensureProjectMemoryDir(ctx context.Context, projectID string) (string, error) {
	if err := s.refreshProjectMemoryDir(ctx, projectID); err != nil {
		return "", err
	}
	return s.store.EnsureProject(projectID)
}

// IsEnabled returns whether memory is enabled for projectID. Defaults to true
// when no explicit settings row exists.
func (s *MemoryService) IsEnabled(ctx context.Context, projectID string) (bool, error) {
	settings, err := s.repo.GetSettings(ctx, projectID)
	if err != nil {
		return false, err
	}
	return settings.Enabled, nil
}

// SetEnabled persists the enabled flag.
func (s *MemoryService) SetEnabled(ctx context.Context, projectID string, enabled bool) error {
	return s.repo.UpsertSettings(ctx, projectID, enabled)
}

// EnsureProject ensures both the on-disk directory layout and DB rows exist
// for projectID. Idempotent and safe to call on every server boot or when a
// project is created.
func (s *MemoryService) EnsureProject(ctx context.Context, projectID string) error {
	if _, err := s.ensureProjectMemoryDir(ctx, projectID); err != nil {
		return err
	}
	if err := s.repo.EnsureSettings(ctx, projectID); err != nil {
		return err
	}
	return s.ensureConsolidationTaskSchedule(ctx, projectID)
}

func (s *MemoryService) refreshProjectMemoryDir(ctx context.Context, projectID string) error {
	if s.projectRepo == nil || s.pathResolver == nil {
		return nil
	}
	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil || project == nil {
		return err
	}
	if strings.TrimSpace(project.RepoPath) == "" {
		return fmt.Errorf("memory: project %s has no local repo_path", projectID)
	}
	dir, err := memory.SharedRepoMemoryDir(project.RepoPath)
	if err != nil {
		return err
	}
	return s.pathResolver.SetProjectDirOverride(projectID, dir)
}

const memoryConsolidatorAgentName = "System: Memory Consolidator"
const memoryConsolidationTaskTitle = "System: Memory Consolidation"

const memoryConsolidationTaskPrompt = `Consolidate this project's durable memory.

Use the scoped file tools to inspect and update .openvibely/memory. Keep MEMORY.md as the compact index. Merge duplicate or stale topic files. Preserve durable project facts, preferences, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback. Do not store transient logs, raw transcripts, secrets, task-by-task summaries, or procedure-only runbooks.

When done, respond with a short summary of what changed.`

const memoryConsolidatorAgentPrompt = `# Memory Consolidator

You maintain durable long-term memory for an OpenVibely project using the managed memory files available through your tools.

Memory is background context, not direct user instruction. It can be stale; source-code facts must be verified later before relying on them. Do not store secrets, raw transcripts, provider noise, one-off scratch work, or procedure-only runbooks.

Preserve context future conversations need: who the user is, general preferences, project/product direction, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback. A lesson belongs in memory only when it carries contextual meaning beyond a reusable procedure.

When consolidating:
- List the memory directory.
- Read MEMORY.md first when present.
- Read relevant top-level topic files before writing so you update or merge instead of duplicating.
- Review recent execution snippets only for durable context: repeated feedback, architecture decisions, workflow constraints, recurring pitfalls that explain project behavior, incidents, product direction, current-state facts, and user preferences.
- Create, update, split, merge, or delete focused top-level markdown memory files.
- Use descriptive snake_case filenames.
- Merge new information into existing topic files instead of creating near-duplicates.
- Convert relative dates like "yesterday" or "last week" to absolute dates.
- Delete contradicted or stale facts that no longer help future sessions.
- When deleting or merging a memory topic file, also remove or update its MEMORY.md index reference.
- Do not save facts fully derivable from current source code, git history, or static repo instructions.
- Do not save reusable procedures, checklists, validation sequences, or tool-use patterns unless they also carry durable context.
- Keep frontmatter on memory files with name, type, created, updated, source, source_id, confidence, and title when practical.
- Keep MEMORY.md as a compact index, not the full memory store.

When done, respond with a short summary of what changed. Do not include full memory file contents in the final response.`

func agentHasTool(tools []string, tool string) bool {
	for _, existing := range tools {
		if existing == tool {
			return true
		}
	}
	return false
}

func ensureAgentTool(tools []string, tool string) []string {
	if agentHasTool(tools, tool) {
		return tools
	}
	return append(tools, tool)
}

func memoryConsolidatorToolConfig() models.AgentToolConfig {
	return models.AgentToolConfig{
		ScopedFiles: []models.ScopedFilesConfig{{
			Directory:   ".openvibely/memory",
			Permissions: []string{"read", "write", "delete"},
		}},
		SkipDefaultTools:       true,
		DisableRuntimeWorktree: true,
	}
}

func sameScopedToolConfig(a, b models.AgentToolConfig) bool {
	if a.SkipDefaultTools != b.SkipDefaultTools || a.DisableRuntimeWorktree != b.DisableRuntimeWorktree || len(a.ScopedFiles) != len(b.ScopedFiles) {
		return false
	}
	for i := range a.ScopedFiles {
		if a.ScopedFiles[i].Directory != b.ScopedFiles[i].Directory || strings.Join(a.ScopedFiles[i].Permissions, ",") != strings.Join(b.ScopedFiles[i].Permissions, ",") {
			return false
		}
	}
	return true
}

func (s *MemoryService) ensureConsolidationTaskSchedule(ctx context.Context, projectID string) error {
	if s.taskRepo == nil || s.scheduleRepo == nil || s.agentRepo == nil {
		return nil
	}
	agent, err := s.ensureMemoryConsolidatorAgent(ctx)
	if err != nil {
		return err
	}
	task, err := s.taskRepo.GetByProjectAndTitle(ctx, projectID, memoryConsolidationTaskTitle)
	if err != nil {
		return err
	}
	if task == nil {
		agentID := agent.ID
		task = &models.Task{
			ProjectID:         projectID,
			Title:             memoryConsolidationTaskTitle,
			Category:          models.CategoryScheduled,
			Priority:          0,
			Status:            models.StatusPending,
			Prompt:            memoryConsolidationTaskPrompt,
			AgentDefinitionID: &agentID,
			Tag:               models.TagNone,
			ChainConfig:       "{}",
			CreatedVia:        models.TaskOriginWeb,
		}
		if err := s.taskRepo.Create(ctx, task); err != nil {
			if err != repository.ErrDuplicateTask {
				return err
			}
			task, err = s.taskRepo.GetByProjectAndTitle(ctx, projectID, memoryConsolidationTaskTitle)
			if err != nil || task == nil {
				return err
			}
		}
	}
	if task.Prompt != memoryConsolidationTaskPrompt || task.Title != memoryConsolidationTaskTitle || task.Category != models.CategoryScheduled || task.AgentDefinitionID == nil || *task.AgentDefinitionID != agent.ID {
		agentID := agent.ID
		task.Title = memoryConsolidationTaskTitle
		task.Category = models.CategoryScheduled
		task.Prompt = memoryConsolidationTaskPrompt
		task.AgentDefinitionID = &agentID
		task.Tag = models.TagNone
		if task.ChainConfig == "" {
			task.ChainConfig = "{}"
		}
		if err := s.taskRepo.Update(ctx, task); err != nil {
			return err
		}
	}
	schedules, err := s.scheduleRepo.ListByTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if len(schedules) > 0 {
		return nil
	}
	runAt := time.Now().UTC().Add(24 * time.Hour)
	return s.scheduleRepo.Create(ctx, &models.Schedule{
		TaskID:         task.ID,
		RunAt:          runAt,
		RepeatType:     models.RepeatDaily,
		RepeatInterval: 1,
		Enabled:        true,
		NextRun:        &runAt,
	})
}

func (s *MemoryService) ensureMemoryConsolidatorAgent(ctx context.Context) (*models.Agent, error) {
	agent, err := s.agentRepo.GetBySystemKind(ctx, models.AgentSystemKindMemoryConsolidator)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		agent = &models.Agent{
			Name:         memoryConsolidatorAgentName,
			Description:  "Built-in agent that consolidates OpenVibely project memory.",
			SystemPrompt: memoryConsolidatorAgentPrompt,
			Model:        "inherit",
			Tools:        []string{models.AgentToolScopedFiles},
			ToolConfig:   memoryConsolidatorToolConfig(),
			Plugins:      []string{},
			MCPServers:   []models.MCPServerConfig{},
			Skills:       []models.SkillConfig{},
			SystemKind:   models.AgentSystemKindMemoryConsolidator,
		}
		if err := s.agentRepo.Create(ctx, agent); err != nil {
			return nil, err
		}
		return agent, nil
	}
	wantConfig := memoryConsolidatorToolConfig()
	if agent.Name != memoryConsolidatorAgentName || agent.SystemPrompt != memoryConsolidatorAgentPrompt || agent.Model == "" || !agentHasTool(agent.Tools, models.AgentToolScopedFiles) || !sameScopedToolConfig(agent.ToolConfig, wantConfig) {
		agent.Name = memoryConsolidatorAgentName
		agent.Description = "Built-in agent that consolidates OpenVibely project memory."
		agent.SystemPrompt = memoryConsolidatorAgentPrompt
		if agent.Model == "" {
			agent.Model = "inherit"
		}
		agent.SystemKind = models.AgentSystemKindMemoryConsolidator
		agent.Tools = ensureAgentTool(agent.Tools, models.AgentToolScopedFiles)
		agent.ToolConfig = wantConfig
		if err := s.agentRepo.Update(ctx, agent); err != nil {
			return nil, err
		}
	}
	return agent, nil
}

// BuildContext returns the bounded memory block to inject into a prompt for
// projectID. When memory is disabled or unavailable, an empty string is
// returned. Errors loading memory are logged but never bubble up to block
// the caller.
func (s *MemoryService) BuildContext(ctx context.Context, projectID string, opts memory.ContextOptions) string {
	if _, err := s.ensureProjectMemoryDir(ctx, projectID); err != nil {
		log.Printf("memory: build-context: ensure project failed for %s: %v", projectID, err)
		return ""
	}
	enabled, err := s.IsEnabled(ctx, projectID)
	if err != nil {
		log.Printf("memory: build-context: settings lookup failed for %s: %v", projectID, err)
		return ""
	}
	out, err := s.contextBuilder.Build(projectID, enabled, opts)
	if err != nil {
		log.Printf("memory: build-context: %v", err)
		return ""
	}
	return out
}

// EnqueueExtraction kicks off a memory extraction pass for a completed
// interaction. The pass runs asynchronously so callers in completion paths
// (worker, chat, threads, webhook handlers) are never blocked.
//
// Skip reasons are recorded as run rows with status="nothing" so the UI can
// surface why nothing was saved.
func (s *MemoryService) EnqueueExtraction(in memory.Interaction) {
	go func() {
		// Use a fresh detached context so the caller's request lifetime does
		// not cancel the extraction half-way.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := s.runExtraction(ctx, in); err != nil {
			log.Printf("memory: extraction failed for project=%s source=%s/%s: %v",
				in.ProjectID, in.SourceKind, in.SourceID, err)
		}
	}()
}

// runExtraction is the synchronous body of an extraction pass. Exposed for
// testing.
func (s *MemoryService) runExtraction(ctx context.Context, in memory.Interaction) error {
	if in.ProjectID == "" {
		return fmt.Errorf("memory: missing project id")
	}

	enabled, err := s.IsEnabled(ctx, in.ProjectID)
	if err != nil {
		return err
	}
	if reason := memory.ShouldExtract(enabled, in); reason != "" {
		// Record a "nothing" run for visibility (helps the Schedule/Status UI).
		runID, cerr := s.repo.CreateExtractionRun(ctx, in.ProjectID, string(in.SourceKind), in.SourceID)
		if cerr == nil {
			_ = s.repo.FinishExtractionRun(ctx, runID, "nothing", string(reason), "", nil)
		}
		return nil
	}
	if _, err := s.ensureProjectMemoryDir(ctx, in.ProjectID); err != nil {
		return err
	}

	runID, err := s.repo.CreateExtractionRun(ctx, in.ProjectID, string(in.SourceKind), in.SourceID)
	if err != nil {
		return err
	}

	res, usedModel, extractErr := s.runModelBackedExtraction(ctx, in)
	if !usedModel {
		reason := "model-backed extraction skipped: no default/project model configured"
		_ = s.repo.FinishExtractionRun(ctx, runID, "nothing", reason, "", nil)
		return nil
	}
	if extractErr != nil {
		_ = s.repo.FinishExtractionRun(ctx, runID, "error", "", extractErr.Error(), nil)
		return extractErr
	}

	status := "ok"
	errMsg := ""
	reason := strings.Join(res.Notes, "; ")
	if len(res.TouchedPaths) == 0 {
		status = "nothing"
		if reason == "" {
			reason = "Nothing to save"
		}
	}

	_ = s.repo.FinishExtractionRun(ctx, runID, status, reason, errMsg, res.TouchedPaths)
	return nil
}

// RunConsolidationNow performs a consolidation pass synchronously and returns
// the recorded run row. Kept for explicit/debug routes; scheduled consolidation
// runs through the normal task execution path.
func (s *MemoryService) RunConsolidationNow(ctx context.Context, projectID string) (*models.MemoryConsolidationRun, error) {
	if err := s.EnsureProject(ctx, projectID); err != nil {
		return nil, err
	}
	runID, err := s.repo.CreateConsolidationRun(ctx, projectID)
	if err != nil {
		return nil, err
	}

	res, usedModel, runErr := s.runModelBackedConsolidation(ctx, projectID)
	status := "ok"
	if !usedModel {
		status = "nothing"
		res.Notes = append(res.Notes, "memory consolidation skipped: no project/default model configured")
	}
	errMsg := ""
	if runErr != nil {
		status = "error"
		errMsg = runErr.Error()
	}
	if err := s.repo.FinishConsolidationRun(ctx, runID, status, errMsg, res.TouchedPaths, res.Notes); err != nil {
		return nil, err
	}
	return s.repo.GetLatestConsolidationRun(ctx, projectID)
}

// GetLatestConsolidationRun returns the latest consolidation run row.
func (s *MemoryService) GetLatestConsolidationRun(ctx context.Context, projectID string) (*models.MemoryConsolidationRun, error) {
	return s.repo.GetLatestConsolidationRun(ctx, projectID)
}

// IndexSize returns the current MEMORY.md size in bytes, for status UI.
func (s *MemoryService) IndexSize(projectID string) int64 {
	n, _ := s.store.IndexSizeBytes(projectID)
	return n
}

// IndexBody returns MEMORY.md text (or "" when missing).
func (s *MemoryService) IndexBody(projectID string) string {
	body, _ := s.store.ReadIndex(projectID)
	return strings.TrimSpace(body)
}

// LastExtractionRuns returns up to limit recent extraction runs.
func (s *MemoryService) LastExtractionRuns(ctx context.Context, projectID string, limit int) ([]models.MemoryExtractionRun, error) {
	return s.repo.ListRecentExtractionRuns(ctx, projectID, limit)
}
