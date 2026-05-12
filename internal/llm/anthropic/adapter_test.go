package anthropic

import (
	"context"
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

	got := toolSecondaryInfo("Bash", raw)
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

	got := toolSecondaryInfo("Grep", raw)
	if !strings.Contains(got, "chat_shared") {
		t.Fatalf("expected later grep context to survive truncation, got %q", got)
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

func TestComposeTaskRuntimeToolFilter_AllowsDefaultToolsWithoutRuntimeTools(t *testing.T) {
	base := func(name string) bool {
		switch name {
		case "read_file", "list_files", "grep_search", "bash":
			return true
		default:
			return false
		}
	}

	filter := composeTaskRuntimeToolFilter(base, nil)
	for _, name := range []string{"read_file", "list_files", "grep_search", "bash"} {
		if !filter(name) {
			t.Fatalf("expected task tool %q to remain allowed without runtime action tools", name)
		}
	}
	if filter("unknown_tool") {
		t.Fatalf("expected base filter denial to be preserved")
	}
}

func TestTaskStreamingRuntimeToolComposition_AllowsScopedFilesRuntimeTools(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "list_files"}},
		Executor: func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
			if name == "list_files" {
				return "[]", true, false, nil
			}
			return "", false, false, nil
		},
		Filter: func(name string) (bool, bool) {
			if name == "list_files" {
				return true, true
			}
			return false, false
		},
		SkipDefaultTools: true,
	}

	extraTools := runtimeAnthropicTools(rt)
	if len(extraTools) != 1 || extraTools[0].Name != "list_files" {
		t.Fatalf("runtimeAnthropicTools() = %#v, want list_files", extraTools)
	}

	exec := composeRuntimeToolExecutor(nil, rt)
	out, isError, err := exec(context.Background(), "list_files", json.RawMessage(`{}`))
	if err != nil || isError || out != "[]" {
		t.Fatalf("runtime executor = (%q, %v, %v), want non-error [] nil", out, isError, err)
	}

	filter := composeTaskRuntimeToolFilter(nil, rt)
	if !filter("list_files") {
		t.Fatalf("expected runtime scoped file tool to be allowed")
	}
	if filter("Read") || filter("Bash") {
		t.Fatalf("expected default tools to be hidden when SkipDefaultTools is true")
	}
}

func TestShouldSkipDefaultToolsForChatMode(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "create_task"},
		},
	}

	if !shouldSkipDefaultToolsForChatMode(false, models.ChatModeOrchestrate, rt) {
		t.Fatalf("expected default tools to be skipped for orchestrate chat with runtime action tools")
	}
	if shouldSkipDefaultToolsForChatMode(true, models.ChatModeOrchestrate, rt) {
		t.Fatalf("did not expect skip for task follow-up mode")
	}
	if shouldSkipDefaultToolsForChatMode(false, models.ChatModePlan, rt) {
		t.Fatalf("did not expect skip for plan mode")
	}
	if shouldSkipDefaultToolsForChatMode(false, models.ChatModeOrchestrate, nil) {
		t.Fatalf("did not expect skip without runtime tools")
	}
}

func TestDirectRuntimeToolsDoNotRequestSkippingDefaultsByDefault(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "write_file"}},
	}
	if rt.SkipDefaultTools {
		t.Fatalf("runtime tools should not skip defaults unless explicitly requested")
	}
	rt.SkipDefaultTools = true
	if !rt.SkipDefaultTools {
		t.Fatalf("expected explicit skip-default flag to be settable for scoped tool sessions")
	}
}

func TestResolveChatToolPolicy(t *testing.T) {
	rt := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{
			{Name: "create_task"},
		},
	}

	tests := []struct {
		name   string
		follow bool
		mode   models.ChatMode
		rt     *llmcontracts.RuntimeTools
		wantD  bool
		wantS  bool
	}{
		{
			name:   "orchestrate without runtime tools disables function tools",
			follow: false,
			mode:   models.ChatModeOrchestrate,
			rt:     nil,
			wantD:  true,
			wantS:  false,
		},
		{
			name:   "orchestrate with runtime tools skips defaults without disabling tools",
			follow: false,
			mode:   models.ChatModeOrchestrate,
			rt:     rt,
			wantD:  false,
			wantS:  true,
		},
		{
			name:   "plan mode keeps tools enabled and defaults visible",
			follow: false,
			mode:   models.ChatModePlan,
			rt:     rt,
			wantD:  false,
			wantS:  false,
		},
		{
			name:   "task follow-up keeps tools enabled and defaults visible",
			follow: true,
			mode:   models.ChatModeOrchestrate,
			rt:     rt,
			wantD:  false,
			wantS:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDisable, gotSkip := resolveChatToolPolicy(tc.follow, tc.mode, tc.rt)
			if gotDisable != tc.wantD || gotSkip != tc.wantS {
				t.Fatalf("resolveChatToolPolicy(follow=%v, mode=%s, rt_nil=%v) = (disable=%v, skip=%v), want (disable=%v, skip=%v)",
					tc.follow, tc.mode, tc.rt == nil, gotDisable, gotSkip, tc.wantD, tc.wantS)
			}
		})
	}
}
