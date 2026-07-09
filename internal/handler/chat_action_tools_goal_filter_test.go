package handler

import (
	"testing"

	"github.com/openvibely/openvibely/internal/chatcontrol"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func TestFilterAssignedAgentRuntimeToolDefs_IncludesSendMessageByDefault(t *testing.T) {
	defs := chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, true)

	filtered := toolDefNameSet(filterAssignedAgentRuntimeToolDefs(defs, &models.Agent{Tools: []string{"Read"}}))
	if !filtered["send_message"] {
		t.Fatalf("assigned task agents should get send_message by default, got %+v", filtered)
	}
	if !filtered["create_task"] {
		t.Fatalf("assigned-agent filter should preserve unrelated chat tools, got %+v", filtered)
	}
}

func TestFilterAssignedAgentCapabilitySummaries_IncludesSendMessageByDefault(t *testing.T) {
	summaries := chatcontrol.ListForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)

	filtered := capabilityNameSet(filterAssignedAgentCapabilitySummaries(summaries, &models.Agent{Tools: []string{"Read"}}))
	if !filtered["send_message"] {
		t.Fatalf("assigned task agents should advertise send_message by default, got %+v", filtered)
	}
	if !filtered["create_task"] {
		t.Fatalf("assigned-agent capability filter should preserve unrelated capabilities, got %+v", filtered)
	}
}

func TestFilterTaskThreadRuntimeToolDefs_GoalStatusToolsRequireExplicitGrant(t *testing.T) {
	defs := chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, true)

	ungranted := toolDefNameSet(filterTaskThreadRuntimeToolDefs(defs, nil, false))
	if ungranted["mark_task_goal_achieved"] || ungranted["report_task_goal_blocked"] {
		t.Fatalf("ungranted task agent got goal status tools: %+v", ungranted)
	}
	if !ungranted["get_task_goal"] || !ungranted["send_to_task"] {
		t.Fatalf("base task goal tools missing for ungranted agent: %+v", ungranted)
	}
	if !ungranted["send_message"] {
		t.Fatalf("task agents should get send_message by default: %+v", ungranted)
	}
	if !ungranted["github_get_issue"] || !ungranted["github_get_project_inbox"] || !ungranted["github_is_actor_authorized"] || !ungranted["github_list_my_assigned_issues"] || !ungranted["github_list_assigned_issues"] || !ungranted["github_open_pull_request"] {
		t.Fatalf("task agents should get GitHub issue tools by default: %+v", ungranted)
	}
	if ungranted["memory_view"] {
		t.Fatalf("unselected memory_view runtime tool was exposed: %+v", ungranted)
	}
	withMemory := toolDefNameSet(filterTaskThreadRuntimeToolDefs(defs, nil, true))
	if !withMemory["memory_view"] {
		t.Fatalf("selected memory_view runtime tool missing: %+v", withMemory)
	}

	agentDef := &models.Agent{Tools: []string{"mark_task_goal_achieved", "send_message"}}
	granted := toolDefNameSet(filterTaskThreadRuntimeToolDefs(defs, agentDef, false))
	if !granted["mark_task_goal_achieved"] {
		t.Fatalf("explicitly granted goal achieved tool missing: %+v", granted)
	}
	if !granted["send_message"] {
		t.Fatalf("explicitly granted send_message tool missing: %+v", granted)
	}
	if granted["report_task_goal_blocked"] {
		t.Fatalf("ungranted blocked-report tool was exposed: %+v", granted)
	}
}

func TestFilterTaskThreadRuntimeToolDefs_HaveWebHandlers(t *testing.T) {
	defs := filterTaskThreadRuntimeToolDefs(chatcontrol.ToolDefsForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb, true), nil, true)
	handlers := (&Handler{}).chatActionHandlers(streamingResponseParams{ExecID: "exec", ProjectID: "project", IsTaskFollowup: true}, nil, models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)
	advertised := toolDefNameSet(defs)

	for _, name := range []string{
		"github_get_project_inbox",
		"github_list_my_assigned_issues",
		"github_list_assigned_issues",
		"github_list_assigned_issues_with_prs",
		"github_open_pull_request",
	} {
		if !advertised[name] {
			t.Fatalf("task-thread web runtime did not advertise required GitHub tool %s", name)
		}
		if _, ok := handlers[name]; !ok {
			t.Fatalf("task-thread web runtime advertised %s without a handler", name)
		}
	}

	for _, def := range defs {
		if def.Name == "memory_view" {
			continue
		}
		if _, ok := handlers[def.Name]; !ok {
			t.Fatalf("task-thread web runtime advertised %s without a handler", def.Name)
		}
	}
}

func TestFilterTaskThreadCapabilitySummaries_GoalStatusToolsRequireExplicitGrant(t *testing.T) {
	summaries := chatcontrol.ListForContext(models.ChatModeOrchestrate, chatcontrol.SurfaceWeb)

	ungranted := capabilityNameSet(filterTaskThreadCapabilitySummaries(summaries, nil, false))
	if ungranted["mark_task_goal_achieved"] || ungranted["report_task_goal_blocked"] {
		t.Fatalf("ungranted task agent capabilities advertised goal status tools: %+v", ungranted)
	}
	if ungranted["memory_view"] {
		t.Fatalf("unselected memory_view capability was advertised: %+v", ungranted)
	}
	if !ungranted["get_task_goal"] || !ungranted["send_to_task"] || !ungranted["list_capabilities"] {
		t.Fatalf("base task-thread capabilities missing for ungranted agent: %+v", ungranted)
	}
	if !ungranted["send_message"] {
		t.Fatalf("task agents should advertise send_message by default: %+v", ungranted)
	}
	if !ungranted["github_get_issue"] || !ungranted["github_get_project_inbox"] || !ungranted["github_is_actor_authorized"] || !ungranted["github_list_my_assigned_issues"] || !ungranted["github_list_assigned_issues"] || !ungranted["github_open_pull_request"] {
		t.Fatalf("task agents should advertise GitHub issue tools by default: %+v", ungranted)
	}

	withMemory := capabilityNameSet(filterTaskThreadCapabilitySummaries(summaries, nil, true))
	if !withMemory["memory_view"] {
		t.Fatalf("selected memory_view capability missing: %+v", withMemory)
	}

	agentDef := &models.Agent{Tools: []string{"report_task_goal_blocked", "send_message"}}
	granted := capabilityNameSet(filterTaskThreadCapabilitySummaries(summaries, agentDef, false))
	if !granted["report_task_goal_blocked"] {
		t.Fatalf("explicitly granted blocked-report capability missing: %+v", granted)
	}
	if !granted["send_message"] {
		t.Fatalf("explicitly granted send_message capability missing: %+v", granted)
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
