package xctx

import (
	"context"

	"github.com/coder/acp-go-sdk"
)

type toolCallIdKey struct{}

func WithToolCallId(ctx context.Context, toolId acp.ToolCallId) context.Context {
	return context.WithValue(ctx, toolCallIdKey{}, toolId)
}

func ToolCallIdFrom(ctx context.Context) acp.ToolCallId {
	id, _ := ctx.Value(toolCallIdKey{}).(acp.ToolCallId)
	return id
}
