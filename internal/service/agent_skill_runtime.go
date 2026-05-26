package service

import (
	"context"
	"strings"

	"github.com/openvibely/openvibely/internal/agentlibrary"
	"github.com/openvibely/openvibely/internal/agentskills"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func (s *LLMService) agentDeclaredSkillRuntimeTools(ctx context.Context, task models.Task, agent *models.Agent, workDir string) *llmcontracts.RuntimeTools {
	if s == nil || agent == nil || !agentExplicitlyAllowsAnyTool(agent, "skill_view", "skills_list", "agent_view", "skill_manage") {
		return nil
	}
	catalog := lifecycleTurnFromContext(ctx).Catalog
	if catalog == nil {
		return nil
	}
	projectRoot := ""
	if s.projectRepo != nil {
		projectRoot = projectSkillRoot(ctx, s.projectRepo, task.ProjectID)
	}
	var inspector agentskills.AgentInspector
	if s.agentRepo != nil {
		inspector = newAgentInspector(s.agentRepo, s.lifecycleRepo, nil)
	}
	var readers *llmcontracts.RuntimeTools
	if agentExplicitlyAllowsAnyTool(agent, "skills_list", "agent_view") {
		readers = agentskills.SkillRuntimeTools(catalog, s.globalSkillRoot, projectRoot, inspector)
	} else if agentExplicitlyAllowsTool(agent, "skill_view") {
		readers = agentskills.SelectedSkillRuntimeTools(catalog)
	}
	var writers *llmcontracts.RuntimeTools
	if agentExplicitlyAllowsTool(agent, "skill_manage") {
		importer := s.agentSkillImporter(task)
		var recorder agentlibrary.MutationRecorder
		if s.mutationRecorder != nil {
			recorder = s.mutationRecorder(task)
		}
		writers = agentlibrary.SkillMutationTools(importer, recorder)
	}
	return llmcontracts.CompositeRuntimeTools(readers, writers)
}

func (s *LLMService) agentSkillImporter(task models.Task) *agentlibrary.Importer {
	if s == nil || s.agentRepo == nil || s.lifecycleRepo == nil {
		return nil
	}
	projectRoot := ""
	if s.projectRepo != nil && task.ProjectID != "" {
		projectRoot = projectSkillRoot(context.Background(), s.projectRepo, task.ProjectID)
	}
	roots := agentlibrary.SkillRoots{Global: s.globalSkillRoot, Project: projectRoot}
	return agentlibrary.NewImporter(roots, agentlibrary.NewRepoApplier(s.agentRepo, s.lifecycleRepo))
}

func agentExplicitlyAllowsAnyTool(agent *models.Agent, names ...string) bool {
	for _, name := range names {
		if agentExplicitlyAllowsTool(agent, name) {
			return true
		}
	}
	return false
}

func agentExplicitlyAllowsTool(agent *models.Agent, name string) bool {
	if agent == nil || strings.TrimSpace(name) == "" {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, tool := range agent.Tools {
		if strings.ToLower(strings.TrimSpace(tool)) == want {
			return true
		}
	}
	return false
}
