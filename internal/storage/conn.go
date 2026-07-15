package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultFilename is the conversation database filename in
// CPE's user config directory when no explicit path is configured.
const DefaultFilename = ".cpeconvo"

// ResolveStoragePath resolves a CLI or environment database path
// into the effective SQLite path.
//
// An empty path selects .cpeconvo in CPE's user config directory. Explicit
// paths support ~ and ~/... home expansion; absolute and relative paths are
// otherwise cleaned without changing whether they are relative.
func ResolveStoragePath(rawPath string) (string, error) {
	if rawPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		return filepath.Join(configDir, "cpe", DefaultFilename), nil
	}

	path, err := expandHomePath(rawPath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

// NewConvoDB opens and initializes CPE's SQLite conversation
// storage. It creates the database's parent directory with user-only
// permissions when needed. The caller must close the returned Sqlite.
func NewConvoDB(ctx context.Context, rawPath string) (*Sqlite, error) {
	dbPath, err := ResolveStoragePath(rawPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("create conversation storage directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open conversation database: %w", err)
	}

	store, err := NewSqlite(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize conversation database: %w", err)
	}
	store.ownedDB = db
	return store, nil
}

func expandHomePath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for %q: %w", path, err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("unsupported home path format %q (use ~/...)", path)
	}
	return path, nil
}
