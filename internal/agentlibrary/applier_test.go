package agentlibrary

import (
	"context"
	"errors"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

type memAgentStore struct {
	byKey   map[string]*models.Agent
	updates int
	creates int
	archive struct {
		id           string
		absorbedInto string
		reason       string
		called       bool
	}
}

func newMemAgentStore() *memAgentStore { return &memAgentStore{byKey: map[string]*models.Agent{}} }

func (s *memAgentStore) GetByKey(_ context.Context, key string) (*models.Agent, error) {
	return s.byKey[key], nil
}

func (s *memAgentStore) Create(_ context.Context, a *models.Agent) error {
	s.creates++
	a.ID = "id_" + a.Key
	cp := *a
	s.byKey[a.Key] = &cp
	return nil
}

func (s *memAgentStore) Update(_ context.Context, a *models.Agent) error {
	s.updates++
	cp := *a
	s.byKey[a.Key] = &cp
	return nil
}

func (s *memAgentStore) MarkArchived(_ context.Context, id, absorbedInto, reason string) error {
	s.archive.called = true
	s.archive.id = id
	s.archive.absorbedInto = absorbedInto
	s.archive.reason = reason
	for _, a := range s.byKey {
		if a.ID == id {
			a.GeneratedStatus = models.AgentStatusArchived
			a.Enabled = false
			a.AbsorbedInto = absorbedInto
		}
	}
	return nil
}

type memHookStore struct {
	hooksByAgent map[string][]models.AgentLifecycleHook
}

func newMemHookStore() *memHookStore {
	return &memHookStore{hooksByAgent: map[string][]models.AgentLifecycleHook{}}
}

func (s *memHookStore) HooksByAgent(_ context.Context, agentID string) ([]models.AgentLifecycleHook, error) {
	return s.hooksByAgent[agentID], nil
}

func (s *memHookStore) CreateHook(_ context.Context, h *models.AgentLifecycleHook) error {
	h.ID = "hk_" + string(h.When) + "_" + h.SkillKey
	s.hooksByAgent[h.AgentID] = append(s.hooksByAgent[h.AgentID], *h)
	return nil
}

func (s *memHookStore) UpdateHook(_ context.Context, h *models.AgentLifecycleHook) error {
	for i, existing := range s.hooksByAgent[h.AgentID] {
		if existing.ID == h.ID {
			s.hooksByAgent[h.AgentID][i] = *h
			return nil
		}
	}
	return errors.New("not found")
}

func (s *memHookStore) DeleteHook(_ context.Context, id string) error {
	for agentID, hs := range s.hooksByAgent {
		out := make([]models.AgentLifecycleHook, 0, len(hs))
		for _, h := range hs {
			if h.ID != id {
				out = append(out, h)
			}
		}
		s.hooksByAgent[agentID] = out
	}
	return nil
}

func newTestDeclaration() *SkillDeclaration {
	return &SkillDeclaration{
		Kind:    "openvibely.agent_skill",
		Version: 1,
		Agent: AgentDeclaration{
			Key:                 "backend-engineer",
			DisplayName:         "Backend Engineer",
			Description:         "Implements Go backend changes with tests.",
			SystemPrompt:        "You are a backend engineer.",
			Scope:               "global",
			SelectableAsPrimary: true,
		},
		Tools:         []string{"Read", "Edit", "Bash"},
		Plugins:       []string{},
		MCPServers:    []string{},
		Permissions:   PermissionsBlock{ReadRepositoryFiles: true, WriteRepositoryFiles: true},
		ModelDefaults: ModelDefaultsBlock{Model: "sonnet"},
		Routing: RoutingBlock{
			Description: "Backend Go service/database work.",
		},
		LifecycleHooks: map[string]HookDecl{
			"after_complete": {
				Skill:          "verify-change",
				OutputContract: "learning_summary",
				Permissions:    map[string]bool{"read_task_execution": true},
			},
		},
		EvidenceRefs: []string{"task_42"},
	}
}

func newSkillDeclaration() *SkillDeclaration {
	return &SkillDeclaration{
		Kind:    "openvibely.agent_skill",
		Version: 1,
		Agent:   AgentDeclaration{Key: "backend-engineer", Scope: "global"},
		Skill: SkillBlock{
			Key:         "verify-change",
			Name:        "Verify Backend Change",
			Scope:       "global",
			Description: "Run go test ./... after backend changes.",
		},
		Tools: []string{"Read"},
	}
}

func TestRepoApplier_CreatesAgentAndHook(t *testing.T) {
	agents := newMemAgentStore()
	hooks := newMemHookStore()
	ap := NewRepoApplier(agents, hooks)

	changes, err := ap.ApplyDeclaration(context.Background(), newTestDeclaration())
	if err != nil {
		t.Fatalf("ApplyDeclaration: %v", err)
	}
	if agents.creates != 1 {
		t.Fatalf("expected 1 create, got %d", agents.creates)
	}
	if len(changes) != 2 {
		t.Fatalf("expected agent+hook changes, got %v", changes)
	}
	stored := agents.byKey["backend-engineer"]
	if stored == nil {
		t.Fatal("expected stored agent")
	}
	if stored.Name != "Backend Engineer" {
		t.Fatalf("display name not applied: %q", stored.Name)
	}
	if stored.CreatedBy != models.AgentCreatedByAgent {
		t.Fatalf("expected created_by=agent, got %q", stored.CreatedBy)
	}
	if stored.GeneratedStatus != models.AgentStatusGenerated {
		t.Fatalf("expected generated_status=generated, got %q", stored.GeneratedStatus)
	}
	if !stored.PermissionDefaults.ReadRepositoryFiles {
		t.Fatalf("permission default not applied")
	}
	if stored.ModelDefaults.Model != "sonnet" {
		t.Fatalf("model defaults not applied: %+v", stored.ModelDefaults)
	}
	if len(hooks.hooksByAgent[stored.ID]) != 1 {
		t.Fatalf("expected one hook, got %d", len(hooks.hooksByAgent[stored.ID]))
	}
	h := hooks.hooksByAgent[stored.ID][0]
	if h.When != models.LifecycleAfterComplete || h.OutputContract != models.OutputContractLearningSummary {
		t.Fatalf("hook content unexpected: %+v", h)
	}
}

func TestRepoApplier_UpdatesExistingAgentAndHook(t *testing.T) {
	agents := newMemAgentStore()
	hooks := newMemHookStore()
	// Seed an existing user-edited agent.
	agents.byKey["backend-engineer"] = &models.Agent{
		ID:              "id_backend-engineer",
		Key:             "backend-engineer",
		Name:            "Backend Engineer (old)",
		GeneratedStatus: models.AgentStatusUserEdited,
		CreatedBy:       models.AgentCreatedByUser,
	}
	// Pretend the hook already exists too.
	hooks.hooksByAgent["id_backend-engineer"] = []models.AgentLifecycleHook{{
		ID:       "hk_existing",
		AgentID:  "id_backend-engineer",
		When:     models.LifecycleAfterComplete,
		SkillKey: "verify-change",
	}}
	ap := NewRepoApplier(agents, hooks)

	if _, err := ap.ApplyDeclaration(context.Background(), newTestDeclaration()); err != nil {
		t.Fatalf("ApplyDeclaration: %v", err)
	}
	if agents.updates != 1 {
		t.Fatalf("expected 1 update, got %d", agents.updates)
	}
	stored := agents.byKey["backend-engineer"]
	// Preserve user-edited status & original creator.
	if stored.GeneratedStatus != models.AgentStatusUserEdited {
		t.Fatalf("expected user-edited preserved, got %q", stored.GeneratedStatus)
	}
	if stored.CreatedBy != models.AgentCreatedByUser {
		t.Fatalf("expected original CreatedBy preserved, got %q", stored.CreatedBy)
	}
	// Hook should be updated in place, not duplicated.
	if got := len(hooks.hooksByAgent["id_backend-engineer"]); got != 1 {
		t.Fatalf("expected 1 hook (updated), got %d", got)
	}
}

func TestRepoApplier_SkillDeclarationPreservesAgentMetadata(t *testing.T) {
	agents := newMemAgentStore()
	agents.byKey["backend-engineer"] = &models.Agent{
		ID:              "id_backend-engineer",
		Key:             "backend-engineer",
		Name:            "Backend Engineer",
		SystemPrompt:    "Root prompt",
		Tools:           []string{"Bash"},
		GeneratedStatus: models.AgentStatusGenerated,
		CreatedBy:       models.AgentCreatedByAgent,
		Skills:          []models.SkillConfig{{Name: "existing", Description: "keep"}},
		PermissionDefaults: models.AgentPermissionDefaults{
			UseShellOrTools: true,
		},
	}
	ap := NewRepoApplier(agents, newMemHookStore())
	changes, err := ap.ApplyDeclaration(context.Background(), newSkillDeclaration())
	if err != nil {
		t.Fatalf("ApplyDeclaration skill: %v", err)
	}
	if len(changes) != 1 || changes[0] != "agent:update:backend-engineer" {
		t.Fatalf("expected only agent skill attachment update, got %v", changes)
	}
	stored := agents.byKey["backend-engineer"]
	if stored.SystemPrompt != "Root prompt" || len(stored.Tools) != 1 || stored.Tools[0] != "Bash" || !stored.PermissionDefaults.UseShellOrTools {
		t.Fatalf("skill declaration must preserve root metadata, got %#v", stored)
	}
	if len(stored.Skills) != 2 || stored.Skills[0].Name != "existing" || stored.Skills[1].Name != "verify-change" {
		t.Fatalf("skill declaration should attach/update only the declared skill, got %#v", stored.Skills)
	}
}

func TestRepoApplier_SkillDeclarationDoesNotCreateHooks(t *testing.T) {
	agents := newMemAgentStore()
	hooks := newMemHookStore()
	ap := NewRepoApplier(agents, hooks)
	decl := newSkillDeclaration()
	decl.LifecycleHooks = map[string]HookDecl{"after_complete": {Skill: "verify-change", OutputContract: "learning_summary"}}
	if _, err := ap.ApplyDeclaration(context.Background(), decl); err != nil {
		t.Fatalf("ApplyDeclaration skill: %v", err)
	}
	stored := agents.byKey["backend-engineer"]
	if stored == nil {
		t.Fatal("expected agent created")
	}
	if len(hooks.hooksByAgent[stored.ID]) != 0 {
		t.Fatalf("individual skill declarations must not create lifecycle hooks, got %#v", hooks.hooksByAgent[stored.ID])
	}
}

func TestRepoApplier_ProtectedRejected(t *testing.T) {
	agents := newMemAgentStore()
	agents.byKey["backend-engineer"] = &models.Agent{
		ID:              "id_backend-engineer",
		Key:             "backend-engineer",
		GeneratedStatus: models.AgentStatusProtected,
	}
	ap := NewRepoApplier(agents, newMemHookStore())

	_, err := ap.ApplyDeclaration(context.Background(), newTestDeclaration())
	if err == nil {
		t.Fatal("expected protected error")
	}
	prot, why, _ := ap.IsProtected(context.Background(), "agent", "backend-engineer")
	if !prot || why == "" {
		t.Fatalf("expected protection report, got prot=%v why=%q", prot, why)
	}
	// Standalone generated skills do not inherit agent protection because they are
	// not owned by agents.
	prot, why, _ = ap.IsProtected(context.Background(), "skill", "verify-change")
	if prot || why != "" {
		t.Fatalf("standalone skill should not inherit agent protection, got prot=%v why=%q", prot, why)
	}
}

func TestRepoApplier_ArchiveAgentAndStandaloneSkill(t *testing.T) {
	agents := newMemAgentStore()
	agents.byKey["backend-engineer"] = &models.Agent{
		ID:              "id_backend-engineer",
		Key:             "backend-engineer",
		GeneratedStatus: models.AgentStatusGenerated,
		Skills:          []models.SkillConfig{{Name: "verify-change"}, {Name: "other"}},
	}
	ap := NewRepoApplier(agents, newMemHookStore())

	if err := ap.ArchiveAgent(context.Background(), "backend-engineer", "platform-engineer", "absorbed"); err != nil {
		t.Fatalf("ArchiveAgent: %v", err)
	}
	if !agents.archive.called || agents.archive.absorbedInto != "platform-engineer" {
		t.Fatalf("MarkArchived not invoked correctly: %+v", agents.archive)
	}
	if agents.byKey["backend-engineer"].GeneratedStatus != models.AgentStatusArchived {
		t.Fatal("expected agent flipped to archived")
	}
	if err := ap.ArchiveSkill(context.Background(), "verify-change", "verify-change-v2", "renamed"); err != nil {
		t.Fatalf("ArchiveSkill: %v", err)
	}
	updated := agents.byKey["backend-engineer"]
	if len(updated.Skills) != 2 {
		t.Fatalf("standalone skill archive must not mutate embedded agent skills, got %+v", updated.Skills)
	}
}

func TestRepoApplier_NilStoreErrors(t *testing.T) {
	ap := NewRepoApplier(nil, nil)
	if _, err := ap.ApplyDeclaration(context.Background(), newTestDeclaration()); err == nil {
		t.Fatal("expected error when AgentStore is nil")
	}
	if err := ap.ArchiveAgent(context.Background(), "x", "", ""); err == nil {
		t.Fatal("expected error when AgentStore is nil")
	}
}

func TestRepoApplier_RejectsInvalidDeclaration(t *testing.T) {
	ap := NewRepoApplier(newMemAgentStore(), newMemHookStore())
	bad := newTestDeclaration()
	bad.Kind = "bogus"
	if _, err := ap.ApplyDeclaration(context.Background(), bad); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRepoApplier_SparseAgentRootDeclarationPreservesRichExistingFields(t *testing.T) {
	agents := newMemAgentStore()
	existing := &models.Agent{
		ID:           "id_backend",
		Key:          "backend",
		Name:         "Backend",
		SystemPrompt: "old prompt",
		Model:        "custom-model",
		Tools:        []string{"Read", "Bash"},
		ToolConfig: models.AgentToolConfig{
			ScopedFiles: []models.ScopedFilesConfig{{Directory: ".openvibely/memory", Permissions: []string{"read", "write"}}},
		},
		Plugins:    []string{"plugin@example"},
		MCPServers: []models.MCPServerConfig{{Name: "server", Command: []string{"cmd"}, Env: map[string]string{"A": "B"}}},
		PermissionDefaults: models.AgentPermissionDefaults{
			ReadAgents:  true,
			WriteSkills: true,
		},
		ModelDefaults: models.AgentModelDefaults{Model: "custom-model", Temperature: 0.3, MaxTokens: 123},
		SourceRefs:    []string{"https://example.test"},
	}
	agents.byKey[existing.Key] = existing
	applier := NewRepoApplier(agents, nil)
	decl := &SkillDeclaration{
		Kind:    "openvibely.agent_skill",
		Version: 1,
		Agent: AgentDeclaration{
			Key:          "backend",
			Name:         "Backend v2",
			SystemPrompt: "new prompt",
			Scope:        "global",
		},
	}
	if _, err := applier.ApplyDeclaration(context.Background(), decl); err != nil {
		t.Fatalf("ApplyDeclaration: %v", err)
	}
	got := agents.byKey["backend"]
	if got.Name != "Backend v2" || got.SystemPrompt != "new prompt" {
		t.Fatalf("identity fields not updated: %+v", got)
	}
	if len(got.Tools) != 2 || got.Tools[1] != "Bash" || len(got.ToolConfig.ScopedFiles) != 1 || len(got.Plugins) != 1 || len(got.MCPServers) != 1 || got.MCPServers[0].Command[0] != "cmd" {
		t.Fatalf("rich runtime fields not preserved: %+v", got)
	}
	if !got.PermissionDefaults.ReadAgents || !got.PermissionDefaults.WriteSkills || got.ModelDefaults.MaxTokens != 123 || len(got.SourceRefs) != 1 {
		t.Fatalf("policy/default fields not preserved: %+v", got)
	}
}
