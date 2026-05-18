package contracts

import "context"

type runtimeToolTraceRecorderContextKey struct{}

// WithRuntimeToolTraceRecorder attaches an optional tool-call trace recorder to
// an LLM request context.
func WithRuntimeToolTraceRecorder(ctx context.Context, recorder RuntimeToolTraceRecorder) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, runtimeToolTraceRecorderContextKey{}, recorder)
}

// RuntimeToolTraceRecorderFromContext returns the recorder for request-scoped
// runtime tool events, if one was attached by the caller.
func RuntimeToolTraceRecorderFromContext(ctx context.Context) RuntimeToolTraceRecorder {
	if ctx == nil {
		return nil
	}
	recorder, _ := ctx.Value(runtimeToolTraceRecorderContextKey{}).(RuntimeToolTraceRecorder)
	return recorder
}
