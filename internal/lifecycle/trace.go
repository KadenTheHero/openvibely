package lifecycle

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/openvibely/openvibely/internal/models"
)

const maxTraceStringBytes = 16 * 1024

// ExecutionEventAppender is implemented by lifecycle stores that can persist
// prompt-safe trace events for one lifecycle execution.
type ExecutionEventAppender interface {
	AppendExecutionEvent(ctx context.Context, event *models.LifecycleExecutionEvent) error
}

// TraceRecorder appends ordered events to the current lifecycle execution.
type TraceRecorder struct {
	store       ExecutionEventAppender
	executionID string
	logger      *log.Logger
}

func NewTraceRecorder(store ExecutionEventAppender, executionID string, logger *log.Logger) *TraceRecorder {
	if store == nil || strings.TrimSpace(executionID) == "" {
		return nil
	}
	if logger == nil {
		logger = log.Default()
	}
	return &TraceRecorder{store: store, executionID: executionID, logger: logger}
}

// RecordRuntimeToolEvent implements llm/contracts.RuntimeToolTraceRecorder.
func (r *TraceRecorder) RecordRuntimeToolEvent(ctx context.Context, eventType string, payload any) {
	r.Record(ctx, eventType, payload)
}

func (r *TraceRecorder) Record(ctx context.Context, eventType string, payload any) {
	if r == nil || r.store == nil || strings.TrimSpace(r.executionID) == "" {
		return
	}
	payloadJSON, err := json.Marshal(sanitizeTracePayload(payload))
	if err != nil {
		payloadJSON, _ = json.Marshal(map[string]any{"marshal_error": err.Error()})
	}
	event := &models.LifecycleExecutionEvent{
		LifecycleExecutionID: r.executionID,
		EventType:            strings.TrimSpace(eventType),
		PayloadJSON:          string(payloadJSON),
	}
	if event.EventType == "" {
		event.EventType = "event"
	}
	if err := r.store.AppendExecutionEvent(ctx, event); err != nil && r.logger != nil {
		r.logger.Printf("[lifecycle] append trace event failed exec=%s type=%s: %v", r.executionID, event.EventType, err)
	}
}

type traceRecorderContextKey struct{}

func WithTraceRecorder(ctx context.Context, recorder *TraceRecorder) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, traceRecorderContextKey{}, recorder)
}

func TraceRecorderFromContext(ctx context.Context) *TraceRecorder {
	if ctx == nil {
		return nil
	}
	rec, _ := ctx.Value(traceRecorderContextKey{}).(*TraceRecorder)
	return rec
}

func RecordTraceEvent(ctx context.Context, eventType string, payload any) {
	if rec := TraceRecorderFromContext(ctx); rec != nil {
		rec.Record(ctx, eventType, payload)
	}
}

func sanitizeTracePayload(v any) any {
	switch x := v.(type) {
	case nil:
		return map[string]any{}
	case string:
		return truncateTraceString(x)
	case []byte:
		return truncateTraceString(string(x))
	case json.RawMessage:
		return truncateTraceString(string(x))
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return x
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = sanitizeTracePayload(v)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = sanitizeTracePayload(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = sanitizeTracePayload(v)
		}
		return out
	default:
		// Round-trip through JSON so structs become maps, then sanitize strings.
		raw, err := json.Marshal(x)
		if err != nil {
			return map[string]any{"value": truncateTraceString(strings.TrimSpace(err.Error()))}
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return truncateTraceString(string(raw))
		}
		return sanitizeTracePayload(decoded)
	}
}

func truncateTraceString(s string) string {
	if len(s) <= maxTraceStringBytes {
		return s
	}
	return s[:maxTraceStringBytes] + "\n...[truncated]"
}
