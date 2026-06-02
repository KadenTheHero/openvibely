package handler

import (
	"testing"

	"github.com/openvibely/openvibely/internal/chatcontrol"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func TestFilterTaskThreadRuntimeToolDefs_GoalStatusToolsRequireExplicitGrant(t *testing.T) {
	defs := chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, true)

	ungranted := toolDefNameSet(filterTaskThreadRuntimeToolDefs(defs, nil))
	if ungranted["mark_task_goal_achieved"] || ungranted["report_task_goal_blocked"] {
		t.Fatalf("ungranted task agent got goal status tools: %+v", ungranted)
	}
	if !ungranted["get_task_goal"] || !ungranted["send_to_task"] {
		t.Fatalf("base task goal tools missing for ungranted agent: %+v", ungranted)
	}

	agentDef := &models.Agent{Tools: []string{"mark_task_goal_achieved"}}
	granted := toolDefNameSet(filterTaskThreadRuntimeToolDefs(defs, agentDef))
	if !granted["mark_task_goal_achieved"] {
		t.Fatalf("explicitly granted goal achieved tool missing: %+v", granted)
	}
	if granted["report_task_goal_blocked"] {
		t.Fatalf("ungranted blocked-report tool was exposed: %+v", granted)
	}
}

func TestFilterTaskThreadCapabilitySummaries_GoalStatusToolsRequireExplicitGrant(t *testing.T) {
	summaries := chatcontrol.ListForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)

	ungranted := capabilityNameSet(filterTaskThreadCapabilitySummaries(summaries, nil))
	if ungranted["mark_task_goal_achieved"] || ungranted["report_task_goal_blocked"] {
		t.Fatalf("ungranted task agent capabilities advertised goal status tools: %+v", ungranted)
	}
	if !ungranted["get_task_goal"] || !ungranted["send_to_task"] || !ungranted["list_capabilities"] {
		t.Fatalf("base task-thread capabilities missing for ungranted agent: %+v", ungranted)
	}

	agentDef := &models.Agent{Tools: []string{"report_task_goal_blocked"}}
	granted := capabilityNameSet(filterTaskThreadCapabilitySummaries(summaries, agentDef))
	if !granted["report_task_goal_blocked"] {
		t.Fatalf("explicitly granted blocked-report capability missing: %+v", granted)
	}
	if granted["mark_task_goal_achieved"] {
		t.Fatalf("ungranted achieved capability was advertised: %+v", granted)
	}
}

func toolDefNameSet(defs []llmcontracts.RuntimeToolDefinition) map[string]bool {
	out := make(map[string]bool, len(defs))
	for _, def := range defs {
		out[def.Name] = true
	}
	return out
}

func capabilityNameSet(summaries []chatcontrol.ActionSummary) map[string]bool {
	out := make(map[string]bool, len(summaries))
	for _, summary := range summaries {
		out[summary.Name] = true
	}
	return out
}
