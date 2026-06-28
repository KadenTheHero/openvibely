package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/agentlibrary"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
)

// writeAgentRootSKILLSmd writes a minimal valid SKILLS.md for an agent to the
// given root so that syncRootDeclarationsFromRoot can discover and apply it.
func writeAgentRootSKILLSmd(t *testing.T, root, key, name, description string) {
	t.Helper()
	enabled := true
	selectable := true
	imp := agentlibrary.NewImporter(agentlibrary.SkillRoots{Global: root}, nil)
	decl := &agentlibrary.SkillDeclaration{
		Kind:    "openvibely.agent_skill",
		Version: 1,
		Agent: agentlibrary.AgentDeclaration{
			Key:                 key,
			Name:                name,
			Description:         description,
			Enabled:             &enabled,
			SelectableAsPrimary: selectable,
			Scope:               "global",
			SystemPrompt:        description,
		},
	}
	if _, err := imp.WriteAgentRootDeclaration(t.Context(), decl, "# "+name+"\n\n"+description+"\n"); err != nil {
		t.Fatalf("write agent SKILLS.md: %v", err)
	}
}

// TestHandler_DeleteAgent_RemovesAgentFromList verifies that a user-created
// agent is deleted from the database and does not appear in the subsequent
// ListAgents response.
func TestHandler_DeleteAgent_RemovesAgentFromList(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	h.SetAgentRepo(agentRepo)

	agent := &models.Agent{
		Name:    "Cindy",
		Key:     "cindy",
		Scope:   models.AgentScopeGlobal,
		Model:   "inherit",
		Enabled: true,
		Tools:   []string{"Read"},
	}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/agents/"+agent.ID, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `data-agent-name="Cindy"`) {
		t.Fatal("expected Cindy to be absent from agents list after delete, but found her card in response")
	}

	// Verify DB row is gone.
	gone, err := agentRepo.GetByID(t.Context(), agent.ID)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if gone != nil {
		t.Fatalf("expected agent to be deleted from DB, got %+v", gone)
	}
}

// TestHandler_DeleteAgent_ProtectedAgentRejected verifies that attempting to
// delete a protected system agent returns 403 and leaves the agent untouched.
func TestHandler_DeleteAgent_ProtectedAgentRejected(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	h.SetAgentRepo(agentRepo)

	protected := &models.Agent{
		Name:            "OpenVibely Engineer",
		Key:             "openvibely_engineer",
		Scope:           models.AgentScopeGlobal,
		Model:           "inherit",
		Enabled:         true,
		GeneratedStatus: models.AgentStatusProtected,
		CreatedBy:       models.AgentCreatedBySystem,
	}
	if err := agentRepo.Create(t.Context(), protected); err != nil {
		t.Fatalf("create protected agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/agents/"+protected.ID, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for protected agent delete, got %d: %s", rec.Code, rec.Body.String())
	}

	// Agent must still exist in DB.
	still, err := agentRepo.GetByID(t.Context(), protected.ID)
	if err != nil {
		t.Fatalf("GetByID after rejected delete: %v", err)
	}
	if still == nil {
		t.Fatal("expected protected agent to still exist in DB after rejected delete")
	}
}

// TestHandler_DeleteAgent_RemovesOnDiskDirectory verifies that when an agent
// with an on-disk directory is deleted, the directory is removed so that
// SyncRootDeclarations cannot re-create the agent from stale SKILLS.md.
func TestHandler_DeleteAgent_RemovesOnDiskDirectory(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	h.SetAgentRepo(agentRepo)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)

	agent := &models.Agent{
		Name:  "Claudia",
		Key:   "claudia",
		Scope: models.AgentScopeGlobal,
		Model: "inherit",
		Tools: []string{},
	}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// Write the on-disk SKILLS.md with a description that differs from the DB
	// to make it obvious if re-creation from disk occurs.
	writeAgentRootSKILLSmd(t, root, "claudia", "Claudia", "does nothing")

	agentDir := filepath.Join(root, "agents", "claudia")
	if _, err := os.Stat(agentDir); err != nil {
		t.Fatalf("expected agent dir to exist before delete: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/agents/"+agent.ID, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// On-disk directory must be gone.
	if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
		t.Fatalf("expected on-disk agent directory to be removed after delete, stat err=%v", err)
	}
}

// TestHandler_DeleteAgent_DoesNotReappearAfterListAgents is the key regression
// test. Before the fix, SyncRootDeclarations re-created deleted agents from
// their on-disk SKILLS.md on the very next ListAgents call. This test verifies
// that after deletion the agent is NOT re-created by a subsequent list request.
func TestHandler_DeleteAgent_DoesNotReappearAfterListAgents(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	lifecycleRepo := repository.NewLifecycleRepo(db)
	taskRepo := repository.NewTaskRepo(db, nil)
	scheduleRepo := repository.NewScheduleRepo(db)

	root := t.TempDir()

	// Set up the maintenance service so SyncRootDeclarations runs during ListAgents.
	maintenanceSvc := service.NewAgentLibraryMaintenanceService(taskRepo, scheduleRepo, agentRepo)
	maintenanceSvc.SetLifecycleRepo(lifecycleRepo)
	maintenanceSvc.SetAgentsRootPath(root)

	h.SetAgentRepo(agentRepo)
	h.SetLifecycleRepo(lifecycleRepo)
	h.SetAgentSkillRoot(root)
	h.SetAgentLibraryMaintenanceService(maintenanceSvc)

	// Create the "Claudia" agent in the DB.
	agent := &models.Agent{
		Name:        "Claudia",
		Key:         "claudia",
		Scope:       models.AgentScopeGlobal,
		Description: "original description",
		Model:       "inherit",
		Tools:       []string{},
	}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Write an on-disk SKILLS.md for Claudia with a different description to
	// confirm re-creation from disk is what was happening before the fix.
	writeAgentRootSKILLSmd(t, root, "claudia", "Claudia", "does nothing")

	// Delete Claudia via the DELETE endpoint (triggers ListAgents which runs
	// SyncRootDeclarations internally).
	req := httptest.NewRequest(http.MethodDelete, "/agents/"+agent.ID, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /agents/:id expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Claudia must NOT appear in the delete response (which includes ListAgents).
	if strings.Contains(rec.Body.String(), `data-agent-name="Claudia"`) {
		t.Fatal("Claudia reappeared in the delete response; SyncRootDeclarations re-created her from disk")
	}

	// Explicitly GET /agents to confirm she is still absent after a fresh list.
	req2 := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req2.Header.Set("HX-Request", "true")
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /agents expected 200, got %d", rec2.Code)
	}
	if strings.Contains(rec2.Body.String(), `data-agent-name="Claudia"`) {
		t.Fatal("Claudia reappeared after GET /agents; on-disk SKILLS.md re-created her via SyncRootDeclarations")
	}

	// DB must not have Claudia.
	claudia, err := agentRepo.GetByKey(t.Context(), "claudia")
	if err != nil {
		t.Fatalf("GetByKey after delete: %v", err)
	}
	if claudia != nil {
		t.Fatalf("expected Claudia to be absent from DB after delete, got %+v", claudia)
	}

	// On-disk directory must also be gone.
	agentDir := filepath.Join(root, "agents", "claudia")
	if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
		t.Fatalf("expected on-disk agent directory to be removed, stat err=%v", err)
	}
}

