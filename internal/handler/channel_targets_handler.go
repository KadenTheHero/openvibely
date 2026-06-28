package handler

import (
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

func (h *Handler) handleOutboundTargetTest(c echo.Context) error {
	if h.channelTargetRepo == nil || h.channelMessageRouter == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Outbound messaging is unavailable")
	}
	projectID, target, err := h.outboundTargetForRequestProject(c)
	if err != nil || target == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Outbound target not found")
	}
	targetRef := fmt.Sprintf("%s:%s", target.Platform, target.TargetID)
	if strings.TrimSpace(target.ThreadID) != "" {
		targetRef += ":" + strings.TrimSpace(target.ThreadID)
	}
	result := h.channelMessageRouter.WithAuditContext("web", "test_button").Send(c.Request().Context(), projectID, service.SendMessageRequest{Target: targetRef, Message: "Test message from OpenVibely", Subject: target.DefaultSubject})
	return outboundTargetTestResultHTML(c, result)
}

func (h *Handler) handleOutboundTargetDraftTest(c echo.Context) error {
	if h.channelMessageRouter == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Outbound messaging is unavailable")
	}
	projectID := strings.TrimSpace(c.FormValue("project_id"))
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Project is required")
	}
	target := service.ChannelTarget{
		ProjectID:      projectID,
		Platform:       strings.TrimSpace(c.FormValue("target_platform")),
		TargetID:       strings.TrimSpace(c.FormValue("target_target_id")),
		ThreadID:       strings.TrimSpace(c.FormValue("target_thread_id")),
		DefaultSubject: strings.TrimSpace(c.FormValue("target_default_subject")),
	}
	result := h.channelMessageRouter.WithAuditContext("web", "test_button").SendDirectTarget(c.Request().Context(), projectID, target, service.SendMessageRequest{Message: "Test message from OpenVibely", Subject: target.DefaultSubject})
	return outboundTargetTestResultHTML(c, result)
}

func outboundTargetTestResultHTML(c echo.Context, result service.SendMessageResult) error {
	if result.OK {
		return c.HTML(http.StatusOK, `<span class="text-success font-semibold" title="Test sent.">Sent</span>`)
	}
	message := strings.TrimSpace(result.Error)
	if message == "" {
		message = "Provider did not accept the test message"
	}
	return c.HTML(http.StatusOK, `<span class="text-error font-semibold" title="Test failed: `+html.EscapeString(message)+`">Failed</span>`)
}

func (h *Handler) outboundTargetForRequestProject(c echo.Context) (string, *models.ChannelTarget, error) {
	projectID := strings.TrimSpace(c.QueryParam("project_id"))
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	if projectID == "" {
		return "", nil, fmt.Errorf("project is required")
	}
	target, err := h.channelTargetRepo.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil || target == nil {
		return projectID, nil, err
	}
	if target.ProjectID != projectID {
		return projectID, nil, fmt.Errorf("outbound target not found")
	}
	return projectID, target, nil
}

func (h *Handler) handleSendMessageExplicitTargets(c echo.Context) error {
	projectID := strings.TrimSpace(c.FormValue("project_id"))
	if projectID == "" {
		projectID, _ = h.getCurrentProjectID(c)
	}
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Project is required")
	}
	if h.channelTargetRepo != nil {
		if err := h.saveOutboundTargetsDraft(c, projectID); err != nil {
			if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code == http.StatusNotFound {
				return err
			}
			targets, explicitAllowed := h.outboundTargetsDataForProject(c, projectID)
			c.Response().Header().Set("HX-Trigger", "outbound-targets-save-error")
			return render(c, http.StatusOK, pages.OutboundTargetsFragment(projectID, targets, explicitAllowed, outboundTargetSaveErrorMessage(err)))
		}
	}
	if h.settingsRepo != nil {
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
	return render(c, http.StatusOK, pages.OutboundTargetsFragment(projectID, targets, explicitAllowed, "Saved outbound message targets."))
}

func outboundTargetSaveErrorMessage(err error) string {
	if err == nil {
		return "Failed to save outbound targets."
	}
	if httpErr, ok := err.(*echo.HTTPError); ok {
		if msg, ok := httpErr.Message.(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	return "Failed to save outbound targets."
}

func (h *Handler) saveOutboundTargetsDraft(c echo.Context, projectID string) error {
	if err := c.Request().ParseForm(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid outbound target form")
	}
	form := c.Request().PostForm
	ids := form["target_row_id"]
	platforms := form["target_platform"]
	names := form["target_name"]
	targetIDs := form["target_target_id"]
	threadIDs := form["target_thread_id"]
	homes := form["target_is_home"]
	defaultSubjects := form["target_default_subject"]
	rowCount := len(platforms)
	if len(ids) != rowCount || len(names) != rowCount || len(targetIDs) != rowCount || len(threadIDs) != rowCount || len(homes) != rowCount || len(defaultSubjects) != rowCount {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid outbound target form")
	}
	targets := make([]models.ChannelTarget, 0, rowCount)
	seenNames := make(map[string]struct{})
	seenDestinations := make(map[string]struct{})
	for i := 0; i < rowCount; i++ {
		platform := strings.ToLower(strings.TrimSpace(platforms[i]))
		name := strings.Trim(strings.ToLower(strings.TrimSpace(names[i])), "#")
		targetID := strings.TrimSpace(targetIDs[i])
		threadID := strings.TrimSpace(threadIDs[i])
		defaultSubject := strings.TrimSpace(defaultSubjects[i])
		if platform == "" && targetID == "" {
			continue
		}
		if platform != "slack" && platform != "telegram" && platform != "email" && platform != "discord" {
			return echo.NewHTTPError(http.StatusBadRequest, "Unsupported platform")
		}
		if targetID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Target ID is required")
		}
		if platform == "email" {
			normalized, err := service.NormalizeOutboundEmailForTarget(targetID)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
			targetID = normalized
		}
		if name != "" {
			nameKey := platform + "\x00" + name
			if _, exists := seenNames[nameKey]; exists {
				return echo.NewHTTPError(http.StatusBadRequest, "Duplicate outbound target name for "+platform)
			}
			seenNames[nameKey] = struct{}{}
		}
		destinationKey := platform + "\x00" + targetID + "\x00" + threadID
		if _, exists := seenDestinations[destinationKey]; exists {
			return echo.NewHTTPError(http.StatusBadRequest, "Duplicate outbound target destination")
		}
		seenDestinations[destinationKey] = struct{}{}
		id := strings.TrimSpace(ids[i])
		if id == "" {
			id = repository.NewID()
		} else {
			existing, err := h.channelTargetRepo.GetByID(c.Request().Context(), id)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load outbound target")
			}
			if existing != nil && existing.ProjectID != projectID {
				return echo.NewHTTPError(http.StatusNotFound, "Outbound target not found")
			}
		}
		targets = append(targets, models.ChannelTarget{ID: id, ProjectID: projectID, Platform: platform, Name: name, TargetID: targetID, ThreadID: threadID, Home: strings.EqualFold(strings.TrimSpace(homes[i]), "true"), DefaultSubject: defaultSubject})
	}
	if err := h.channelTargetRepo.ReplaceProjectTargets(c.Request().Context(), projectID, targets); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save outbound targets")
	}
	return nil
}
