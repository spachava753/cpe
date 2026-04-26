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
)

// ExecuteRootOptions contains pre-resolved dependencies and inputs for
// ExecuteRoot. The CLI layer is responsible for populating this struct from
// flags/environment/config and for initializing optional persistence.
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
	Config config.Config
	// CustomURL overrides the model's base URL
	CustomURL string

	// DialogSaver is the storage backend for conversation persistence.
	// When nil, no conversations are saved (incognito mode).
	// The caller is responsible for initializing and closing the underlying store.
	DialogSaver storage.DialogSaver

	// InitialDialog is the pre-loaded conversation history to continue from.
	// When non-empty, the new user message is appended to this dialog.
	// The caller is responsible for loading the dialog (e.g. via
	// storage.GetDialogForMessage). When empty, a new conversation is started.
	InitialDialog gai.Dialog

	// Stdout is where model responses are written.
	// If nil, defaults to os.Stdout.
	Stdout io.Writer
	// Stderr is where to write status messages.
	// If nil, defaults to os.Stderr.
	Stderr io.Writer
}

// ExecuteRoot runs the core generation orchestration.
//
// Boundary contract:
//   - Does: process input blocks, render system prompt, initialize MCP,
//     construct generator, and execute generation.
//   - Does not: parse CLI flags, load/resolve config files, or open/close
//     storage databases (except using the provided DialogSaver abstraction).
func ExecuteRoot(ctx context.Context, opts ExecuteRootOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	// effectiveConfig aliases opts.Config; field updates below intentionally apply
	// to the runtime config for this invocation.
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

	// Initialize MCP connections (fails fast on any error)
	mcpState, err := mcp.InitializeConnections(ctx, effectiveConfig.MCPServers)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP connections: %w", err)
	}
	defer mcpState.Close()

	// Build generator options
	generatorOpts := []agent.GeneratorOption{
		agent.WithStdout(stdout),
	}
	// Enable turn-lifecycle persistence if storage is available (not incognito mode).
	if opts.DialogSaver != nil {
		generatorOpts = append(generatorOpts, agent.WithDialogSaver(opts.DialogSaver))
	}

	// Create the generator with optional turn-lifecycle persistence.
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

	// Call the generation logic.
	// Persistence and user-facing output are handled by the turn-lifecycle middleware when not in incognito mode.
	return Generate(ctx, GenerateOptions{
		UserBlocks:    userBlocks,
		InitialDialog: opts.InitialDialog,
		GenOptsFunc: func(dialog gai.Dialog) *gai.GenOpts {
			return agent.BuildGenOptsForDialog(effectiveConfig.Model, effectiveConfig.GenerationDefaults, dialog)
		},
		Generator: toolGen,
		Stderr:    stderr,
	})
}
