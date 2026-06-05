package anthropic

import (
	"encoding/json"
	"testing"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func TestComposeRuntimeToolFilter_AllowsFilteredReadOnlyRuntimeToolInPlanMode(t *testing.T) {
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

func TestComposeRuntimeToolFilter_OrchestrateAllowsMemoryViewWithoutFilesystemRead(t *testing.T) {
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

func TestClaudeCodeMaxOutputTokens(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  int
	}{
		{"opus 4.8", "claude-opus-4-8", 64000},
		{"opus 4.7", "claude-opus-4-7-20260514", 64000},
		{"opus 4.6", "claude-opus-4-6-20260401", 64000},
		{"sonnet 4.6", "claude-sonnet-4-6-20260514", 32000},
		{"opus 4.5", "claude-opus-4-5-20251101", 32000},
		{"sonnet 4.5", "claude-sonnet-4-5-20250929", 32000},
		{"sonnet 4.0", "claude-sonnet-4-0-20250514", 32000},
		{"haiku 4.5", "claude-haiku-4-5-20251001", 32000},
		{"opus 4.1", "claude-opus-4-1-20250805", 32000},
		{"opus 4.0", "claude-opus-4-0-20250514", 32000},
		{"3.7 sonnet", "claude-3-7-sonnet-20250219", 32000},
		{"3.5 sonnet", "claude-3-5-sonnet-20241022", 8192},
		{"3.5 haiku", "claude-3-5-haiku-20241022", 8192},
		{"3 sonnet", "claude-3-sonnet-20240229", 8192},
		{"3 opus", "claude-3-opus-20240229", 4096},
		{"3 haiku", "claude-3-haiku-20240307", 4096},
		{"fallback", "claude-future-model", 32000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(claudeCodeMaxOutputTokensEnv, "")
			if got := claudeCodeMaxOutputTokens(tt.model); got != tt.want {
				t.Fatalf("claudeCodeMaxOutputTokens(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestClaudeCodeMaxOutputTokensOverrideClampsToUpperLimit(t *testing.T) {
	t.Setenv(claudeCodeMaxOutputTokensEnv, "999999")
	if got := claudeCodeMaxOutputTokens("claude-opus-4-7-20260514"); got != 128000 {
		t.Fatalf("opus 4.7 override = %d, want 128000", got)
	}
	if got := claudeCodeMaxOutputTokens("claude-sonnet-4-6-20260514"); got != 64000 {
		t.Fatalf("sonnet 4.6 override = %d, want 64000", got)
	}
	if got := claudeCodeMaxOutputTokens("claude-3-5-sonnet-20241022"); got != 8192 {
		t.Fatalf("3.5 sonnet override = %d, want 8192", got)
	}
}

func TestClaudeCodeMaxOutputTokensOverrideUsesParseIntSemantics(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"positive value", "20000", 20000},
		{"numeric prefix", "20000extra", 20000},
		{"leading whitespace", " 20000", 20000},
		{"plus sign", "+20000", 20000},
		{"positive overflow caps", "999999999999999999999999999999999999999999", 64000},
		{"negative prefix falls back", "-1", 32000},
		{"invalid falls back", "not-a-number", 32000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(claudeCodeMaxOutputTokensEnv, tt.raw)
			if got := claudeCodeMaxOutputTokens("claude-sonnet-4-5-20250929"); got != tt.want {
				t.Fatalf("override %q = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestClaudeCodeMaxOutputTokensInvalidOverrideUsesDefault(t *testing.T) {
	t.Setenv(claudeCodeMaxOutputTokensEnv, "not-a-number")
	if got := claudeCodeMaxOutputTokens("claude-opus-4-7-20260514"); got != 64000 {
		t.Fatalf("invalid override = %d, want default 64000", got)
	}
}
