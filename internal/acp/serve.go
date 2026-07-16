package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"

	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	cpelogging "github.com/spachava753/cpe/internal/logging"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
	"github.com/spachava753/cpe/internal/textedit"
)

type ServeOptions struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	RawConfig *config.RawConfig
	Store     *storage.Sqlite
}

type serverRuntimeCreator struct {
	rawCfg *config.RawConfig
	store  *storage.Sqlite
	conn   *acp.AgentConnection
}

var (
	_                            RuntimeCreator = (*serverRuntimeCreator)(nil)
	initializeGeneratorFromModel                = agent.InitGeneratorFromModel
	initializeMCPConnections                    = mcp.InitializeConnections
)

func (c *serverRuntimeCreator) Create(ctx context.Context, s session, caps acp.ClientCapabilities) (runtime, error) {
	cfg, err := config.ResolveFromRaw(c.rawCfg, config.RuntimeOptions{
		ModelRef: s.model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve model config: %v", err)
	}

	slog.DebugContext(ctx, "config resolved")

	// Load and render system prompt
	systemPrompt, err := config.LoadSystemPrompt(ctx, config.LoadSystemPromptOptions{
		SystemPromptPath: cfg.SystemPromptPath,
		Config:           cfg,
		Skills:           s.skillCatalog.ModelVisible(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load system prompt: %v", err)
	}

	slog.DebugContext(ctx, "system prompt loaded", slog.String("path", cfg.SystemPromptPath))

	// Model clients and MCP transports are session-scoped. Link their context to
	// the creation context only until setup finishes so early cancellation still
	// aborts cold start, but a completed prompt/load RPC does not kill long-lived
	// auth token sources or MCP servers.
	runtimeCtx, cancelRuntime := context.WithCancel(context.WithoutCancel(ctx))
	stopRuntimeOnCreateCancel := context.AfterFunc(ctx, cancelRuntime)
	runtimeCtxOwned := false
	defer func() {
		if !runtimeCtxOwned {
			stopRuntimeOnCreateCancel()
			cancelRuntime()
		}
	}()

	genBase, err := initializeGeneratorFromModel(runtimeCtx, cfg.Model, systemPrompt, cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create generator: %v", err)
	}

	gen, ok := genBase.(gai.ToolCallingGenerator)
	if !ok {
		return nil, fmt.Errorf("generator does not implement ToolCallingGenerator interface")
	}

	wrappers := []gai.WrapperFunc{
		agent.WithBlockFilter(cfg.Model.Type),
	}

	wrapped := gai.Wrap(gen, wrappers...)
	gen, ok = wrapped.(gai.ToolCallingGenerator)
	if !ok {
		return nil, fmt.Errorf("wrapped generator does not implement ToolCallingGenerator interface")
	}

	l := Loop{
		Store: c.store,
		Cfg:   cfg,
		G:     gen,
		conn:  c.conn,
	}

	ca := closerAgent{
		Loop: &l,
	}

	if !cfg.DisableEditTool {
		textEditTool, textEditCallback := textedit.MakeTool(
			s.id,
			c.conn,
		)
		if err := l.Register(textEditTool, textEditCallback); err != nil {
			return nil, fmt.Errorf("failed to register text_edit tool: %w", err)
		}
	}

	mcpServersConfig, err := mergeACPServerConfigs(cfg.MCPServers, s.mcpServers)
	if err != nil {
		return nil, err
	}

	// connecting to mcps
	// TODO: we connect to mcp servers for each active session, we really need a way to pool connections or something
	mcpState, err := initializeMCPConnections(runtimeCtx, mcpServersConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP connections: %w", err)
	}
	ca.mcpState = mcpState
	ca.cancelRuntime = cancelRuntime
	if !stopRuntimeOnCreateCancel() && ctx.Err() != nil {
		_ = ca.Close()
		return nil, ctx.Err()
	}
	runtimeCtxOwned = true

	slog.DebugContext(ctx, "initialized mcp connections")

	codeModeEnabled := cfg.CodeMode != nil && cfg.CodeMode.Enabled
	slog.DebugContext(ctx, "code mode config", slog.Bool("enabled", codeModeEnabled))
	if codeModeEnabled {
		executeGoCodeTool := codemode.MakeTool(cfg.CodeMode.MaxTimeout)
		callback := &codemode.ExecuteGoCodeCallback{
			Cwd:                  s.cwd,
			SessionId:            s.id,
			MaxTimeout:           cfg.CodeMode.MaxTimeout,
			LargeOutputCharLimit: codemode.ResolveLargeOutputCharLimit(cfg.CodeMode.LargeOutputCharLimit, cfg.Model.ContextWindow),
			Conn:                 c.conn,
			TerminalSupport:      caps.Terminal,
		}

		if err := l.Register(executeGoCodeTool, callback); err != nil {
			ca.Close()
			return nil, fmt.Errorf("failed to register execute_go_code tool: %w", err)
		}
	}

	for serverName, conn := range mcpState.Connections {
		for _, mcpTool := range conn.Tools {
			gaiTool, err := mcp.ToGaiTool(mcpTool)
			if err != nil {
				ca.Close()
				return nil, fmt.Errorf("converting tool %s: %w", mcpTool.Name, err)
			}
			if err := l.Register(gaiTool, mcp.NewToolCallback(
				c.conn,
				s.id,
				conn.ClientSession,
				serverName,
				mcpTool.Name,
				conn.Config,
			)); err != nil {
				ca.Close()
				return nil, fmt.Errorf("failed to register tool %s: %w", mcpTool.Name, err)
			}
		}
	}

	if cfg.Compaction != nil {
		if err := l.Register(cfg.Compaction.Tool, nil); err != nil {
			ca.Close()
			return nil, fmt.Errorf("failed to register compaction tool: %w", err)
		}
	}

	return &ca, nil
}

func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Stdout == nil {
		return errors.New("provided stdout cannot be nil")
	}
	if opts.Stderr == nil {
		return errors.New("provided stderr cannot be nil")
	}

	handlers := []slog.Handler{
		cpelogging.NewProcessHandler(slog.NewJSONHandler(opts.Stderr, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
		})),
	}
	if slog.Default().Handler() != nil {
		handlers = append(handlers, slog.Default().Handler())
	}
	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))

	// for the purposes of access logging, log all
	// incoming and outgoing messages, to help with
	// debugging communication between ACP client
	// and server
	stdin := io.TeeReader(opts.Stdin, &rpcLogger{
		log: slog.Default(),
		dir: "incoming",
	})
	stdout := io.MultiWriter(opts.Stdout, &rpcLogger{
		log: slog.Default(),
		dir: "outgoing",
	})
	transport := &acp.IOTransport{
		Reader: io.NopCloser(stdin),
		Writer: nopWriteCloser{Writer: stdout},
	}
	return Run(ctx, transport, RunOptions{
		RawConfig: opts.RawConfig,
		Store:     opts.Store,
		Stderr:    opts.Stderr,
	})
}

