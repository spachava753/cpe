package commands

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/render"
)

// MCPListServersFromConfigOptions contains CLI-facing inputs for listing MCP
// servers from resolved config.
type MCPListServersFromConfigOptions struct {
	ConfigPath string
	ModelRef   string
	Writer     io.Writer
}

// MCPListServersFromConfig resolves config and lists configured MCP servers.
func MCPListServersFromConfig(ctx context.Context, opts MCPListServersFromConfigOptions) error {
	cfg, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{ModelRef: opts.ModelRef})
	if err != nil {
		return err
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return MCPListServers(ctx, MCPListServersOptions{
		MCPServers: cfg.MCPServers,
		Writer:     writer,
	})
}

// MCPInfoFromConfigOptions contains CLI-facing inputs for MCP server info.
type MCPInfoFromConfigOptions struct {
	ConfigPath string
	ModelRef   string
	ServerName string
	Writer     io.Writer
	Timeout    time.Duration
}

// MCPInfoFromConfig resolves config and displays server info.
func MCPInfoFromConfig(ctx context.Context, opts MCPInfoFromConfigOptions) error {
	cfg, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{ModelRef: opts.ModelRef})
	if err != nil {
		return err
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return MCPInfo(ctx, MCPInfoOptions{
		MCPServers: cfg.MCPServers,
		ServerName: opts.ServerName,
		Writer:     writer,
		Timeout:    opts.Timeout,
	})
}

// MCPListToolsFromConfigOptions contains CLI-facing inputs for listing tools.
type MCPListToolsFromConfigOptions struct {
	ConfigPath   string
	ModelRef     string
	ServerName   string
	Writer       io.Writer
	ShowAll      bool
	ShowFiltered bool
	Renderer     render.Iface
}

// MCPListToolsFromConfig resolves config and lists tools on one server.
func MCPListToolsFromConfig(ctx context.Context, opts MCPListToolsFromConfigOptions) error {
	cfg, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{ModelRef: opts.ModelRef})
	if err != nil {
		return err
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}
	renderer := opts.Renderer
	if renderer == nil {
		renderer = &render.PlainTextRenderer{}
		if render.IsTTYWriter(writer) {
			renderer = render.NewGlamourRendererForWriter(writer)
		}
	}

	return MCPListTools(ctx, MCPListToolsOptions{
		MCPServers:   cfg.MCPServers,
		ServerName:   opts.ServerName,
		Writer:       writer,
		ShowAll:      opts.ShowAll,
		ShowFiltered: opts.ShowFiltered,
		Renderer:     renderer,
	})
}

// MCPCallToolFromConfigOptions contains CLI-facing inputs for tool execution.
type MCPCallToolFromConfigOptions struct {
	ConfigPath string
	ModelRef   string
	ServerName string
	ToolName   string
	ToolArgs   map[string]any
	Writer     io.Writer
}

// MCPCallToolFromConfig resolves config and calls one MCP tool.
func MCPCallToolFromConfig(ctx context.Context, opts MCPCallToolFromConfigOptions) error {
	cfg, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{ModelRef: opts.ModelRef})
	if err != nil {
		return err
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return MCPCallTool(ctx, MCPCallToolOptions{
		MCPServers: cfg.MCPServers,
		ServerName: opts.ServerName,
		ToolName:   opts.ToolName,
		ToolArgs:   opts.ToolArgs,
		Writer:     writer,
	})
}

// MCPCodeDescFromConfigOptions contains CLI-facing inputs for generating the
// execute_go_code description from resolved config.
type MCPCodeDescFromConfigOptions struct {
	ConfigPath string
	ModelRef   string
	Writer     io.Writer
	Renderer   render.Iface
}

// MCPCodeDescFromConfig resolves config and prints the code mode description.
func MCPCodeDescFromConfig(ctx context.Context, opts MCPCodeDescFromConfigOptions) error {
	cfg, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{ModelRef: opts.ModelRef})
	if err != nil {
		return err
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}
	renderer := opts.Renderer
	if renderer == nil {
		renderer = &render.PlainTextRenderer{}
		if render.IsTTYWriter(writer) {
			renderer = render.NewGlamourRendererForWriter(writer)
		}
	}

	return MCPCodeDesc(ctx, MCPCodeDescOptions{
		MCPServers: cfg.MCPServers,
		CodeMode:   cfg.CodeMode,
		Writer:     writer,
		Renderer:   renderer,
	})
}
