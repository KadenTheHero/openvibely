package contracts

import "context"

type steeringCallbackKey struct{}
type steeringRetryResetCallbackKey struct{}

// SteeringCallback returns raw steering text to inject before the next provider/tool-loop model request.
type SteeringCallback func(context.Context) (string, error)

// SteeringRetryResetCallback resets steering claimed by a failed provider attempt before retrying.
type SteeringRetryResetCallback func(context.Context) error

func WithSteeringCallback(ctx context.Context, callback SteeringCallback) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, steeringCallbackKey{}, callback)
}

func SteeringCallbackFromContext(ctx context.Context) SteeringCallback {
	if ctx == nil {
		return nil
	}
	callback, _ := ctx.Value(steeringCallbackKey{}).(SteeringCallback)
	return callback
}

func WithSteeringRetryResetCallback(ctx context.Context, callback SteeringRetryResetCallback) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, steeringRetryResetCallbackKey{}, callback)
}

func SteeringRetryResetCallbackFromContext(ctx context.Context) SteeringRetryResetCallback {
	if ctx == nil {
		return nil
	}
	callback, _ := ctx.Value(steeringRetryResetCallbackKey{}).(SteeringRetryResetCallback)
	return callback
}
