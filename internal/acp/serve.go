package acp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/coder/acp-go-sdk"

	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
)

type ServeOptions struct {
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

	rawCfg, err := config.LoadRawConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("could not load config: %v", err)
	}

	conversationDBPath, err := config.ResolveConversationStoragePath(opts.DbPath)
	if err != nil {
		return fmt.Errorf("invalid db path: %w", err)
	}

	storageDB, err := sql.Open("sqlite3", conversationDBPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storageDB.Close()

	sqliteStorage, err := storage.NewSqlite(ctx, storageDB)
	if err != nil {
		return fmt.Errorf("failed to initialize dialog storage: %w", err)
	}

	runtimeFactory := func(modelRef string) acpRuntime {
		resolvedConfig, err := config.ResolveFromRaw(rawCfg, config.RuntimeOptions{
			ModelRef: modelRef,
		})
		if err != nil {
			panic(fmt.Sprintf("failed to create acp runtime: %v", err))
		}
		// Load and render system prompt
		systemPrompt, err := commands.LoadSystemPrompt(ctx, commands.LoadSystemPromptOptions{
			SystemPromptPath: resolvedConfig.SystemPromptPath,
			Config:           resolvedConfig,
			Stderr:           opts.Stderr,
		})
		if err != nil {
			panic(fmt.Sprintf("failed to create acp runtime: %v", err))
		}
		_ = systemPrompt

		panic("unimplemented")
	}

	ag := Agent{
		activeSessions: new(sync.Map[acp.SessionId, sync.Guard[session]]),
		rawCfg:         rawCfg,
		db:             sqliteStorage,
		genId: func() acp.SessionId {
			return acp.SessionId(storage.GenerateId())
		},
		runtimeFactory: runtimeFactory,
	}
	asc := acp.NewAgentSideConnection(&ag, os.Stdout, os.Stdin)
	ag.conn = asc
	<-asc.Done()
	return nil
}
