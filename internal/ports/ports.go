// Package ports provides shared interface definitions used across the codebase.
package ports

import (
	"context"

	"github.com/spachava753/gai"
)

// ToolRegistrar is an interface for registering tools with a generator.
type ToolRegistrar interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

// RuntimeObserver receives ordered events from a CPE-owned agent runtime.
type RuntimeObserver interface {
	ToolResult(ctx context.Context, dialog gai.Dialog, msg gai.Message, associatedCall gai.Block) error
	ThoughtTrace(ctx context.Context, dialog gai.Dialog, block gai.Block) error
	ToolCall(ctx context.Context, dialog gai.Dialog, block gai.Block) error
}

// RuntimeObserverRegistrar is implemented by generators that accept a runtime observer.
type RuntimeObserverRegistrar interface {
	SetRuntimeObserver(observer RuntimeObserver)
}

// Generator is an interface for AI generators that work with gai.Dialog.
type Generator interface {
	Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
}
