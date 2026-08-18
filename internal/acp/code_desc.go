package acp

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/render"
)

// CodeDescOptions contains parameters for rendering the code mode description.
type CodeDescOptions struct {
	CodeMode *config.CodeModeConfig
	Writer   io.Writer
	Renderer render.Iface
}

// CodeDesc renders and prints the starlark_repl tool description.
func CodeDesc(_ context.Context, opts CodeDescOptions) error {
	var mdBuilder strings.Builder
	mdBuilder.WriteString("# starlark_repl Tool Description\n\n")

	if opts.CodeMode == nil || !opts.CodeMode.Enabled {
		mdBuilder.WriteString("> **Note:** Code mode is not enabled in current configuration.\n\n")
	}

	mdBuilder.WriteString("---\n\n")
	mdBuilder.WriteString(GenerateToolDescription())

	rendered, err := opts.Renderer.Render(mdBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprintln(opts.Writer, rendered)
	return nil
}

// CodeDescFromConfig resolves config and prints the code mode description.
func CodeDescFromConfig(ctx context.Context, configPath, modelRef string, writer io.Writer) error {
	cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{ModelRef: modelRef})
	if err != nil {
		return err
	}

	if writer == nil {
		writer = os.Stdout
	}
	var renderer render.Iface = render.NewPlainTextRenderer()
	if render.IsTTYWriter(writer) {
		renderer = render.NewGlamourRendererForWriter(writer)
	}

	return CodeDesc(ctx, CodeDescOptions{
		CodeMode: cfg.CodeMode,
		Writer:   writer,
		Renderer: renderer,
	})
}
