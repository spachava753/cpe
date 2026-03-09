package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

// ResolveConversationDBPath resolves the configured conversation storage path,
// falling back to the default local database when no config file is found.
func ResolveConversationDBPath(explicitConfigPath string) (string, error) {
	rawCfg, resolvedConfigPath, err := config.LoadRawConfigWithPath(explicitConfigPath)
	if err != nil {
		if explicitConfigPath == "" && errors.Is(err, config.ErrConfigNotFound) {
			return config.DefaultConversationStoragePath, nil
		}
		return "", err
	}

	dbPath, err := config.ResolveConversationStoragePath(rawCfg.Defaults, resolvedConfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve conversation storage path: %w", err)
	}
	return dbPath, nil
}

// OpenConversationStorage opens the configured conversation database and
// initializes the sqlite-backed dialog storage wrapper.
func OpenConversationStorage(ctx context.Context, explicitConfigPath string) (*sql.DB, *storage.Sqlite, error) {
	dbPath, err := ResolveConversationDBPath(explicitConfigPath)
	if err != nil {
		return nil, nil, err
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	dialogStorage, err := storage.NewSqlite(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to initialize dialog storage: %w", err)
	}

	return db, dialogStorage, nil
}