// Run starts the ACP agent over the provided transport.
func Run(ctx context.Context, transport acp.Transport, opts RunOptions) error {
	if opts.Stderr == nil {
		return errors.New("provided stderr cannot be nil")
	}

	if opts.RawConfig == nil {
		return errors.New("provided raw config cannot be nil")
	}
	if opts.Store == nil {
		return errors.New("provided conversation store cannot be nil")
	}

	// TODO: we should refactor the runtime factory to be made from the session config options
	runtimeFactory := &serverRuntimeCreator{
		rawCfg: opts.RawConfig,
		store:  opts.Store,
	}

	ag := Agent{
		activeSessions: new(sync.Map[acp.SessionId, *sync.Guard[session]]),
		rawCfg:         opts.RawConfig,
		db:             opts.Store,
		genId: func() acp.SessionId {
			return acp.SessionId(storage.GenerateId())
		},
		runtimeFactory: runtimeFactory,
	}
	return acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
		ag.conn = conn
		runtimeFactory.conn = conn
		return &ag
	})
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error {
	return nil
}

type rpcLogger struct {
	b   bytes.Buffer
	log *slog.Logger
	dir string
}

// Write implements [io.Writer]. It writes to its buffer, then reads
func (r *rpcLogger) Write(p []byte) (int, error) {
	n, err := r.b.Write(p)
	// loop on contained JSON RPC frames,
	// the buffer may contain multiple
	for {
		delim := bytes.IndexRune(r.b.Bytes(), '\n')
		if delim < 0 {
			return n, err
		}

		// we have a complete JSON RPC message in the buffer, flush it
		rpcBytes := r.b.Next(delim)
		type msg struct {
			ID     json.RawMessage `json:"id"`
			Method *string         `json:"method,omitempty"`
			Params json.RawMessage `json:"params,omitempty"`
			Result json.RawMessage `json:"result,omitempty"`
			Error  json.RawMessage `json:"error,omitempty"`
		}
		var m msg
		if err := json.Unmarshal(rpcBytes, &m); err != nil {
			r.log.LogAttrs(
				context.Background(),
				slog.LevelDebug,
				"jsonrpc frame parse error",
				slog.String("direction", r.dir),
				slog.String("err", err.Error()),
				slog.String("raw", string(rpcBytes)),
			)
		} else {
			attrs := []slog.Attr{
				slog.String("direction", r.dir),
			}
			if len(m.ID) > 0 {
				attrs = append(attrs, slog.Any("id", m.ID))
			}
			scope := m.Params
			if m.Method == nil {
				scope = m.Result
			}
			attrs = append(attrs, rpcLogScopeAttrs(scope)...)
			if m.Method != nil {
				attrs = append(attrs, slog.String("method", *m.Method))
				if len(m.Params) > 0 {
					attrs = append(attrs, slog.Any("params", m.Params))
				}
				if len(m.ID) > 0 {
					attrs = append(attrs, slog.String("type", "request"))
				} else {
					attrs = append(attrs, slog.String("type", "notification"))
				}
			} else {
				if len(m.Result) > 0 {
					attrs = append(attrs, slog.Any("result", m.Result))
				}
				if len(m.Error) > 0 {
					attrs = append(attrs, slog.Any("error", m.Error))
				}
				attrs = append(attrs, slog.String("type", "response"))
			}
			r.log.LogAttrs(
				context.Background(),
				slog.LevelDebug,
				"jsonrpc frame",
				attrs...,
			)
		}

		// read the newline rune
		if r.b.Len() > 0 {
			r.b.Next(1)
		}
	}
}

