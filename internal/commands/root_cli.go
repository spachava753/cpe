package commands

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

// ExecuteRootCLIOptions contains CLI-facing inputs for one root command run.
// Unlike ExecuteRootOptions, this struct carries unresolved config/storage
// inputs so the commands package remains the orchestration hub beneath cmd.
type ExecuteRootCLIOptions struct {
	Args            []string
	InputPaths      []string
	Stdin           io.Reader
	SkipStdin       bool
	ConfigPath      string
	ModelRef        string
	GenParams       *gai.GenOpts
	Timeout         string
	CustomURL       string
	ContinueID      string
	NewConversation bool
	IncognitoMode   bool
	Stdout          io.Writer
	Stderr          io.Writer
	VerboseSubagent bool
}

// ExecuteRootCLI resolves CLI/runtime dependencies and delegates generation
// orchestration to ExecuteRoot.
func ExecuteRootCLI(ctx context.Context, opts ExecuteRootCLIOptions) error {
	effectiveConfig, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{
		ModelRef:  opts.ModelRef,
		GenParams: opts.GenParams,
		Timeout:   opts.Timeout,
	})
	if err != nil {
		return fmt.Errorf("failed to resolve configuration: %w", err)
	}

	var dialogSaver storage.DialogSaver
	var initialDialog gai.Dialog

	if opts.IncognitoMode {
		if opts.ContinueID != "" {
			return fmt.Errorf("--continue cannot be used with --incognito")
		}
	} else {
		storageDB, err := sql.Open("sqlite3", effectiveConfig.ConversationStoragePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer storageDB.Close()

		sqliteStorage, err := storage.NewSqlite(ctx, storageDB)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %w", err)
		}
		dialogSaver = sqliteStorage

		initialDialog, err = ResolveInitialDialog(ctx, sqliteStorage, opts.ContinueID, opts.NewConversation)
		if err != nil {
			return fmt.Errorf("failed to resolve conversation history: %w", err)
		}
	}

	return ExecuteRoot(ctx, ExecuteRootOptions{
		Args:            opts.Args,
		InputPaths:      opts.InputPaths,
		Stdin:           opts.Stdin,
		SkipStdin:       opts.SkipStdin,
		Config:          effectiveConfig,
		CustomURL:       opts.CustomURL,
		DialogSaver:     dialogSaver,
		InitialDialog:   initialDialog,
		Stdout:          opts.Stdout,
		Stderr:          opts.Stderr,
		VerboseSubagent: opts.VerboseSubagent,
	})
}
