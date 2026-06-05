package contracts

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRuntimeToolsContextRoundTrip(t *testing.T) {
	rt := &RuntimeTools{
		Definitions: []RuntimeToolDefinition{
			{
				Name:        "create_task",
				Description: "Create a task",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	}

	ctx := WithRuntimeTools(context.Background(), rt)
	got := RuntimeToolsFromContext(ctx)
	if got == nil {
		t.Fatalf("expected runtime tools in context")
	}
	if !got.HasDefinition("create_task") {
		t.Fatalf("expected create_task definition")
	}
}

func TestRuntimeToolsContextNilSafe(t *testing.T) {
	if got := RuntimeToolsFromContext(context.TODO()); got != nil {
		t.Fatalf("expected nil runtime tools for context without runtime tools")
	}
	ctx := WithRuntimeTools(context.TODO(), nil)
	if ctx == nil {
		t.Fatalf("expected non-nil context")
	}
	if got := RuntimeToolsFromContext(ctx); got != nil {
		t.Fatalf("expected nil runtime tools when none set")
	}
}

func TestCompositeRuntimeToolsDeduplicatesDefinitions(t *testing.T) {
	base := &RuntimeTools{Definitions: []RuntimeToolDefinition{
		{Name: "memory_view", Description: "selected memory", Parameters: json.RawMessage(`{"type":"object","properties":{"handle":{"type":"string"}}}`), Access: RuntimeToolAccessRead},
	}}
	actions := &RuntimeTools{Definitions: []RuntimeToolDefinition{
		{Name: "memory_view", Description: "placeholder memory action", Parameters: json.RawMessage(`{"type":"object"}`), Access: RuntimeToolAccessRead},
		{Name: "list_capabilities", Description: "list actions", Parameters: json.RawMessage(`{"type":"object"}`), Access: RuntimeToolAccessRead},
	}}

	got := CompositeRuntimeTools(base, actions)
	if got == nil {
		t.Fatalf("expected composite runtime tools")
	}
	if len(got.Definitions) != 2 {
		t.Fatalf("expected duplicate memory_view definition to be collapsed, got %#v", got.Definitions)
	}
	if got.Definitions[0].Description != "selected memory" {
		t.Fatalf("expected first memory_view definition to win, got %#v", got.Definitions[0])
	}
	if !got.HasDefinition("memory_view") || !got.HasDefinition("list_capabilities") {
		t.Fatalf("expected memory_view and list_capabilities definitions, got %#v", got.Definitions)
	}
}
