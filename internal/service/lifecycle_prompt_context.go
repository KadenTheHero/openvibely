package service

import "context"

type additionalProjectInstructionsKey struct{}
type selectedMemoryHandlesKey struct{}

func withAdditionalProjectInstructions(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if value == "" {
		return ctx
	}
	return context.WithValue(ctx, additionalProjectInstructionsKey{}, value)
}

func additionalProjectInstructionsFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(additionalProjectInstructionsKey{}).(string)
	return v
}

func combineAdditionalProjectInstructions(ctx context.Context, base string) string {
	extra := additionalProjectInstructionsFromContext(ctx)
	if extra == "" {
		return base
	}
	if base == "" {
		return extra
	}
	return extra + "\n\n" + base
}

// WithSelectedMemoryHandles annotates ctx with the managed memory handles selected
// for the current model turn.
func WithSelectedMemoryHandles(ctx context.Context, handles []string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(handles) == 0 {
		return ctx
	}
	return context.WithValue(ctx, selectedMemoryHandlesKey{}, append([]string(nil), handles...))
}

// SelectedMemoryHandlesFromContext returns the managed memory handles selected
// for the current model turn, if lifecycle memory recall selected any.
func SelectedMemoryHandlesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	handles, _ := ctx.Value(selectedMemoryHandlesKey{}).([]string)
	return append([]string(nil), handles...)
}
