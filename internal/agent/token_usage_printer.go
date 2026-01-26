package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/spachava753/cpe/internal/types"
	"github.com/spachava753/gai"
)

type TokenUsagePrinterGenerator struct {
	wrapped  gai.ToolCapableGenerator
	renderer types.Renderer
	writer   io.Writer
}

func NewTokenUsagePrinterGenerator(wrapped gai.ToolCapableGenerator, writer io.Writer) *TokenUsagePrinterGenerator {
	return &TokenUsagePrinterGenerator{
		wrapped:  wrapped,
		renderer: NewRenderer(),
		writer:   writer,
	}
}

func (g *TokenUsagePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	resp, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	if inputTokens, ok := gai.InputTokens(resp.UsageMetadata); ok {
		if outputTokens, ok := gai.OutputTokens(resp.UsageMetadata); ok {
			tokenMsg, _ := g.renderer.Render(fmt.Sprintf("> input: `%v`, output: `%v`", inputTokens, outputTokens))
			fmt.Fprint(g.writer, tokenMsg)
		}
	}

	return resp, nil
}

func (g *TokenUsagePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}
