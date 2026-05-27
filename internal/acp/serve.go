package acp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/coder/acp-go-sdk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
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

	slog.SetDefault(
		slog.New(
			slog.NewTextHandler(opts.Stderr, &slog.HandlerOptions{
				AddSource: true,
				Level:     slog.LevelDebug,
			}),
		),
	)

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
	runtimeFactory := func(conn *acp.AgentSideConnection, modelRef string) (acpRuntime, error) {
		cfg, err := config.ResolveFromRaw(rawCfg, config.RuntimeOptions{
			ModelRef: modelRef,
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
			conn:        conn,
		}

		ca := closerAgent{
			Loop: &l,
		}

		// connecting to mcps
		// TODO: we connect to mcp servers for each active session, we really need a way to pool connections or something
		mcpState, err := mcp.InitializeConnections(ctx, cfg.MCPServers)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize MCP connections: %w", err)
		}
		ca.mcpState = mcpState

		slog.Debug("initialized mcp connections")

		// Check if code mode is enabled
		codeModeEnabled := cfg.CodeMode != nil && cfg.CodeMode.Enabled
		slog.Debug("code mode config", slog.Bool("enabled", codeModeEnabled))

		if codeModeEnabled {
			// Partition tools into code-mode and excluded
			var excludedToolNames []string
			if cfg.CodeMode.ExcludedTools != nil {
				excludedToolNames = cfg.CodeMode.ExcludedTools
			}

			codeModeServers, excludedByServer := codemode.PartitionTools(mcpState, excludedToolNames)

			// Run collision detection on code-mode tools
			codeModeToolNames := codemode.GetCodeModeToolNames(codeModeServers)
			if err := codemode.CheckToolNameCollisions(codeModeToolNames); err != nil {
				ca.Close()
				return nil, err
			}

			// Collect all code-mode tools for tool description generation
			var allCodeModeTools []*mcpsdk.Tool
			for _, serverInfo := range codeModeServers {
				allCodeModeTools = append(allCodeModeTools, serverInfo.Tools...)
			}

			// Always register execute_go_code when code mode is enabled, even without MCP tools.
			// The tool provides access to the Go standard library for file I/O, etc.
			executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool(allCodeModeTools, cfg.CodeMode.MaxTimeout)
			if err != nil {
				ca.Close()
				return nil, fmt.Errorf("failed to generate execute_go_code tool: %w", err)
			}

			callback := &codemode.ExecuteGoCodeCallback{
				Servers:              codeModeServers,
				MaxTimeout:           cfg.CodeMode.MaxTimeout,
				LargeOutputCharLimit: codemode.ResolveLargeOutputCharLimit(cfg.CodeMode.LargeOutputCharLimit, cfg.Model.ContextWindow),
				LocalModulePaths:     cfg.CodeMode.LocalModulePaths,
			}

			if err := l.Register(executeGoCodeTool, callback); err != nil {
				ca.Close()
				return nil, fmt.Errorf("failed to register execute_go_code tool: %w", err)
			}

			// Register excluded tools normally
			for serverName, tools := range excludedByServer {
				conn := mcpState.Connections[serverName]
				for _, mcpTool := range tools {
					gaiTool, err := mcp.ToGaiTool(mcpTool)
					if err != nil {
						ca.Close()
						return nil, fmt.Errorf("converting tool %s: %w", mcpTool.Name, err)
					}
					if err := l.Register(gaiTool, mcp.NewToolCallback(conn.ClientSession, serverName, mcpTool.Name, conn.Config)); err != nil {
						ca.Close()
						return nil, fmt.Errorf("failed to register excluded tool %s: %w", mcpTool.Name, err)
					}
				}
			}
		} else {
			// Code mode disabled: register all tools normally
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
	asc := acp.NewAgentSideConnection(&ag, opts.Stdout, opts.Stdin)
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

// closerAgent is a type that embeds [Loop] and implementes a close function to close the mcp connections
type closerAgent struct {
	*Loop
	mcpState *mcp.MCPState
}

func (c *closerAgent) Close() error {
	return c.mcpState.Close()
}
