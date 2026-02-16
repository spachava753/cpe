package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/subagentlog"
)

// ExecuteRootOptions contains all parameters for the root command execution
type ExecuteRootOptions struct {
	// Args are the command line arguments (prompt)
	Args []string
	// InputPaths are file paths or URLs to include
	InputPaths []string
	// Stdin is the stdin reader
	Stdin io.Reader
	// SkipStdin disables reading from stdin
	SkipStdin bool

	// Config is the resolved effective configuration.
	// The caller is responsible for loading and resolving the config.
	Config *config.Config
	// CustomURL overrides the model's base URL
	CustomURL string

	// MessageDB is the storage backend for conversation persistence.
	// When nil, no conversations are saved (incognito mode).
	// The caller is responsible for initializing and closing the underlying store.
	MessageDB storage.MessageDB

	// ContinueID is the message ID to continue from
	ContinueID string
	// NewConversation starts a new conversation
	NewConversation bool

	// Stdout is where model responses are written.
	// If nil, defaults to os.Stdout.
	Stdout io.Writer
	// Stderr is where to write status messages.
	// If nil, defaults to os.Stderr.
	Stderr io.Writer
	// VerboseSubagent enables verbose subagent output
	VerboseSubagent bool
}

// ExecuteRoot runs the main CPE generation flow
func ExecuteRoot(ctx context.Context, opts ExecuteRootOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	effectiveConfig := opts.Config

	// Process user input
	userBlocks, err := ProcessUserInput(ctx, ProcessUserInputOptions{
		Args:       opts.Args,
		InputPaths: opts.InputPaths,
		Stdin:      opts.Stdin,
		SkipStdin:  opts.SkipStdin,
	})
	if err != nil {
		return fmt.Errorf("could not process user input: %w", err)
	}

	// Load and render system prompt
	systemPrompt, err := LoadSystemPrompt(ctx, LoadSystemPromptOptions{
		SystemPromptPath: effectiveConfig.SystemPromptPath,
		Config:           effectiveConfig,
		Stderr:           stderr,
	})
	if err != nil {
		return err
	}

	// Apply custom URL override if provided
	if opts.CustomURL != "" {
		effectiveConfig.Model.BaseUrl = opts.CustomURL
	}

	// Start the subagent event server if we're the root process.
	// When CPE_SUBAGENT_LOGGING_ADDRESS is set, we're running as a subagent
	// and should not start another server.
	var subagentLoggingAddress string
	if os.Getenv(subagentlog.SubagentLoggingAddressEnv) == "" {
		// Determine render mode for subagent events
		renderMode := subagentlog.RenderModeConcise
		if opts.VerboseSubagent {
			renderMode = subagentlog.RenderModeVerbose
		}
		renderer := subagentlog.NewRenderer(agent.NewRenderer(), renderMode)
		stderrWriter := subagentlog.NewSyncWriter(stderr)
		eventServer := subagentlog.NewServer(func(event subagentlog.Event) {
			rendered := renderer.RenderEvent(event)
			if rendered != "" {
				stderrWriter.WriteString(rendered)
			}
		})

		subagentLoggingAddress, err = eventServer.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start subagent event server: %w", err)
		}

		// Set the env var so code mode subprocesses inherit it
		os.Setenv(subagentlog.SubagentLoggingAddressEnv, subagentLoggingAddress)
	}

	// Initialize MCP connections (fails fast on any error)
	mcpState, err := mcp.InitializeConnections(ctx, effectiveConfig.MCPServers, subagentLoggingAddress)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP connections: %w", err)
	}
	defer mcpState.Close()

	// Build generator options
	generatorOpts := []agent.GeneratorOption{
		agent.WithStdout(stdout),
	}
	// Enable saving middleware if storage is available (not incognito mode)
	if opts.MessageDB != nil {
		generatorOpts = append(generatorOpts, agent.WithDialogSaver(opts.MessageDB))
	}

	// Create the generator with optional saving middleware
	toolGen, err := agent.NewGenerator(
		ctx,
		effectiveConfig,
		systemPrompt,
		mcpState,
		generatorOpts...,
	)
	if err != nil {
		return fmt.Errorf("failed to create tool capable generator: %w", err)
	}

	genOpts := effectiveConfig.GenerationDefaults

	// Call the generation logic
	// Saving is handled by the SavingMiddleware in the generator pipeline when not in incognito mode.
	return Generate(ctx, GenerateOptions{
		UserBlocks:      userBlocks,
		ContinueID:      opts.ContinueID,
		NewConversation: opts.NewConversation,
		GenOptsFunc: func(dialog gai.Dialog) *gai.GenOpts {
			return genOpts
		},
		MessageDB: opts.MessageDB,
		Generator: toolGen,
		Stderr:    stderr,
	})
}
