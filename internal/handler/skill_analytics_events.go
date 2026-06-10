package handler

import (
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/models"
)

func (h *Handler) recordManualSkillEdited(c echo.Context, handle, scope, agentID string) {
	if h == nil || h.skillAnalyticsRepo == nil || strings.TrimSpace(handle) == "" {
		return
	}
	normalizedScope := strings.TrimSpace(scope)
	switch normalizedScope {
	case models.SkillScopeAgentOwned:
		// keep
	case models.SkillScopeProject:
		normalizedScope = models.SkillScopeProject
	default:
		normalizedScope = models.SkillScopeGlobal
	}
	_ = h.skillAnalyticsRepo.RecordEvent(c.Request().Context(), &models.SkillAnalyticsEvent{
		ProjectID:   strings.TrimSpace(c.QueryParam("project_id")),
		AgentID:     agentID,
		SkillScope:  normalizedScope,
		SkillHandle: strings.TrimSpace(handle),
		EventType:   models.SkillEventEdited,
		Source:      models.SkillEventSourceManual,
		Surface:     models.SkillSurfaceTaskThread,
	})
}
