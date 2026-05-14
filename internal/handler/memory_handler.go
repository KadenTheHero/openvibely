package handler

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/memory"
)

// enqueueMemoryExtractionForChat fires a memory extraction pass for a
// completed chat/thread interaction. Safe to call when the memory service is
// not wired (no-op).
//
// Chat page interactions (interactive /chat and /api/chat/message) are
// intentionally NOT enqueued for memory extraction: their prompts are
// transient orchestration and mode-control text (Orchestrate/Plan, "Switch
// to Orchestrate", <proposed_plan>, etc.) that must never end up in durable
// project memory. Task-thread follow-ups remain eligible because they
// represent durable work against a real task.
func (h *Handler) enqueueMemoryExtractionForChat(params streamingResponseParams, output string) {
	if h.memorySvc == nil || params.ProjectID == "" {
		return
	}
	if !params.IsTaskFollowup {
		// Chat page surface — skip entirely. The memory package also rejects
		// SourceChat/SourceAPIChat defensively via ShouldExtract, but
		// short-circuiting here avoids creating a "nothing" run row for
		// every chat message and keeps the Schedule/Status UI clean.
		return
	}
	h.memorySvc.EnqueueExtraction(memory.Interaction{
		ProjectID:    params.ProjectID,
		SourceKind:   memory.SourceThread,
		SourceID:     params.ExecID,
		UserText:     params.Message,
		AssistantOut: output,
	})
}

// RunMemoryConsolidationNow performs a synchronous consolidation pass. This is
// available for settings/debug surfaces; scheduled execution uses the normal
// schedules table and is intercepted by SchedulerService.
func (h *Handler) RunMemoryConsolidationNow(c echo.Context) error {
	if h.memorySvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "memory service not available")
	}
	projectID, err := h.getCurrentProjectID(c)
	if err != nil {
		return err
	}
	if _, err := h.memorySvc.RunConsolidationNow(c.Request().Context(), projectID); err != nil {
		log.Printf("[handler] RunMemoryConsolidationNow: %v", err)
	}
	if isHTMX(c) {
		c.Response().Header().Set("HX-Refresh", "true")
		return c.NoContent(http.StatusOK)
	}
	return c.Redirect(http.StatusSeeOther, "/schedule?project_id="+projectID)
}

// UpdateMemorySettings toggles memory enabled/disabled per project.
func (h *Handler) UpdateMemorySettings(c echo.Context) error {
	if h.memorySvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "memory service not available")
	}
	projectID, err := h.getCurrentProjectID(c)
	if err != nil {
		return err
	}
	enabled := c.FormValue("enabled") == "on" || c.FormValue("enabled") == "true" || c.FormValue("enabled") == "1"
	if err := h.memorySvc.SetEnabled(c.Request().Context(), projectID, enabled); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
