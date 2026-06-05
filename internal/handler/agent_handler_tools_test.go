package handler

import (
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestDefaultAgentToolsIncludesSelectedMemoryView(t *testing.T) {
	tools := defaultAgentTools()
	if !agentToolsInclude(tools, "memory_view") {
		t.Fatalf("defaultAgentTools() = %#v, missing memory_view", tools)
	}
	if !agentToolsInclude(models.AllAgentTools, "memory_view") {
		t.Fatalf("models.AllAgentTools missing memory_view")
	}
}

func TestNormalizeAgentToolsKeepsTaskGoalRuntimeTools(t *testing.T) {
	input := []string{
		"get_task_goal",
		"mark_task_goal_achieved",
		"report_task_goal_blocked",
		"send_to_task",
	}

	tools := normalizeAgentTools(input)
	for _, want := range input {
		if !agentToolsInclude(tools, want) {
			t.Fatalf("normalizeAgentTools() = %#v, missing %q", tools, want)
		}
	}

	for _, want := range []string{
		"set_task_goal",
		"clear_task_goal",
		"pause_task_goal",
		"resume_task_goal",
	} {
		if !agentToolsInclude(models.AllAgentTools, want) {
			t.Fatalf("models.AllAgentTools missing %q", want)
		}
	}
}
