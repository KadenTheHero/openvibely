package openaiclient

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompletionsOptions_DefaultMaxTurnsNoLimit(t *testing.T) {
	client := NewWithAPIKey("test-key")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := &CompletionsOptions{}
	_, err := client.SendCompletions(ctx, "test", opts)
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}

	if opts.MaxTurns != math.MaxInt32 {
		t.Fatalf("expected MaxTurns default to no limit (%d), got %d", math.MaxInt32, opts.MaxTurns)
	}
}

func TestSendCompletions_DisableToolsSuppressesExtraTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(body, &reqBody)
		if _, ok := reqBody["tools"]; ok {
			t.Fatalf("expected no tools when DisableTools=true, got %#v", reqBody["tools"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"Done.\"}}]}\n\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1}}\n\n" +
				"data: [DONE]\n\n",
		))
	}))
	defer srv.Close()

	oldBaseURL := OpenAIAPIBaseURL
	OpenAIAPIBaseURL = srv.URL + "/"
	defer func() { OpenAIAPIBaseURL = oldBaseURL }()

	client := NewWithAPIKey("sk-test")
	_, err := client.SendCompletions(context.Background(), "test", &CompletionsOptions{
		Model:        "gpt-5.3-codex",
		DisableTools: true,
		ExtraTools: []ToolDefinition{
			{Name: "memory_view", Description: "Load an authorized selected memory"},
		},
	})
	if err != nil {
		t.Fatalf("SendCompletions: %v", err)
	}
}

func TestSendCompletions_ToolFilterRemovesDeniedToolsFromRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(body, &reqBody)

		tools, ok := reqBody["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("expected memory_view runtime tool only, got %#v", reqBody["tools"])
		}
		seen := map[string]bool{}
		for _, rawTool := range tools {
			tool, _ := rawTool.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			name, _ := function["name"].(string)
			seen[name] = true
		}
		if !seen["memory_view"] || seen["list_files"] || seen["Bash"] {
			t.Fatalf("expected memory_view only, got %#v", seen)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"Done.\"}}]}\n\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1}}\n\n" +
				"data: [DONE]\n\n",
		))
	}))
	defer srv.Close()

	oldBaseURL := OpenAIAPIBaseURL
	OpenAIAPIBaseURL = srv.URL + "/"
	defer func() { OpenAIAPIBaseURL = oldBaseURL }()

	client := NewWithAPIKey("sk-test")
	resp, err := client.SendCompletions(context.Background(), "test", &CompletionsOptions{
		Model: "gpt-5.3-codex",
		ExtraTools: []ToolDefinition{
			{
				Type:        "function",
				Name:        "memory_view",
				Description: "Load an authorized selected memory",
			},
		},
		ToolFilter: func(name string) bool { return name == "memory_view" },
	})
	if err != nil {
		t.Fatalf("SendCompletions: %v", err)
	}
	if resp.Text != "Done." {
		t.Errorf("Text = %q", resp.Text)
	}
}

func TestCompletionsTurnResult_Structure(t *testing.T) {
	// Just a compilation test
	result := &completionsTurnResult{
		text:        "test",
		stopReason:  "stop",
		model:       "gpt-4",
		inputTokens: 10,
	}

	if result.text != "test" {
		t.Errorf("expected text=test, got %s", result.text)
	}
}
