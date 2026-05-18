package service

import (
	"context"
	"strings"

	"github.com/openvibely/openvibely/internal/agentlibrary"
	"github.com/openvibely/openvibely/internal/agentskills"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func (w *WorkerService) buildSkillCatalog(ctx context.Context, task models.Task) *agentskills.Catalog {
	projectRoot := projectSkillRoot(ctx, w.projectRepo, task.ProjectID)
	if agent := w.taskAgentDefinition(ctx, task); agent != nil && agent.Key != "" {
		catalog, err := agentskills.BuildAgentCatalog(task.ID, w.globalSkillRoot, projectRoot, agent.Key)
		if err != nil {
			return agentskills.NewCatalog(task.ID, nil)
		}
		return catalog
	}
	catalog, err := agentskills.BuildCatalog(task.ID, w.globalSkillRoot, projectRoot)
	if err != nil {
		// Catalog construction should never fail the user task; it only limits
		// available skills for this turn.
		return agentskills.NewCatalog(task.ID, nil)
	}
	return catalog
}

func (w *WorkerService) renderAvailableSkillsForTask(ctx context.Context, task models.Task, projectRoot string) string {
	if agent := w.taskAgentDefinition(ctx, task); agent != nil && agent.Key != "" {
		return agentskills.RenderAvailableAgentSkillsMarkdown(w.globalSkillRoot, projectRoot, agent.Key)
	}
	return agentskills.RenderAvailableSkillsMarkdown(w.globalSkillRoot, projectRoot)
}

func (w *WorkerService) buildLifecycleReadRuntimeTools(task models.Task, catalog *agentskills.Catalog) *llmcontracts.RuntimeTools {
	if catalog == nil {
		return nil
	}
	if catalog.IsAgentOwned() {
		return agentskills.SelectedSkillRuntimeTools(catalog)
	}
	var inspector agentskills.AgentInspector
	if w.agentRepo != nil {
		inspector = newAgentInspector(w.agentRepo, w.lifecycleRepo, w.CurrentLifecycleCatalog)
	}
	projectRoot := projectSkillRoot(context.Background(), w.projectRepo, task.ProjectID)
	return agentskills.SkillRuntimeTools(catalog, w.globalSkillRoot, projectRoot, inspector)
}

func (w *WorkerService) buildTaskSkillRuntimeTools(_ context.Context, _ models.Task, catalog *agentskills.Catalog) *llmcontracts.RuntimeTools {
	if catalog == nil {
		return nil
	}
	return agentskills.SelectedSkillRuntimeTools(catalog)
}

func (w *WorkerService) taskAgentDefinition(ctx context.Context, task models.Task) *models.Agent {
	if task.AgentDefinitionID == nil || *task.AgentDefinitionID == "" || w == nil || w.agentRepo == nil {
		return nil
	}
	agent, err := w.agentRepo.GetByID(ctx, *task.AgentDefinitionID)
	if err != nil {
		return nil
	}
	return agent
}

func (w *WorkerService) buildLifecycleRuntimeTools(task models.Task, catalog *agentskills.Catalog) *llmcontracts.RuntimeTools {
	if catalog == nil {
		return nil
	}
	var inspector agentskills.AgentInspector
	if w.agentRepo != nil {
		inspector = newAgentInspector(w.agentRepo, w.lifecycleRepo, w.CurrentLifecycleCatalog)
	}
	importer := w.buildLifecycleImporter(task)
	var recorder agentlibrary.MutationRecorder
	if w.mutationRecorder != nil {
		recorder = w.mutationRecorder(task)
	}
	projectRoot := projectSkillRoot(context.Background(), w.projectRepo, task.ProjectID)
	assignedAgent := w.taskAgentDefinition(context.Background(), task)
	assignedAgentKey := ""
	agentScope := "project"
	if assignedAgent != nil {
		assignedAgentKey = strings.TrimSpace(assignedAgent.Key)
		if strings.TrimSpace(string(assignedAgent.Scope)) == "global" {
			agentScope = "global"
		}
	}
	return lifecycleRuntimeTools(catalog, inspector, importer, recorder, w.globalSkillRoot, projectRoot, assignedAgentKey, agentScope)
}

func (w *WorkerService) buildLifecycleImporter(task models.Task) *agentlibrary.Importer {
	if w.agentRepo == nil || w.lifecycleRepo == nil {
		return nil
	}
	projectRoot := ""
	if w.projectRepo != nil && task.ProjectID != "" {
		projectRoot = projectSkillRoot(context.Background(), w.projectRepo, task.ProjectID)
	}
	roots := agentlibrary.SkillRoots{Global: w.globalSkillRoot, Project: projectRoot}
	return agentlibrary.NewImporter(roots, agentlibrary.NewRepoApplier(w.agentRepo, w.lifecycleRepo))
}
