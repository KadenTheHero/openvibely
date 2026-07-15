package chatcontrol

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestBuildRuntimeToolExecutor_UnknownToolFallsThrough(t *testing.T) {
	// Regression: grep_search, read_file, list_files are provider-native tools
	// that are NOT in the chatcontrol registry. The executor must return
	// handled=false so the provider's base executor can handle them.
	handlers := map[string]RuntimeActionHandler{}
	executor := BuildRuntimeToolExecutor(models.ChatModePlan, SurfaceWeb, handlers)

	for _, tool := range []string{"grep_search", "read_file", "list_files", "write_file", "edit_file", "bash"} {
		_, handled, _, err := executor(context.Background(), tool, json.RawMessage(`{}`))
		if err != nil {
			t.Errorf("tool %q: unexpected error: %v", tool, err)
		}
		if handled {
			t.Errorf("tool %q: expected handled=false for non-registry tool so base executor handles it", tool)
		}
	}
}

func TestBuildRuntimeToolExecutor_ModeBlockedReturnsHandled(t *testing.T) {
	// Write actions blocked in plan mode should return handled=true with error.
	handlers := map[string]RuntimeActionHandler{
		"create_task": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "created", nil
		},
	}
	executor := BuildRuntimeToolExecutor(models.ChatModePlan, SurfaceWeb, handlers)

	output, handled, isError, err := executor(context.Background(), "create_task", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true for mode-blocked action")
	}
	if !isError {
		t.Fatal("expected isError=true for mode-blocked action")
	}
	if output == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestBuildRuntimeToolExecutorForActions_NonOwnedRegisteredActionFallsThrough(t *testing.T) {
	handlers := map[string]RuntimeActionHandler{
		"send_message": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "sent", nil
		},
	}
	executor := BuildRuntimeToolExecutorForActions(models.ChatModeOrchestrate, SurfaceWeb, handlers, map[string]bool{"send_message": true})

	_, handled, isError, err := executor(context.Background(), "github_get_project_inbox", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled || isError {
		t.Fatalf("expected non-owned registered action to fall through, handled=%v isError=%v", handled, isError)
	}

	output, handled, isError, err := executor(context.Background(), "send_message", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected owned action error: %v", err)
	}
	if !handled || isError || output != "sent" {
		t.Fatalf("expected owned action to execute, handled=%v isError=%v output=%q", handled, isError, output)
	}
}

func TestBuildRuntimeToolExecutor_RegisteredActionExecutes(t *testing.T) {
	handlers := map[string]RuntimeActionHandler{
		"list_models": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "models listed", nil
		},
	}
	executor := BuildRuntimeToolExecutor(models.ChatModeOrchestrate, SurfaceWeb, handlers)

	output, handled, isError, err := executor(context.Background(), "list_models", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true for registered action")
	}
	if isError {
		t.Fatal("expected isError=false for successful execution")
	}
	if output != "models listed" {
		t.Fatalf("expected output='models listed', got %q", output)
	}
}

func TestBuildRuntimeToolExecutor_EmptyNameFallsThrough(t *testing.T) {
	handlers := map[string]RuntimeActionHandler{}
	executor := BuildRuntimeToolExecutor(models.ChatModeOrchestrate, SurfaceWeb, handlers)

	_, handled, _, err := executor(context.Background(), "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatal("expected handled=false for empty tool name")
	}
}

func TestBuildRuntimeToolExecutor_AllowsGrantedGoalStatusActions(t *testing.T) {
	handlers := map[string]RuntimeActionHandler{
		"mark_task_goal_achieved": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "marked", nil
		},
	}
	executor := BuildRuntimeToolExecutor(models.ChatModeOrchestrate, SurfaceWeb, handlers)

	output, handled, isError, err := executor(context.Background(), "mark_task_goal_achieved", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled || isError || output != "marked" {
		t.Fatalf("expected goal status action to execute, handled=%v isError=%v output=%q", handled, isError, output)
	}
}

func TestBuildLifecycleRuntimeToolExecutor_AllowsLifecycleOnly(t *testing.T) {
	handlers := map[string]RuntimeActionHandler{
		"mark_task_goal_achieved": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "marked", nil
		},
	}
	executor := BuildLifecycleRuntimeToolExecutor(models.ChatModeOrchestrate, SurfaceWeb, handlers)

	output, handled, isError, err := executor(context.Background(), "mark_task_goal_achieved", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled || isError || output != "marked" {
		t.Fatalf("expected lifecycle action to execute, handled=%v isError=%v output=%q", handled, isError, output)
	}
}

func TestBuildRuntimeToolExecutor_PlanModeReadActionsWork(t *testing.T) {
	// Read-only chatcontrol actions (like list_models) should work in plan mode.
	handlers := map[string]RuntimeActionHandler{
		"list_models": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "models", nil
		},
		"get_chat_mode": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "plan", nil
		},
	}
	executor := BuildRuntimeToolExecutor(models.ChatModePlan, SurfaceWeb, handlers)

	for _, tool := range []string{"list_models", "get_chat_mode"} {
		output, handled, isError, err := executor(context.Background(), tool, json.RawMessage(`{}`))
		if err != nil {
			t.Errorf("tool %q: unexpected error: %v", tool, err)
		}
		if !handled {
			t.Errorf("tool %q: expected handled=true", tool)
		}
		if isError {
			t.Errorf("tool %q: expected isError=false", tool)
		}
		if output == "" {
			t.Errorf("tool %q: expected non-empty output", tool)
		}
	}
}
