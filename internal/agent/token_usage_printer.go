package agent

import (
	"context"
	"fmt"
	"slices"

	"github.com/spachava753/gai"
)

type TokenUsagePrinterGenerator struct {
	wrapped interface {
		gai.Generator
		gai.TokenCounter
	}
}

func (g *TokenUsagePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	if dialog[len(dialog)-1].Role == gai.ToolResult {
		tokens, err := g.wrapped.Count(ctx, dialog)
		if err != nil {
			return gai.Response{}, err
		}
		fmt.Printf("Total tokens: %d", tokens)
	}

	resp, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	if !slices.ContainsFunc(resp.Candidates[0].Blocks, func(block gai.Block) bool {
		return block.BlockType == gai.ToolCall
	}) {
		tokens, err := g.wrapped.Count(ctx, append(slices.Clone(dialog), resp.Candidates[0]))
		if err != nil {
			return gai.Response{}, err
		}
		fmt.Printf("Total tokens: %d", tokens)
	}

	return resp, nil
}

// NewTokenUsagePrinterGenerator creates a new TokenUsagePrinterGenerator
// that wraps a generator implementing both gai.Generator and gai.TokenCounter interfaces
func NewTokenUsagePrinterGenerator(wrapped interface {
	gai.Generator
	gai.TokenCounter
}) *TokenUsagePrinterGenerator {
	return &TokenUsagePrinterGenerator{
		wrapped: wrapped,
	}
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped generator
func (g *TokenUsagePrinterGenerator) Register(tool gai.Tool) error {
	if registerer, ok := g.wrapped.(gai.ToolRegister); ok {
		return registerer.Register(tool)
	}
	return fmt.Errorf("wrapped generator does not implement ToolRegister")
}
