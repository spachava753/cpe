package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/spachava753/gai"
)

// SubagentOptions contains parameters for subagent execution
type SubagentOptions struct {
	// UserBlocks is the input to the subagent
	UserBlocks []gai.Block

	// Generator is the tool-capable generator to use
	Generator ToolCapableGenerator

	// GenOptsFunc returns generation options (optional)
	GenOptsFunc gai.GenOptsGenerator
}

// ExecuteSubagent runs a subagent and returns the final response text.
// Unlike Generate, this does not handle conversation state or storage.
func ExecuteSubagent(ctx context.Context, opts SubagentOptions) (string, error) {
	if len(opts.UserBlocks) == 0 {
		return "", fmt.Errorf("empty input")
	}

	// Build dialog with user message
	userMessage := gai.Message{
		Role:   gai.User,
		Blocks: opts.UserBlocks,
	}
	dialog := gai.Dialog{userMessage}

	// Generate response
	resultDialog, err := opts.Generator.Generate(ctx, dialog, opts.GenOptsFunc)
	if err != nil {
		return "", fmt.Errorf("generation failed: %w", err)
	}

	// Extract the final assistant response
	return extractFinalResponse(resultDialog), nil
}

// extractFinalResponse extracts the final text response from the dialog
func extractFinalResponse(dialog gai.Dialog) string {
	// Find the last assistant message
	for i := len(dialog) - 1; i >= 0; i-- {
		if dialog[i].Role == gai.Assistant {
			// Extract text content from blocks
			var textParts []string
			for _, block := range dialog[i].Blocks {
				if block.BlockType == gai.Content && block.ModalityType == gai.Text {
					textParts = append(textParts, block.Content.String())
				}
			}
			if len(textParts) > 0 {
				return strings.Join(textParts, "\n")
			}
		}
	}
	return ""
}
