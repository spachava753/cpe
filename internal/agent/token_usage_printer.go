package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"github.com/spachava753/gai"
	"golang.org/x/term"
)

type TokenUsagePrinterGenerator struct {
	wrapped  gai.ToolCapableGenerator
	renderer Renderer
}

func NewTokenUsagePrinterGenerator(wrapped gai.ToolCapableGenerator) *TokenUsagePrinterGenerator {
	var renderer Renderer

	style := styles.LightStyleConfig
	if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		style = styles.NoTTYStyleConfig
	}

	style.Document.BlockPrefix = ""

	renderer, _ = glamour.NewTermRenderer(
		glamour.WithStyles(style),
	)
	return &TokenUsagePrinterGenerator{
		wrapped:  wrapped,
		renderer: renderer,
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
			fmt.Fprint(os.Stderr, tokenMsg)
		}
	}

	return resp, nil
}

func (g *TokenUsagePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}
