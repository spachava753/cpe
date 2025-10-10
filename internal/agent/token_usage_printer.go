package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/spachava753/gai"
)

type TokenUsagePrinterGenerator struct {
	wrapped gai.ToolCapableGenerator
}

func NewTokenUsagePrinterGenerator(wrapped gai.ToolCapableGenerator) *TokenUsagePrinterGenerator {
	return &TokenUsagePrinterGenerator{
		wrapped: wrapped,
	}
}

func (g *TokenUsagePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	resp, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	if inputTokens, ok := gai.InputTokens(resp.UsageMetadata); ok {
		if outputTokens, ok := gai.OutputTokens(resp.UsageMetadata); ok {
			tokenMsg := fmt.Sprintf("Tokens used: %v input, %v output", inputTokens, outputTokens)
			fmt.Fprintln(os.Stderr, redStyle.Render(tokenMsg))
		}
	}

	return resp, nil
}

func (g *TokenUsagePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}