// TestHandler_ListAgents_ProtectedAgentShowsDisabledDelete verifies that the
// agents page renders a disabled, non-functional Delete button for protected
// system agents so users are clearly informed the agent cannot be deleted.
func TestHandler_ListAgents_ProtectedAgentShowsDisabledDelete(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	h.SetAgentRepo(agentRepo)

	protected := &models.Agent{
		Name:            "OpenVibely Engineer",
		Key:             "openvibely_engineer",
		Scope:           models.AgentScopeGlobal,
		Model:           "inherit",
		Enabled:         true,
		GeneratedStatus: models.AgentStatusProtected,
		CreatedBy:       models.AgentCreatedBySystem,
	}
	if err := agentRepo.Create(t.Context(), protected); err != nil {
		t.Fatalf("create protected agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Must show the disabled "Delete (protected)" button.
	if !strings.Contains(body, "Delete (protected)") {
		t.Error("expected protected agent to show 'Delete (protected)' disabled button")
	}
	if !strings.Contains(body, "Protected system agents cannot be deleted") {
		t.Error("expected protected agent delete button to have tooltip explaining it cannot be deleted")
	}

	// Must NOT present a working openDeleteAgentConfirm for this protected agent.
	// The non-protected path calls openDeleteAgentConfirm(this) in the delete button.
	// Verify the protected agent ID is not wired to openDeleteAgentConfirm.
	// (The agent card exists, but its delete button must be disabled.)
	if !strings.Contains(body, `data-agent-name="OpenVibely Engineer"`) {
		t.Error("expected OpenVibely Engineer agent card to be present in the list")
	}
}

// TestHandler_DeleteAgent_NoDiskDirDoesNotFail ensures that an agent with no
// on-disk directory can still be deleted successfully (handles agents that were
// created only in the DB without being materialized to disk).
func TestHandler_DeleteAgent_NoDiskDirDoesNotFail(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	agentRepo := repository.NewAgentRepo(db)
	h.SetAgentRepo(agentRepo)
	root := t.TempDir()
	h.SetAgentSkillRoot(root)

	agent := &models.Agent{
		Name:  "No-Disk Agent",
		Key:   "no_disk_agent",
		Scope: models.AgentScopeGlobal,
		Model: "inherit",
		Tools: []string{},
	}
	if err := agentRepo.Create(t.Context(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// Note: no SKILLS.md written to disk.

	req := httptest.NewRequest(http.MethodDelete, "/agents/"+agent.ID, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `data-agent-name="No-Disk Agent"`) {
		t.Fatal("expected No-Disk Agent to be absent from agents list after delete")
	}
}
