package pages

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func renderTasksContentForSwarmTest(t *testing.T) string {
	t.Helper()

	project := &models.Project{ID: "project-1", Name: "Project"}
	var buf bytes.Buffer
	if err := TasksContent(project, nil, nil, nil, "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render tasks content: %v", err)
	}
	return buf.String()
}

func TestTasksContent_SwarmOptionsHiddenByDefault(t *testing.T) {
	body := renderTasksContentForSwarmTest(t)

	if !strings.Contains(body, `name="swarm_mode" id="create-task-swarm-mode-toggle"`) {
		t.Fatal("expected swarm mode toggle to expose the create-task visibility binding id")
	}
	if !strings.Contains(body, `id="create-task-swarm-options" class="hidden space-y-3"`) {
		t.Fatal("expected swarm sub-options container to be hidden by default")
	}

	optionsStart := strings.Index(body, `id="create-task-swarm-options"`)
	if optionsStart == -1 {
		t.Fatal("expected swarm sub-options container to render")
	}
	for _, field := range []string{
		"Autonomous planner",
		"Max workers",
		"Worker isolation",
		"Reviewer enabled",
		"Merger enabled",
	} {
		fieldIndex := strings.Index(body, field)
		if fieldIndex == -1 {
			t.Fatalf("expected %q to remain rendered inside the hidden swarm options section", field)
		}
		if fieldIndex < optionsStart {
			t.Fatalf("expected %q to render inside create-task-swarm-options", field)
		}
	}
}

func TestTasksContent_SwarmOptionsScriptTogglesVisibilityAndResyncsReset(t *testing.T) {
	body := renderTasksContentForSwarmTest(t)

	for _, want := range []string{
		`var toggle = document.getElementById('create-task-swarm-mode-toggle');`,
		`var options = document.getElementById('create-task-swarm-options');`,
		`function syncSwarmOptionsVisibility()`,
		`options.classList.toggle('hidden', !toggle.checked);`,
		`toggle.addEventListener('change', syncSwarmOptionsVisibility);`,
		`toggle.form?.addEventListener('reset', function()`,
		`setTimeout(syncSwarmOptionsVisibility, 0);`,
		`syncSwarmOptionsVisibility();`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected create-task swarm visibility script to contain %q", want)
		}
	}

	if strings.Contains(body, `swarmOptions.innerHTML = ''`) || strings.Contains(body, `swarmOptions.remove()`) {
		t.Fatal("swarm options should be hidden, not removed or emptied")
	}
}

func TestTasksContent_OpenNewTaskModalResyncsSwarmOptionsAfterFormReset(t *testing.T) {
	body := renderTasksContentForSwarmTest(t)

	resetIndex := strings.Index(body, `form.reset();`)
	if resetIndex == -1 {
		t.Fatal("expected openNewTaskModal to reset the create-task form")
	}
	resyncIndex := strings.Index(body[resetIndex:], `swarmOptions.classList.toggle('hidden', !swarmToggle.checked);`)
	if resyncIndex == -1 {
		t.Fatal("expected openNewTaskModal to resync swarm option visibility after reset")
	}
	if !strings.Contains(body[resetIndex:], `setTimeout(function()`) {
		t.Fatal("expected reset resync to run after native form.reset() has restored defaults")
	}
}

func TestTasksContent_HiddenSwarmOptionsPreserveSubmittedFieldValues(t *testing.T) {
	body := renderTasksContentForSwarmTest(t)

	swarmOptionControls := []string{
		`name="swarm_autonomous_planner" value="true" class="toggle toggle-sm toggle-primary" checked`,
		`name="swarm_max_workers" class="input input-bordered" min="1" max="8" value="3"`,
		`name="swarm_worker_isolation" class="select select-bordered"`,
		`name="swarm_reviewer_enabled" value="true" class="checkbox checkbox-sm" checked`,
		`name="swarm_merger_enabled" value="true" class="checkbox checkbox-sm" checked`,
	}
	for _, control := range swarmOptionControls {
		if !strings.Contains(body, control) {
			t.Fatalf("expected preserved swarm option control %q", control)
		}
	}

	for _, name := range []string{
		"swarm_autonomous_planner",
		"swarm_max_workers",
		"swarm_worker_isolation",
		"swarm_reviewer_enabled",
		"swarm_merger_enabled",
	} {
		disabledPattern := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `"[^>]*\sdisabled(?:[\s=>]|$)`)
		if disabledPattern.MatchString(body) {
			t.Fatalf("expected hidden swarm option %s to stay enabled so its value is preserved while hidden", name)
		}
	}
}

func TestTaskDetailEditForm_DoesNotRenderSwarmCreateControls(t *testing.T) {
	task := &models.Task{
		ID:        "task-1",
		Title:     "Task",
		ProjectID: "project-1",
		Status:    models.StatusPending,
		Category:  models.CategoryBacklog,
	}

	var buf bytes.Buffer
	if err := TaskDetailContent(task, nil, nil, nil, nil, nil, nil, "details", nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render task detail content: %v", err)
	}
	body := buf.String()

	for _, unexpected := range []string{
		`name="swarm_mode"`,
		`name="swarm_autonomous_planner"`,
		`name="swarm_max_workers"`,
		`name="swarm_worker_isolation"`,
		`name="swarm_reviewer_enabled"`,
		`name="swarm_merger_enabled"`,
		`id="create-task-swarm-options"`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("task edit form should not render create-task swarm control %q", unexpected)
		}
	}
}
