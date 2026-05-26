package service

import "context"

type additionalProjectInstructionsKey struct{}

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
