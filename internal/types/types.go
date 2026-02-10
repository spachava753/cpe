// Package types provides shared interface definitions used across the codebase.
package types

import (
	"context"

	"github.com/spachava753/gai"
)

// MessageIDKey is the key used in gai.Message.ExtraFields to store the message ID.
// This enables the saving middleware to track which messages have been persisted
// and allows printers to display message IDs.
const MessageIDKey = "cpe_message_id"

// DialogSaver is an interface for saving messages to persistent storage.
// Implementations must be safe to call from the saving middleware during generation.
type DialogSaver interface {
	SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error)
}

// ToolRegistrar is an interface for registering tools with a generator.
type ToolRegistrar interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

// Generator is an interface for AI generators that work with gai.Dialog.
type Generator interface {
	Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
}

// Renderer is an interface for rendering content (e.g., markdown to formatted output).
type Renderer interface {
	Render(in string) (string, error)
}
