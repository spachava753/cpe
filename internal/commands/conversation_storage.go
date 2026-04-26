package commands

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

// ResolveConversationDBPath resolves the conversation storage path from a CLI flag or environment value.
func ResolveConversationDBPath(rawPath string) (string, error) {
	dbPath, err := config.ResolveConversationStoragePath(rawPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve conversation storage path: %w", err)
	}
	return dbPath, nil
}

// OpenConversationStorage opens the configured conversation database and
// initializes the sqlite-backed dialog storage wrapper.
func OpenConversationStorage(ctx context.Context, rawPath string) (*sql.DB, *storage.Sqlite, error) {
	dbPath, err := ResolveConversationDBPath(rawPath)
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
