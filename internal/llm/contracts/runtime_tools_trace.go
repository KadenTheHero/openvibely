package contracts

import (
	"context"
	"encoding/json"
	"time"
)

// RuntimeToolTraceRecorder observes request-scoped tool calls. It is intentionally
// small so higher layers can attach lifecycle-specific durable tracing without
// making individual tools depend on lifecycle packages.
type RuntimeToolTraceRecorder interface {
	RecordRuntimeToolEvent(ctx context.Context, eventType string, payload any)
}

// TraceRuntimeTools wraps a RuntimeTools executor and records tool_call and
// tool_result events. It returns rt unchanged when no executor/recorder exists.
func TraceRuntimeTools(rt *RuntimeTools, recorder RuntimeToolTraceRecorder) *RuntimeTools {
	if rt == nil || rt.Executor == nil || recorder == nil {
		return rt
	}
	out := *rt
	base := rt.Executor
	out.Executor = func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
		start := time.Now()
		recorder.RecordRuntimeToolEvent(ctx, "tool_call", map[string]any{
			"name": name,
			"args": string(input),
		})
		output, handled, isError, err := base(ctx, name, input)
		payload := map[string]any{
			"name":        name,
			"handled":     handled,
			"is_error":    isError,
			"output":      output,
			"duration_ms": time.Since(start).Milliseconds(),
		}
		if err != nil {
			payload["error"] = err.Error()
		}
		recorder.RecordRuntimeToolEvent(ctx, "tool_result", payload)
		return output, handled, isError, err
	}
	return &out
}