func rpcLogScopeAttrs(raw json.RawMessage) []slog.Attr {
	if len(raw) == 0 {
		return nil
	}
	var scope struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &scope); err != nil {
		return nil
	}
	attrs := make([]slog.Attr, 0, 2)
	if scope.SessionID != "" {
		attrs = append(attrs, slog.String("session_id", scope.SessionID))
	}
	if scope.Cwd != "" {
		attrs = append(attrs, slog.String("cwd", scope.Cwd))
	}
	return attrs
}

func mergeACPServerConfigs(
	configured map[string]mcpconfig.ServerConfig,
	provided []acp.McpServer,
) (map[string]mcpconfig.ServerConfig, error) {
	merged := make(map[string]mcpconfig.ServerConfig, len(configured)+len(provided))
	maps.Copy(merged, configured)

	for i, server := range provided {
		name, cfg, err := acpMCPServerConfig(server)
		if err != nil {
			return nil, fmt.Errorf("acp MCP server[%d]: %w", i, err)
		}
		if name == "" {
			return nil, fmt.Errorf("acp MCP server[%d]: name is required", i)
		}
		if _, exists := merged[name]; exists {
			return nil, fmt.Errorf("acp MCP server %q conflicts with an existing MCP server", name)
		}
		merged[name] = cfg
	}

	return merged, nil
}

func acpMCPServerConfig(server acp.McpServer) (string, mcpconfig.ServerConfig, error) {
	switch server.Type {
	case "":
		env := make(map[string]string, len(server.Env))
		for _, variable := range server.Env {
			env[variable.Name] = variable.Value
		}
		return server.Name, mcpconfig.ServerConfig{
			Type:    "stdio",
			Command: server.Command,
			Args:    server.Args,
			Env:     env,
		}, nil
	case acp.McpServerTypeHttp:
		return server.Name, mcpconfig.ServerConfig{
			Type:    "http",
			URL:     server.Url,
			Headers: acpHTTPHeaders(server.Headers),
		}, nil
	case acp.McpServerTypeSse:
		return server.Name, mcpconfig.ServerConfig{
			Type:    "sse",
			URL:     server.Url,
			Headers: acpHTTPHeaders(server.Headers),
		}, nil
	case acp.McpServerTypeAcp:
		return server.Name, mcpconfig.ServerConfig{}, errors.New("ACP transport is not supported")
	default:
		return "", mcpconfig.ServerConfig{}, fmt.Errorf("unsupported transport %q", server.Type)
	}
}

func acpHTTPHeaders(headers []acp.HttpHeader) map[string]string {
	mapped := make(map[string]string, len(headers))
	for _, header := range headers {
		mapped[header.Name] = header.Value
	}
	return mapped
}

// closerAgent is a type that embeds [Loop] and implements a close function to close the mcp connections.
type closerAgent struct {
	*Loop
	mcpState      *mcp.MCPState
	cancelRuntime context.CancelFunc
}

func (c *closerAgent) Close() error {
	if c.cancelRuntime != nil {
		defer c.cancelRuntime()
	}
	if c.mcpState == nil {
		return nil
	}
	return c.mcpState.Close()
}
