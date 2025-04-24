package agent

import (
	"context"
	"github.com/spachava753/gai"
)

// ThinkingFilterToolGenerator is a wrapper around the ToolGenerator that filters
// thinking blocks only from the initial dialog passed to ToolGenerator.Generate.
// This wrapper preserves thinking blocks generated during
// tool execution within ToolGenerator.Generate.
type ThinkingFilterToolGenerator struct {
	// wrapped is the ToolGenerator being wrapped, defined as an interface to allow mocking
	wrapped interface {
		Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
		Register(tool gai.Tool, callback gai.ToolCallback) error
	}
}

// NewThinkingFilterToolGenerator creates a new ThinkingFilterToolGenerator
func NewThinkingFilterToolGenerator(wrapped *gai.ToolGenerator) *ThinkingFilterToolGenerator {
	return &ThinkingFilterToolGenerator{
		wrapped: wrapped,
	}
}

// filterThinkingBlocks removes thinking blocks from a dialog
func filterThinkingBlocks(dialog gai.Dialog) gai.Dialog {
	filtered := make(gai.Dialog, len(dialog))

	for i, message := range dialog {
		// Create a new message with the same role
		filteredMessage := gai.Message{
			Role:            message.Role,
			ToolResultError: message.ToolResultError,
		}

		// Filter out thinking blocks
		for _, block := range message.Blocks {
			if block.BlockType != gai.Thinking {
				filteredMessage.Blocks = append(filteredMessage.Blocks, block)
			}
		}

		filtered[i] = filteredMessage
	}

	return filtered
}

// Generate implements the gai.Generator interface.
// It filters thinking blocks from the initial dialog before passing it to the wrapped ToolGenerator.
func (g *ThinkingFilterToolGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	// Filter thinking blocks from the initial dialog
	filteredDialog := filterThinkingBlocks(dialog)

	// Call the wrapped ToolGenerator with the filtered dialog
	// This preserves thinking blocks that occur during tool execution
	return g.wrapped.Generate(ctx, filteredDialog, optsGen)
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped ToolGenerator.
func (g *ThinkingFilterToolGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	return g.wrapped.Register(tool, callback)
}
