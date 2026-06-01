package repository

import (
	"context"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/testutil"
)

func TestAgentRepo_CreateAndReadWithoutColorColumn(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewAgentRepo(db)
	ctx := context.Background()

	agent := &models.Agent{
		Name:         "No Color Agent",
		Description:  "Agent without legacy color field",
		SystemPrompt: "Do focused work.",
		Model:        "inherit",
		Tools:        []string{"Read", "Grep"},
		Plugins:      []string{"playwright@claude-plugins-official"},
		Skills: []models.SkillConfig{
			{
				Name:        "scope-and-plan",
				Description: "Understand constraints before edits",
				Tools:       "Read, Grep",
				Content:     "Review related files first.",
			},
		},
		MCPServers: []models.MCPServerConfig{
			{
				Name:    "playwright",
				Command: []string{"npx", "-y", "@playwright/mcp"},
			},
		},
		SystemKind: "test_system",
	}

	if err := repo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	stored, err := repo.GetByID(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored agent")
	}
	if stored.Name != agent.Name {
		t.Fatalf("expected name %q, got %q", agent.Name, stored.Name)
	}
	if len(stored.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(stored.Tools))
	}
	if len(stored.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(stored.Plugins))
	}
	if len(stored.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(stored.Skills))
	}
	if len(stored.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(stored.MCPServers))
	}
	if stored.SystemKind != "test_system" {
		t.Fatalf("expected system_kind to round-trip, got %q", stored.SystemKind)
	}
}

func TestAgentRepo_RoundTripsScopedFilesToolConfig(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewAgentRepo(db)
	ctx := context.Background()

	agent := &models.Agent{
		Name:         "Scoped Files Agent",
		SystemPrompt: "Work inside a restricted directory.",
		Model:        "inherit",
		Tools:        []string{models.AgentToolScopedFiles},
		ToolConfig: models.AgentToolConfig{
			ScopedFiles: []models.ScopedFilesConfig{{
				Directory:   "docs",
				Permissions: []string{"read", "write"},
			}},
			SkipDefaultTools:       true,
			DisableRuntimeWorktree: true,
		},
	}
	if err := repo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	stored, err := repo.GetByID(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if len(stored.Tools) != 1 || stored.Tools[0] != models.AgentToolScopedFiles {
		t.Fatalf("expected ScopedFiles tool, got %v", stored.Tools)
	}
	if !stored.ToolConfig.SkipDefaultTools {
		t.Fatal("expected scoped files config to disable default tools")
	}
	if !stored.ToolConfig.DisableRuntimeWorktree {
		t.Fatal("expected scoped files config to disable runtime worktrees")
	}
	if len(stored.ToolConfig.ScopedFiles) != 1 {
		t.Fatalf("expected one scoped files config, got %d", len(stored.ToolConfig.ScopedFiles))
	}
	scope := stored.ToolConfig.ScopedFiles[0]
	if scope.Directory != "docs" {
		t.Fatalf("expected docs scope, got %q", scope.Directory)
	}
	if len(scope.Permissions) != 2 || scope.Permissions[0] != "read" || scope.Permissions[1] != "write" {
		t.Fatalf("expected read/write permissions, got %v", scope.Permissions)
	}
}

func TestAgentRepo_GetUniqueSelectableByNameRequiresUniqueEnabledSelectableExactName(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewAgentRepo(db)
	ctx := context.Background()

	bob := &models.Agent{Name: "Bob", Key: "bob", Enabled: true, SelectableAsPrimary: true}
	if err := repo.Create(ctx, bob); err != nil {
		t.Fatalf("create Bob: %v", err)
	}
	got, err := repo.GetUniqueSelectableByName(ctx, "bob")
	if err != nil {
		t.Fatalf("GetUniqueSelectableByName: %v", err)
	}
	if got == nil || got.ID != bob.ID {
		t.Fatalf("expected Bob by exact case-insensitive name, got %+v", got)
	}

	disabled := &models.Agent{Name: "Disabled", Key: "disabled", Enabled: false, SelectableAsPrimary: true}
	if err := repo.Create(ctx, disabled); err != nil {
		t.Fatalf("create disabled: %v", err)
	}
	got, err = repo.GetUniqueSelectableByName(ctx, "Disabled")
	if err != nil {
		t.Fatalf("GetUniqueSelectableByName disabled: %v", err)
	}
	if got != nil {
		t.Fatalf("disabled agent must not be selectable, got %+v", got)
	}

	nonPrimary := &models.Agent{Name: "Helper", Key: "helper", Enabled: true, SelectableAsPrimary: false}
	if err := repo.Create(ctx, nonPrimary); err != nil {
		t.Fatalf("create helper: %v", err)
	}
	got, err = repo.GetUniqueSelectableByName(ctx, "Helper")
	if err != nil {
		t.Fatalf("GetUniqueSelectableByName helper: %v", err)
	}
	if got != nil {
		t.Fatalf("non-primary agent must not be selectable, got %+v", got)
	}

	dup := &models.Agent{Name: "Bob", Key: "bob_two", Enabled: true, SelectableAsPrimary: true}
	if err := repo.Create(ctx, dup); err != nil {
		t.Fatalf("create duplicate Bob: %v", err)
	}
	got, err = repo.GetUniqueSelectableByName(ctx, "Bob")
	if err != nil {
		t.Fatalf("GetUniqueSelectableByName duplicate: %v", err)
	}
	if got != nil {
		t.Fatalf("duplicate agent name must be ambiguous, got %+v", got)
	}
}

func TestAgentRepo_GetByKeyIncludingArchivedSeesArchivedRows(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := NewAgentRepo(db)
	ctx := context.Background()

	agent := &models.Agent{
		ID:              "archived-agent-id",
		Key:             "archived_agent",
		Name:            "Archived Agent",
		Enabled:         true,
		GeneratedStatus: models.AgentStatusArchived,
	}
	if err := repo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	live, err := repo.GetByKey(ctx, "archived_agent")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if live != nil {
		t.Fatalf("GetByKey should hide archived rows, got %+v", live)
	}

	archived, err := repo.GetByKeyIncludingArchived(ctx, "archived_agent")
	if err != nil {
		t.Fatalf("GetByKeyIncludingArchived: %v", err)
	}
	if archived == nil || archived.Key != "archived_agent" || archived.GeneratedStatus != models.AgentStatusArchived {
		t.Fatalf("expected archived row, got %+v", archived)
	}
}
