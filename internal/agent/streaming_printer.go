package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tidwall/pretty"
	"iter"
	"strings"

	"github.com/spachava753/gai"
)

// StreamingPrinterGenerator is a middleware that implements gai.StreamingGenerator
// and prints each chunk as it arrives to stdout
type StreamingPrinterGenerator struct {
	wrapped gai.StreamingGenerator
}

// NewStreamingPrinterGenerator creates a new StreamingPrinterGenerator
func NewStreamingPrinterGenerator(wrapped gai.StreamingGenerator) *StreamingPrinterGenerator {
	return &StreamingPrinterGenerator{
		wrapped: wrapped,
	}
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped generator
func (g *StreamingPrinterGenerator) Register(tool gai.Tool) error {
	if registerer, ok := g.wrapped.(gai.ToolRegister); ok {
		return registerer.Register(tool)
	}
	return fmt.Errorf("wrapped generator does not implement ToolRegister")
}

// Stream implements the gai.StreamingGenerator interface
func (g *StreamingPrinterGenerator) Stream(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) iter.Seq2[gai.StreamChunk, error] {
	return func(yield func(gai.StreamChunk, error) bool) {
		// Keep track of whether we're in a tool call
		var inToolCall bool
		var toolCallBuffer strings.Builder
		var currentToolName string

		for chunk, err := range g.wrapped.Stream(ctx, dialog, options) {
			// Always yield the chunk, regardless of printing
			if !yield(chunk, err) {
				return
			}

			// Handle errors
			if err != nil {
				continue
			}

			// Print based on block type
			switch chunk.Block.BlockType {
			case gai.Content:
				// Print content text directly
				if chunk.Block.ModalityType == gai.Text {
					fmt.Print(chunk.Block.Content.String())
				}

			case gai.Thinking:
				// Print thinking blocks
				if chunk.Block.ModalityType == gai.Text {
					if !inToolCall {
						fmt.Print(chunk.Block.Content.String())
					}
				}

			case gai.ToolCall:
				if chunk.Block.ID != "" {
					// Start of a new tool call
					if inToolCall {
						// Finish previous tool call
						fmt.Println()
					}
					inToolCall = true
					toolCallBuffer.Reset()
					currentToolName = chunk.Block.Content.String()
					fmt.Printf("\n[Tool Call: %s]\n", currentToolName)
				} else {
					// Tool call parameter chunk
					toolCallBuffer.WriteString(chunk.Block.Content.String())
				}
			}
		}

		// Handle any remaining tool call
		if inToolCall {
			// Try to parse and format the parameters
			params := toolCallBuffer.String()
			if params != "" {
				var jsonParams map[string]any
				if err := json.Unmarshal([]byte(params), &jsonParams); err == nil {
					fmt.Printf("%s\n", string(pretty.Color([]byte(params), nil)))
				} else {
					fmt.Printf("%s\n", params)
				}
			}
		}
	}
}
