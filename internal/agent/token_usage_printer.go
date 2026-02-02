package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/types"
)

type TokenUsagePrinterGenerator struct {
	gai.GeneratorWrapper
	renderer types.Renderer
	writer   io.Writer
}

func NewTokenUsagePrinterGenerator(wrapped gai.Generator, writer io.Writer) *TokenUsagePrinterGenerator {
	return &TokenUsagePrinterGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: wrapped},
		renderer:         NewRenderer(),
		writer:           writer,
	}
}

// WithTokenUsagePrinting returns a WrapperFunc for use with gai.Wrap
func WithTokenUsagePrinting(writer io.Writer) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewTokenUsagePrinterGenerator(g, writer)
	}
}

func (g *TokenUsagePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	resp, err := g.GeneratorWrapper.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	if inputTokens, ok := gai.InputTokens(resp.UsageMetadata); ok {
		if outputTokens, ok := gai.OutputTokens(resp.UsageMetadata); ok {
			// Build the token usage message with optional cache metrics
			msg := fmt.Sprintf("> input: `%v`, output: `%v`", inputTokens, outputTokens)

			// Add cache read tokens if present
			if cacheRead, ok := gai.CacheReadTokens(resp.UsageMetadata); ok {
				msg += fmt.Sprintf(", cache read: `%v`", cacheRead)
			}

			// Add cache write tokens if present
			if cacheWrite, ok := gai.CacheWriteTokens(resp.UsageMetadata); ok {
				msg += fmt.Sprintf(", cache write: `%v`", cacheWrite)
			}

			tokenMsg, _ := g.renderer.Render(msg)
			fmt.Fprint(g.writer, tokenMsg)
		}
	}

	return resp, nil
}
