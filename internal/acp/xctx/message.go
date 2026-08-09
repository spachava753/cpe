package xctx

import "context"

type executionMessageIDKey struct{}

// WithExecutionMessageID records the persisted assistant message that contains
// the tool callback being executed.
func WithExecutionMessageID(ctx context.Context, messageID string) context.Context {
	return context.WithValue(ctx, executionMessageIDKey{}, messageID)
}

// ExecutionMessageIDFrom returns the persisted assistant message associated
// with the current tool callback, or an empty string when none is attached.
func ExecutionMessageIDFrom(ctx context.Context) string {
	messageID, _ := ctx.Value(executionMessageIDKey{}).(string)
	return messageID
}
