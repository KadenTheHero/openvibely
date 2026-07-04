package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/openvibely/openvibely/internal/models"
)

// GetSwarm returns a swarm parent and its child task summary.
// @Summary Get task swarm
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} models.Task
// @Router /api/tasks/{id}/swarm [get]
func (h *Handler) GetSwarm(c echo.Context) error {
	id := c.Param("id")
	parent, err := h.taskRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if parent == nil {
		return echo.NewHTTPError(http.StatusNotFound, "task not found")
	}
	children, err := h.taskRepo.ListSwarmChildren(c.Request().Context(), id)
	if err != nil {
		return err
	}
	parent.SwarmChildren = children
	return c.JSON(http.StatusOK, parent)
}

// StartSwarm starts or resumes the planner for a swarm parent task.
// @Summary Start swarm planner
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} map[string]string
// @Router /api/tasks/{id}/swarm/start [post]
func (h *Handler) StartSwarm(c echo.Context) error {
	if h.swarmSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "swarm service unavailable")
	}
	if err := h.swarmSvc.StartPlanner(c.Request().Context(), c.Param("id")); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "planner_started"})
}

// SwarmFollowup records swarm follow-up routing metadata.
// @Summary Send swarm follow-up
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Param message formData string false "Follow-up message"
// @Success 200 {object} map[string]string
// @Router /api/tasks/{id}/swarm/followup [post]
func (h *Handler) SwarmFollowup(c echo.Context) error {
	if h.swarmSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "swarm service unavailable")
	}
	id := c.Param("id")
	task, err := h.taskRepo.GetByID(c.Request().Context(), id)
	if err != nil || task == nil {
		return echo.NewHTTPError(http.StatusNotFound, "task not found")
	}
	message := c.FormValue("message")
	if task.SwarmRole == models.SwarmRoleParent {
		err = h.swarmSvc.HandleParentFollowup(c.Request().Context(), id, message)
	} else {
		err = h.swarmSvc.HandleChildFollowup(c.Request().Context(), id, message)
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "accepted"})
}

// CancelSwarm cancels a swarm parent and runnable/running children.
// @Summary Cancel swarm
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} map[string]string
// @Router /api/tasks/{id}/swarm/cancel [post]
func (h *Handler) CancelSwarm(c echo.Context) error {
	if h.swarmSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "swarm service unavailable")
	}
	if err := h.swarmSvc.CancelSwarm(c.Request().Context(), c.Param("id")); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "cancelled"})
}

// RerunSwarmReviewer reruns the reviewer child task.
// @Summary Rerun swarm reviewer
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} map[string]string
// @Router /api/tasks/{id}/swarm/rerun-reviewer [post]
func (h *Handler) RerunSwarmReviewer(c echo.Context) error {
	return h.rerunSwarmRole(c, models.SwarmRoleReviewer)
}

// RerunSwarmIntegrator reruns the integrator child task.
// @Summary Rerun swarm integrator
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} map[string]string
// @Router /api/tasks/{id}/swarm/rerun-integrator [post]
func (h *Handler) RerunSwarmIntegrator(c echo.Context) error {
	return h.rerunSwarmRole(c, models.SwarmRoleIntegrator)
}

func (h *Handler) rerunSwarmRole(c echo.Context, role models.SwarmRole) error {
	child, err := h.taskRepo.FindSwarmChildByRole(c.Request().Context(), c.Param("id"), role)
	if err != nil {
		return err
	}
	if child == nil {
		return echo.NewHTTPError(http.StatusNotFound, "swarm role task not found")
	}
	if err := h.taskRepo.UpdateStatus(c.Request().Context(), child.ID, models.StatusPending); err != nil {
		return err
	}
	child.Status = models.StatusPending
	if h.workerSvc != nil {
		h.workerSvc.Submit(*child)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "started", "task_id": child.ID})
}
