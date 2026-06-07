package acp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"

	"github.com/coder/acp-go-sdk"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
	"github.com/spachava753/cpe/internal/textedit"
)

type ServeOptions struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	ConfigPath string
	DbPath     string
}

func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Stdout == nil {
		return errors.New("provided stdout cannot be nil")
	}
	if opts.Stderr == nil {
		return errors.New("provided stderr cannot be nil")
	}

	handlers := []slog.Handler{
		slog.NewJSONHandler(opts.Stderr, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
		}),
	}
	if slog.Default().Handler() != nil {
		handlers = append(handlers, slog.Default().Handler())
	}
	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))

	rawCfg, err := config.LoadRawConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("could not load config: %v", err)
	}

	slog.Debug("loaded config file", slog.String("path", opts.ConfigPath))

	dbPath, err := config.ResolveConversationStoragePath(opts.DbPath)
	if err != nil {
		return fmt.Errorf("invalid db path: %w", err)
	}

	slog.Debug("loaded db", slog.String("path", dbPath))

	storageDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		slog.Debug("closing db connection")
		if err := storageDB.Close(); err != nil {
			slog.Error("could not close db conn", slog.String("err", err.Error()))
		}
	}()

	sqliteStorage, err := storage.NewSqlite(ctx, storageDB)
	if err != nil {
		return fmt.Errorf("failed to initialize dialog storage: %w", err)
	}

	// TODO: we should refactor the runtime factory to be made from the session config options
	runtimeFactory := func(
		runtimeOpts runtimeOpts,
	) (acpRuntime, error) {
		cfg, err := config.ResolveFromRaw(rawCfg, config.RuntimeOptions{
			ModelRef: runtimeOpts.modelRef,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to resolve model config: %v", err)
		}

		slog.Debug("config resolved")

		// Load and render system prompt
		systemPrompt, err := commands.LoadSystemPrompt(ctx, commands.LoadSystemPromptOptions{
			SystemPromptPath: cfg.SystemPromptPath,
			Config:           cfg,
			Stderr:           opts.Stderr,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to load system prompt: %v", err)
		}

		slog.Debug("system prompt loaded", slog.String("path", cfg.SystemPromptPath))

		genBase, err := agent.InitGeneratorFromModel(ctx, cfg.Model, systemPrompt, cfg.Timeout)
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
			DialogSaver: sqliteStorage,
			Cfg:         cfg,
			G:           gen,
			conn:        runtimeOpts.conn,
		}

		ca := closerAgent{
			Loop: &l,
		}

		if !cfg.DisableEditTool {
			textEditTool, textEditCallback := textedit.MakeTool()
			if err := l.Register(textEditTool, textEditCallback); err != nil {
				return nil, fmt.Errorf("failed to register text_edit tool: %w", err)
			}
		}

		mcpServersConfig, err := mergeACPServerConfigs(cfg.MCPServers, runtimeOpts.mcpServers)
		if err != nil {
			return nil, err
		}

		// connecting to mcps
		// TODO: we connect to mcp servers for each active session, we really need a way to pool connections or something
		mcpState, err := mcp.InitializeConnections(ctx, mcpServersConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize MCP connections: %w", err)
		}
		ca.mcpState = mcpState

		slog.Debug("initialized mcp connections")

		codeModeEnabled := cfg.CodeMode != nil && cfg.CodeMode.Enabled
		slog.Debug("code mode config", slog.Bool("enabled", codeModeEnabled))
		if codeModeEnabled {
			executeGoCodeTool := codemode.MakeTool(cfg.CodeMode.MaxTimeout)
			callback := &codemode.ExecuteGoCodeCallback{
				Cwd:                  runtimeOpts.cwd,
				SessionId:            runtimeOpts.sessionId,
				MaxTimeout:           cfg.CodeMode.MaxTimeout,
				LargeOutputCharLimit: codemode.ResolveLargeOutputCharLimit(cfg.CodeMode.LargeOutputCharLimit, cfg.Model.ContextWindow),
				Conn:                 runtimeOpts.conn,
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
				if err := l.Register(gaiTool, mcp.NewToolCallback(conn.ClientSession, serverName, mcpTool.Name, conn.Config)); err != nil {
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

	ag := Agent{
		activeSessions: new(sync.Map[acp.SessionId, *sync.Guard[session]]),
		rawCfg:         rawCfg,
		db:             sqliteStorage,
		genId: func() acp.SessionId {
			return acp.SessionId(storage.GenerateId())
		},
		runtimeFactory: runtimeFactory,
	}
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
	asc := acp.NewAgentSideConnection(&ag, stdout, stdin)
	ag.conn = asc
	asc.SetLogger(slog.Default())
	select {
	case <-asc.Done():
	case <-ctx.Done():
		return ctx.Err()
	}

	// TODO: we should close on connection end and clean up mcp connections
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
	switch {
	case server.Stdio != nil:
		env := make(map[string]string, len(server.Stdio.Env))
		for _, variable := range server.Stdio.Env {
			env[variable.Name] = variable.Value
		}
		return server.Stdio.Name, mcpconfig.ServerConfig{
			Type:    "stdio",
			Command: server.Stdio.Command,
			Args:    server.Stdio.Args,
			Env:     env,
		}, nil
	case server.Http != nil:
		return server.Http.Name, mcpconfig.ServerConfig{
			Type:    "http",
			URL:     server.Http.Url,
			Headers: acpHTTPHeaders(server.Http.Headers),
		}, nil
	case server.Sse != nil:
		return server.Sse.Name, mcpconfig.ServerConfig{
			Type:    "sse",
			URL:     server.Sse.Url,
			Headers: acpHTTPHeaders(server.Sse.Headers),
		}, nil
	case server.Acp != nil:
		return server.Acp.Name, mcpconfig.ServerConfig{}, errors.New("ACP transport is not supported")
	default:
		return "", mcpconfig.ServerConfig{}, errors.New("transport is required")
	}
}

func acpHTTPHeaders(headers []acp.HttpHeader) map[string]string {
	mapped := make(map[string]string, len(headers))
	for _, header := range headers {
		mapped[header.Name] = header.Value
	}
	return mapped
}

// closerAgent is a type that embeds [Loop] and implementes a close function to close the mcp connections
type closerAgent struct {
	*Loop
	mcpState *mcp.MCPState
}

func (c *closerAgent) Close() error {
	return c.mcpState.Close()
}
