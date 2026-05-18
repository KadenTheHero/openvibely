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
