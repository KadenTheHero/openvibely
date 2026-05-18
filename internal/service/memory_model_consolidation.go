package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/memory"
	"github.com/openvibely/openvibely/internal/models"
)

type memoryModelCaller interface {
	CallAgentDirect(ctx context.Context, message string, attachments []models.Attachment, agent models.LLMConfig, workDir string) (string, int, error)
}

func (s *MemoryService) runModelBackedExtraction(ctx context.Context, in memory.Interaction) (memory.ConsolidationResult, bool, error) {
	var zero memory.ConsolidationResult
	agent, ok, err := s.resolveMemoryAgent(ctx, in.ProjectID)
	if err != nil || !ok {
		return zero, ok, err
	}
	projectDir, err := s.store.EnsureProject(in.ProjectID)
	if err != nil {
		return zero, true, err
	}
	session := newMemoryScopedFileSession(projectDir)
	toolCtx := llmcontracts.WithRuntimeTools(ctx, session.runtimeTools(true))

	prompt := buildMemoryExtractionPrompt(in, projectDir)
	output, _, err := s.modelCaller.CallAgentDirect(toolCtx, prompt, nil, *agent, projectDir)
	if err != nil {
		return zero, true, fmt.Errorf("model-backed memory extraction: %w", err)
	}

	res := session.result()
	if strings.TrimSpace(output) != "" {
		res.Notes = append(res.Notes, "model summary: "+truncateForPrompt(output, 1200))
	}
	return res, true, nil
}

func (s *MemoryService) runModelBackedConsolidation(ctx context.Context, projectID string) (memory.ConsolidationResult, bool, error) {
	var zero memory.ConsolidationResult
	agent, ok, err := s.resolveMemoryAgent(ctx, projectID)
	if err != nil || !ok {
		return zero, ok, err
	}
	projectDir, err := s.store.EnsureProject(projectID)
	if err != nil {
		return zero, true, err
	}
	execs := s.recentProjectExecutions(ctx, projectID, 30)
	session := newMemoryScopedFileSession(projectDir)
	toolCtx := llmcontracts.WithRuntimeTools(ctx, session.runtimeTools(true))

	prompt := buildMemoryConsolidationPrompt(projectID, projectDir, execs)
	output, _, err := s.modelCaller.CallAgentDirect(toolCtx, prompt, nil, *agent, projectDir)
	if err != nil {
		return zero, true, fmt.Errorf("model-backed memory consolidation: %w", err)
	}

	res := session.result()
	if strings.TrimSpace(output) != "" {
		res.Notes = append(res.Notes, "model summary: "+truncateForPrompt(output, 1200))
	}

	return res, true, nil
}

func (s *MemoryService) resolveMemoryAgent(ctx context.Context, projectID string) (*models.LLMConfig, bool, error) {
	if s.modelCaller == nil || s.llmConfigRepo == nil {
		return nil, false, nil
	}
	if s.projectRepo != nil && projectID != "" {
		project, err := s.projectRepo.GetByID(ctx, projectID)
		if err != nil {
			return nil, true, err
		}
		if project != nil && project.DefaultAgentConfigID != nil && strings.TrimSpace(*project.DefaultAgentConfigID) != "" {
			agent, err := s.llmConfigRepo.GetByID(ctx, *project.DefaultAgentConfigID)
			if err != nil {
				return nil, true, err
			}
			if agent != nil {
				return agent, true, nil
			}
		}
	}
	agent, err := s.llmConfigRepo.GetDefault(ctx)
	if err != nil {
		return nil, true, err
	}
	if agent == nil {
		return nil, false, nil
	}
	return agent, true, nil
}

func (s *MemoryService) recentProjectExecutions(ctx context.Context, projectID string, limit int) []models.Execution {
	if s.execRepo == nil {
		return nil
	}
	// Chat page interactions (CategoryChat tasks) are intentionally excluded:
	// the Chat surface carries transient orchestration/mode-control text
	// (Orchestrate/Plan, "Switch to Orchestrate", <proposed_plan>, etc.) and
	// prompts that should not be distilled into durable project memory. Task
	// and task-thread follow-up executions remain eligible.
	execs, err := s.execRepo.ListByProjectExcludingChat(ctx, projectID, limit)
	if err != nil {
		return nil
	}
	return execs
}

