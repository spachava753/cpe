package agent

import (
	"context"
	"fmt"
	"iter"

	"github.com/spachava753/gai"
)

// StreamingPrinterGenerator is a middleware that implements gai.StreamingGenerator
// and prints each chunk as it arrives to stdout
type StreamingPrinterGenerator struct {
	wrapped      gai.StreamingGenerator
	continuation bool
}

// NewStreamingPrinterGenerator creates a new StreamingPrinterGenerator
func NewStreamingPrinterGenerator(wrapped gai.StreamingGenerator) *StreamingPrinterGenerator {
	return &StreamingPrinterGenerator{
		wrapped: wrapped,
	}
}

// Count implements the gai.TokenCounter interface by delegating to the wrapped generator
func (g *StreamingPrinterGenerator) Count(ctx context.Context, dialog gai.Dialog) (uint, error) {
	if counter, ok := g.wrapped.(gai.TokenCounter); ok {
		return counter.Count(ctx, dialog)
	}
	return 0, fmt.Errorf("wrapped generator does not implement TokenCounter")
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
		var prevBlockType string
		for chunk, err := range g.wrapped.Stream(ctx, dialog, options) {
			if prevBlockType != chunk.Block.BlockType {
				// insert a new lines before starting to print a new block, but only if this not the first block ever printed
				if g.continuation {
					fmt.Printf("\n\n")
				}
				prevBlockType = chunk.Block.BlockType
			}
			// Always yield the chunk, regardless of printing
			if !yield(chunk, err) {
				return
			}

			// Handle errors
			if err != nil {
				continue
			}

			text := chunk.Block.Content.String()

			switch chunk.Block.BlockType {
			case gai.Content, gai.Thinking:
				fmt.Print(text)

			case gai.ToolCall:
				if chunk.Block.ID == "" {
					fmt.Print(chunk.Block.Content.String())
					continue
				}

				fmt.Printf("[Tool Call: %s]\n", chunk.Block.Content.String())
			}
			g.continuation = true
		}
		fmt.Printf("\n")
	}
}
