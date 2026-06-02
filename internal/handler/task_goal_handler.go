package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/service"
	"github.com/openvibely/openvibely/web/templates/pages"
)

type taskGoalResponse struct {
	OK   bool             `json:"ok"`
	Goal *models.TaskGoal `json:"goal"`
}

func wantsJSON(c echo.Context) bool {
	return c.Request().Header.Get("Accept") == "application/json"
}

func (h *Handler) renderTaskGoal(c echo.Context, taskID string, status int) error {
	if h.taskGoalSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "task goal service unavailable")
	}
	goal, err := h.taskGoalSvc.GetGoal(c.Request().Context(), taskID)
	if err != nil {
		return err
	}
	if wantsJSON(c) {
		return c.JSON(status, taskGoalResponse{OK: true, Goal: goal})
	}
	return render(c, status, pages.TaskGoalPanel(taskID, goal))
}

func (h *Handler) GetTaskGoal(c echo.Context) error {
	return h.renderTaskGoal(c, c.Param("taskId"), http.StatusOK)
}

func (h *Handler) SetTaskGoal(c echo.Context) error {
	if h.taskGoalSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "task goal service unavailable")
	}
	goal, err := h.taskGoalSvc.SetGoal(c.Request().Context(), c.Param("taskId"), c.FormValue("goal"), service.GoalOptions{Actor: "user"})
	if err != nil {
		if err == service.ErrTaskGoalEmpty || err == service.ErrTaskGoalTooLong {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return err
	}
	if wantsJSON(c) {
		return c.JSON(http.StatusOK, taskGoalResponse{OK: true, Goal: goal})
	}
	return render(c, http.StatusOK, pages.TaskGoalPanel(c.Param("taskId"), goal))
}

func (h *Handler) PauseTaskGoal(c echo.Context) error {
	if h.taskGoalSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "task goal service unavailable")
	}
	if err := h.taskGoalSvc.PauseGoal(c.Request().Context(), c.Param("taskId"), "user"); err != nil {
		if err == service.ErrTaskGoalNotFound {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	return h.renderTaskGoal(c, c.Param("taskId"), http.StatusOK)
}

func (h *Handler) ResumeTaskGoal(c echo.Context) error {
	if h.taskGoalSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "task goal service unavailable")
	}
	if err := h.taskGoalSvc.ResumeGoal(c.Request().Context(), c.Param("taskId"), "user"); err != nil {
		if err == service.ErrTaskGoalNotFound {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	return h.renderTaskGoal(c, c.Param("taskId"), http.StatusOK)
}

func (h *Handler) ClearTaskGoal(c echo.Context) error {
	if h.taskGoalSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "task goal service unavailable")
	}
	if err := h.taskGoalSvc.ClearGoal(c.Request().Context(), c.Param("taskId"), "user"); err != nil {
		return err
	}
	return h.renderTaskGoal(c, c.Param("taskId"), http.StatusOK)
}