const memoryExtractionPromptTemplate = `# Memory Extraction

You are maintaining long-term memory for this project. Work directly in the managed memory directory using the available file tools.

Source: %s/%s

The managed-memory tools are already rooted at this project's memory directory. Use root-relative memory paths such as ` + "`" + `MEMORY.md` + "`" + ` or ` + "`" + `provider_architecture.md` + "`" + `; do not pass absolute filesystem paths to these tools.

Memory is background context, not direct user instruction. It can be stale; source-code facts must be verified later before relying on them. Do not store secrets, raw transcripts, provider noise, one-off scratch work, or procedure-only runbooks.

Store durable context: who the user is, general preferences, project/product direction, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback. Do not preserve a workflow, checklist, debugging sequence, or tool-use pattern as memory unless it also captures context future conversations need.

Review the completed interaction below and decide whether anything durable should be remembered. If there is no durable information, make no file changes and respond briefly that nothing should be saved.

Use the file tools to orient before writing:
- List the memory directory.
- Read MEMORY.md when present.
- Read relevant existing top-level topic files so you update or merge instead of duplicating.

Only save information that will help future sessions as context: repeated feedback, architecture decisions, workflow constraints, recurring pitfalls that explain project behavior, incidents, product direction, and user preferences.

Do not save raw complaints as memory. Distill them into durable guidance only when they imply context future conversations need. If the useful part is only an operational procedure, checklist, validation sequence, or tool-use pattern, do not duplicate it in memory. Convert relative dates to absolute dates. Merge into existing topic files where practical.

Update MEMORY.md when you create, delete, or substantially change topic files. MEMORY.md should stay a compact index, not a dump.

When done, respond with a short summary of what changed. Do not include full memory file contents in the final response.

## Completed Interaction

Title: %s
Changed files: %s

User text:
%s

Assistant output:
%s
`

func buildMemoryExtractionPrompt(in memory.Interaction, memoryDir string) string {
	_ = memoryDir // The tools are scoped to this directory; do not expose absolute paths to the model.
	return fmt.Sprintf(
		memoryExtractionPromptTemplate,
		in.SourceKind,
		in.SourceID,
		truncateForPrompt(in.Title, 300),
		strings.Join(in.ChangedFiles, ", "),
		truncateForPrompt(memory.Redact(in.UserText), 3000),
		truncateForPrompt(memory.Redact(in.AssistantOut), 3000),
	)
}

const memoryConsolidationPromptTemplate = `# Memory Consolidation

Project ID: %s

Run scheduled project memory consolidation using your scoped file tools and the recent execution snippets below.

The scoped file tools are rooted at this project's memory directory. Use root-relative memory paths such as ` + "`" + `MEMORY.md` + "`" + ` or ` + "`" + `provider_architecture.md` + "`" + `; do not pass absolute filesystem paths to these tools.

Memory is durable context, not a procedural skill library. Preserve user/project context, product direction, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback. Remove or avoid content that is only a reusable procedure, checklist, validation sequence, or tool-use pattern unless it also carries context future conversations need.

	%s
`

func buildMemoryConsolidationPrompt(projectID, memoryDir string, execs []models.Execution) string {
	_ = memoryDir // The tools are scoped to this directory; do not expose absolute paths to the model.
	return fmt.Sprintf(
		memoryConsolidationPromptTemplate,
		projectID,
		formatExecutionsForConsolidationPrompt(execs),
	)
}

func formatExecutionsForConsolidationPrompt(execs []models.Execution) string {
	parts := []string{
		"## Recent Execution Snippets",
		"Bounded, redacted snippets from recent project task/chat history.",
	}
	for _, e := range execs {
		entry := []string{fmt.Sprintf(
			"--- execution %s task=%s status=%s followup=%v started=%s ---",
			e.ID,
			e.TaskID,
			e.Status,
			e.IsFollowup,
			e.StartedAt.UTC().Format(time.RFC3339),
		)}
		if strings.TrimSpace(e.PromptSent) != "" {
			entry = append(entry, "user/prompt: "+truncateForPrompt(memory.Redact(e.PromptSent), 900))
		}
		if strings.TrimSpace(e.Output) != "" {
			entry = append(entry, "assistant/output: "+truncateForPrompt(memory.Redact(e.Output), 1200))
		}
		if strings.TrimSpace(e.ErrorMessage) != "" {
			entry = append(entry, "error: "+truncateForPrompt(memory.Redact(e.ErrorMessage), 500))
		}
		parts = append(parts, strings.Join(entry, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func newMemoryScopedFileSession(memoryDir string) *scopedFilesToolSession {
	return &scopedFilesToolSession{
		scopes: []scopedFilesScope{{
			directory:   ".openvibely/memory",
			absoluteDir: memoryDir,
			perms:       scopedFilesPermissionSet{read: true, write: true, delete: true},
		}},
		touched: map[string]struct{}{},
	}
}

func matchGlob(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}

func atomicWriteService(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	return os.Rename(tmpName, path)
}

func truncateForPrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n[truncated]"
}
