package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
	"github.com/openvibely/openvibely/web/templates/pages"
)

func (h *Handler) outboundTargetsData(c echo.Context) ([]models.ChannelTarget, bool) {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	return h.outboundTargetsDataForProject(c, projectID)
}

func (h *Handler) outboundTargetsDataForProject(c echo.Context, projectID string) ([]models.ChannelTarget, bool) {
	var targets []models.ChannelTarget
	if projectID != "" && h.channelTargetRepo != nil {
		targets, _ = h.channelTargetRepo.ListByProject(c.Request().Context(), projectID)
	}
	explicitAllowed := false
	if h.settingsRepo != nil && projectID != "" {
		val, _ := h.settingsRepo.Get(c.Request().Context(), service.SendMessageAllowExplicitTargetsSetting+":"+projectID)
		explicitAllowed = strings.EqualFold(strings.TrimSpace(val), "true")
	}
	return targets, explicitAllowed
}

func (h *Handler) handleOutboundTargetsFragment(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	targets, explicitAllowed := h.outboundTargetsDataForProject(c, projectID)
	return render(c, http.StatusOK, pages.OutboundTargetsFragment(projectID, targets, explicitAllowed, ""))
}

func (h *Handler) handleOutboundTargetsCardFragment(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	targets, explicitAllowed := h.outboundTargetsDataForProject(c, projectID)
	return render(c, http.StatusOK, pages.OutboundTargetsCardFragment(projectID, targets, explicitAllowed))
}

func (h *Handler) handleOutboundTargetSave(c echo.Context) error {
	if h.channelTargetRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Outbound target repository unavailable")
	}
	projectID := strings.TrimSpace(c.FormValue("project_id"))
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	platform := strings.ToLower(strings.TrimSpace(c.FormValue("platform")))
	name := strings.Trim(strings.ToLower(strings.TrimSpace(c.FormValue("name"))), "#")
	targetID := strings.TrimSpace(c.FormValue("target_id"))
	threadID := strings.TrimSpace(c.FormValue("thread_id"))
	defaultSubject := strings.TrimSpace(c.FormValue("default_subject"))
	if projectID == "" || platform == "" || targetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Project, platform, and target ID are required")
	}
	if platform != "slack" && platform != "telegram" && platform != "email" {
		return echo.NewHTTPError(http.StatusBadRequest, "Unsupported platform")
	}
	if platform == "email" {
		normalized, err := service.NormalizeOutboundEmailForTarget(targetID)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		targetID = normalized
	}
	id := strings.TrimSpace(c.FormValue("id"))
	if id == "" {
		id = repository.NewID()
	}
	if err := h.channelTargetRepo.Upsert(c.Request().Context(), models.ChannelTarget{ID: id, ProjectID: projectID, Platform: platform, Name: name, TargetID: targetID, ThreadID: threadID, Home: c.FormValue("is_home") == "true", DefaultSubject: defaultSubject}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save outbound target")
	}
	targets, explicitAllowed := h.outboundTargetsDataForProject(c, projectID)
	c.Response().Header().Set("HX-Trigger", "outbound-targets-card-refresh")
	return render(c, http.StatusOK, pages.OutboundTargetsFragment(projectID, targets, explicitAllowed, "Added outbound target."))
}

func (h *Handler) handleOutboundTargetDelete(c echo.Context) error {
	if h.channelTargetRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Outbound target repository unavailable")
	}
	if err := h.channelTargetRepo.Delete(c.Request().Context(), c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Outbound target not found")
	}
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	targets, explicitAllowed := h.outboundTargetsDataForProject(c, projectID)
	c.Response().Header().Set("HX-Trigger", "outbound-targets-card-refresh")
	return render(c, http.StatusOK, pages.OutboundTargetsFragment(projectID, targets, explicitAllowed, "Deleted outbound target."))
}

func (h *Handler) handleOutboundTargetTest(c echo.Context) error {
	if h.channelTargetRepo == nil || h.channelMessageRouter == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Outbound messaging is unavailable")
	}
	target, err := h.channelTargetRepo.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil || target == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Outbound target not found")
	}
	targetRef := fmt.Sprintf("%s:%s", target.Platform, target.TargetID)
	if strings.TrimSpace(target.ThreadID) != "" {
		targetRef += ":" + strings.TrimSpace(target.ThreadID)
	}
	result := h.channelMessageRouter.WithAuditContext("web", "test_button").Send(c.Request().Context(), target.ProjectID, service.SendMessageRequest{Target: targetRef, Message: "Test message from OpenVibely", Subject: target.DefaultSubject})
	b, _ := json.Marshal(result)
	escaped := html.EscapeString(string(b))
	if result.OK {
		return c.HTML(http.StatusOK, `<div class="text-success text-sm">Test sent: `+escaped+`</div>`)
	}
	return c.HTML(http.StatusOK, `<div class="text-error text-sm">Test failed: `+escaped+`</div>`)
}

func (h *Handler) handleSendMessageExplicitTargets(c echo.Context) error {
	projectID := strings.TrimSpace(c.FormValue("project_id"))
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	if h.settingsRepo != nil && projectID != "" {
		value := "false"
		if c.FormValue("enabled") == "true" {
			value = "true"
		}
		if err := h.settingsRepo.Set(c.Request().Context(), service.SendMessageAllowExplicitTargetsSetting+":"+projectID, value); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save setting")
		}
	}
	targets, explicitAllowed := h.outboundTargetsDataForProject(c, projectID)
	c.Response().Header().Set("HX-Trigger", "outbound-targets-card-refresh")
	return render(c, http.StatusOK, pages.OutboundTargetsFragment(projectID, targets, explicitAllowed, "Saved send_message target policy."))
}
