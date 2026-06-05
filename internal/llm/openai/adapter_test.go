package openai

import (
	"encoding/json"
	"strings"
	"testing"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func TestToolSecondaryInfo_LongBashPreservesLaterContext(t *testing.T) {
	input := map[string]any{
		"command": "cd /Users/dubee/go/src/github.com/openvibely/openvibely/.worktrees/task_6a40e9f8fefa53ac8d203aa3fd3a70be && rg -n \"toolSecondaryInfo|truncateToolSecondary|task thread\" internal pkg web/templates/components/chat_shared.templ",
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	got := toolSecondaryInfo("bash", raw)
	if !strings.HasPrefix(got, "$ cd ") {
		t.Fatalf("expected bash detail prefix, got %q", got)
	}
	if !strings.Contains(got, "chat_shared.templ") {
		t.Fatalf("expected later command context to survive truncation, got %q", got)
	}
}

func TestToolSecondaryInfo_LongGrepPreservesLaterPatternContext(t *testing.T) {
	input := map[string]any{
		"pattern": "len\\(cmd\\) >|len\\(p\\) >|toolSecondaryInfo|truncateToolSecondary|task thread|chat_shared\\.templ|stream/events\\.go",
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	got := toolSecondaryInfo("grep_search", raw)
	if !strings.Contains(got, "chat_shared") {
		t.Fatalf("expected later grep context to survive truncation, got %q", got)
	}
}

func TestToolSecondaryInfo_WebSearchUsesURLFallback(t *testing.T) {
	input := map[string]any{
		"url": "https://www.crunchydata.com/blog/postgres-is-out-of-disk-and-how-to-recover-the-dos-and-donts",
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	got := toolSecondaryInfo("web_search", raw)
	if !strings.Contains(got, "crunchydata.com/blog/postgres-is-out-of-disk") {
		t.Fatalf("expected web_search secondary to include url, got %q", got)
	}
}

func TestToolSecondaryInfo_WebSearchFindInPageDetail(t *testing.T) {
	input := map[string]any{
		"action":  "findInPage",
		"pattern": "WAL files",
		"url":     "https://www.crunchydata.com/blog/postgres-is-out-of-disk-and-how-to-recover-the-dos-and-donts",
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	got := toolSecondaryInfo("web_search", raw)
	if !strings.Contains(got, "'WAL files' in https://www.crunchydata.com/blog/") {
		t.Fatalf("expected find-in-page detail, got %q", got)
	}
}

func TestRuntimeToolFilter_AllowsFilteredReadOnlyRuntimeToolInPlanMode(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "memory_view", Description: "read selected memory", Parameters: json.RawMessage(`{"type":"object"}`), Access: llmcontracts.RuntimeToolAccessRead},
			{Name: "write_file", Description: "write selected file", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		Filter: func(name string) (bool, bool) {
			switch name {
			case "memory_view", "write_file":
				return true, true
			default:
				return false, false
			}
		},
	}
	filter := composeRuntimeToolFilter(nil, rt, false, models.ChatModePlan)
	if !filter("memory_view") {
		t.Fatal("expected filtered selected-memory runtime tools to be allowed in plan mode")
	}
	if filter("write_file") {
		t.Fatal("did not expect unrelated runtime action tool in plan mode")
	}
}

func TestRuntimeToolFilter_OrchestrateAllowsMemoryViewWithoutFilesystemRead(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "memory_view", Description: "read selected memory", Parameters: json.RawMessage(`{"type":"object"}`), Access: llmcontracts.RuntimeToolAccessRead},
			{Name: "create_task", Description: "create task", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		Filter: func(name string) (bool, bool) {
			switch name {
			case "memory_view", "create_task":
				return true, true
			default:
				return false, false
			}
		},
	}
	filter := composeRuntimeToolFilter(func(name string) bool { return true }, rt, false, models.ChatModeOrchestrate)
	if !filter("memory_view") {
		t.Fatal("expected selected-memory runtime tools to be allowed in orchestrate mode")
	}
	if !filter("create_task") {
		t.Fatal("expected action runtime tool to be allowed in orchestrate mode")
	}
	if filter("read_file") || filter("Read") || filter("list_files") {
		t.Fatal("did not expect filesystem/default read tools to be allowed in orchestrate mode")
	}
}

func TestRuntimeToolFilter_SkipDefaultToolsAllowsOnlyRuntimeTools(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		SkipDefaultTools: true,
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "read_file"},
		},
		Filter: func(name string) (bool, bool) {
			if name == "read_file" {
				return true, true
			}
			return false, true
		},
	}
	filter := composeRuntimeToolFilter(nil, rt, true, models.ChatModeOrchestrate)
	if !filter("read_file") {
		t.Fatalf("expected runtime scoped file tool to be allowed")
	}
	if filter("bash") {
		t.Fatalf("expected default tool to be blocked by runtime filter")
	}
}

