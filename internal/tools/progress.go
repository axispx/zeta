package tools

import "context"

type progressKey struct{}

// WithProgress registers a callback for live tool output snapshots.
func WithProgress(ctx context.Context, fn func(string)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, fn)
}

func progressFrom(ctx context.Context) func(string) {
	fn, _ := ctx.Value(progressKey{}).(func(string))
	return fn
}
