package handler

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/web/templates/components"
)

// ListEmailAuthorizedSenders returns the authorized email senders list for a project.
func (h *Handler) ListEmailAuthorizedSenders(c echo.Context) error {
	if h.emailAuthRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Email auth not configured")
	}
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	senders, err := h.emailAuthRepo.ListByProject(c.Request().Context(), projectID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load authorized senders")
	}
	return render(c, http.StatusOK, components.EmailAuthorizedSendersList(senders, projectID))
}

// AddEmailAuthorizedSender adds a new authorized email sender.
func (h *Handler) AddEmailAuthorizedSender(c echo.Context) error {
	if h.emailAuthRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Email auth not configured")
	}
	projectID := c.FormValue("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	emailAddress := repository.NormalizeEmailAddress(c.FormValue("authorized_email_address"))
	displayName := strings.TrimSpace(c.FormValue("display_name"))
	if emailAddress == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Email address is required")
	}
	if displayName == "" {
		displayName = emailAddress
	}
	sender := &models.EmailAuthorizedSender{
		ProjectID:    projectID,
		EmailAddress: emailAddress,
		DisplayName:  displayName,
		AddedBy:      "web",
	}
	if err := h.emailAuthRepo.Create(c.Request().Context(), sender); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to add authorized sender: "+err.Error())
	}
	senders, err := h.emailAuthRepo.ListByProject(c.Request().Context(), projectID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load authorized senders")
	}
	return render(c, http.StatusOK, components.EmailAuthorizedSendersList(senders, projectID))
}

// RemoveEmailAuthorizedSender removes an authorized email sender.
func (h *Handler) RemoveEmailAuthorizedSender(c echo.Context) error {
	if h.emailAuthRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Email auth not configured")
	}
	id := c.Param("id")
	projectID := c.QueryParam("project_id")
	sender, err := h.emailAuthRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find sender")
	}
	if sender == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Sender not found")
	}
	if projectID == "" {
		projectID = sender.ProjectID
	}
	if err := h.emailAuthRepo.Delete(c.Request().Context(), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to remove sender: "+err.Error())
	}
	senders, err := h.emailAuthRepo.ListByProject(c.Request().Context(), projectID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load authorized senders")
	}
	return render(c, http.StatusOK, components.EmailAuthorizedSendersList(senders, projectID))
}