func TestAgentSkipDefaultToolsBlocksDefaultsButKeepsRuntimeMemoryTool(t *testing.T) {
	agent := &models.Agent{ToolConfig: models.AgentToolConfig{SkipDefaultTools: true}}
	if agentAllowsBuiltInTool(agent, "list_files") || agentAllowsBuiltInTool(agent, "bash") || agentAllowsBuiltInTool(agent, "read_file") {
		t.Fatalf("expected agent SkipDefaultTools to block default built-in tools")
	}

	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "memory_view", Access: llmcontracts.RuntimeToolAccessRead}},
		Filter: func(name string) (bool, bool) {
			if name == "memory_view" {
				return true, true
			}
			return false, true
		},
	}
	filter := composeRuntimeToolFilter(func(name string) bool { return agentAllowsBuiltInTool(agent, name) }, rt, true, models.ChatModeOrchestrate)
	if !filter("memory_view") {
		t.Fatalf("expected selected memory runtime tool to remain available")
	}
	if filter("list_files") || filter("bash") || filter("read_file") {
		t.Fatalf("expected default tools to stay blocked")
	}
}

func TestTaskStreamingRuntimeToolComposition_AllowsScopedFilesRuntimeTools(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		SkipDefaultTools: true,
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "list_files"},
		},
		Filter: func(name string) (bool, bool) {
			if name == "list_files" {
				return true, true
			}
			return false, false
		},
	}

	extraTools := runtimeOpenAITools(rt)
	if len(extraTools) != 1 || extraTools[0].Name != "list_files" {
		t.Fatalf("expected runtime tool definition to be exposed, got %#v", extraTools)
	}

	filter := composeRuntimeToolFilter(nil, rt, true, models.ChatModeOrchestrate)
	if !filter("list_files") {
		t.Fatalf("expected task streaming runtime filter to allow managed memory tool")
	}
	if filter("bash") {
		t.Fatalf("expected task streaming runtime filter to block default tools when runtime filter handles them")
	}
}

func TestApplyOpenAIOAuthSystemPrompt_OAuthAppendsWorkingSection(t *testing.T) {
	agent := models.LLMConfig{Provider: models.ProviderOpenAI, AuthMethod: models.AuthMethodOAuth}
	base := "base system prompt"

	got := applyOpenAIOAuthSystemPrompt(base, agent)

	if !strings.Contains(got, base) {
		t.Fatalf("expected base prompt to be preserved")
	}
	if !strings.Contains(got, "# Working with the user") {
		t.Fatalf("expected oauth prompt to include working-with-user section, got %q", got)
	}
	if !strings.Contains(got, "Share intermediary updates in `commentary` channel.") {
		t.Fatalf("expected oauth prompt to include intermediary update guidance")
	}
}

func TestApplyOpenAIOAuthSystemPrompt_APIKeyDoesNotAppendWorkingSection(t *testing.T) {
	agent := models.LLMConfig{Provider: models.ProviderOpenAI, AuthMethod: models.AuthMethodAPIKey}
	base := "base system prompt"

	got := applyOpenAIOAuthSystemPrompt(base, agent)
	if got != base {
		t.Fatalf("expected api key prompt to remain unchanged, got %q", got)
	}
}

func TestApplyOpenAIOAuthSystemPrompt_OAuthNoDuplicateAppend(t *testing.T) {
	agent := models.LLMConfig{Provider: models.ProviderOpenAI, AuthMethod: models.AuthMethodOAuth}
	base := applyOpenAIOAuthSystemPrompt("base system prompt", agent)

	got := applyOpenAIOAuthSystemPrompt(base, agent)

	if strings.Count(got, "# Working with the user") != 1 {
		t.Fatalf("expected working-with-user section to appear once, got %d", strings.Count(got, "# Working with the user"))
	}
}

func TestWrapToolFilterForPlanMode_ReadOnlyAllowlist(t *testing.T) {
	base := func(name string) bool { return true }
	filter := wrapToolFilterForPlanMode(base, false, models.ChatModePlan)

	if !filter("read_file") || !filter("list_files") || !filter("grep_search") {
		t.Fatalf("expected read-only tool allowlist to pass")
	}
	if filter("write_file") || filter("edit_file") || filter("bash") {
		t.Fatalf("expected mutating tools to be blocked in plan mode")
	}
}

func TestComposeRuntimeToolFilter_OrchestrateAllowsOnlyActionTools(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "create_task"},
		},
	}
	base := func(name string) bool { return true }

	filter := composeRuntimeToolFilter(base, rt, false, models.ChatModeOrchestrate)
	if !filter("create_task") {
		t.Fatalf("expected action tool to be allowed in orchestrate mode")
	}
	if filter("read_file") {
		t.Fatalf("expected filesystem tool to be blocked in orchestrate mode")
	}
}

func TestComposeRuntimeToolFilter_PlanBlocksActionToolsAndMutations(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "create_task"},
		},
	}
	base := func(name string) bool { return true }

	filter := composeRuntimeToolFilter(base, rt, false, models.ChatModePlan)
	if filter("create_task") {
		t.Fatalf("expected action tool to be blocked in plan mode")
	}
	if !filter("read_file") || !filter("list_files") || !filter("grep_search") {
		t.Fatalf("expected read-only tools to remain allowed in plan mode")
	}
	if filter("write_file") || filter("bash") {
		t.Fatalf("expected mutating tools to be blocked in plan mode")
	}
}
