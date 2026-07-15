package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
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

// NewConvoDB opens and initializes CPE's SQLite conversation storage. It
// creates the database's parent directory, enables WAL, and configures
// connection-local busy timeout, foreign-key, and IMMEDIATE transaction
// settings. The caller must close the returned Sqlite.
func NewConvoDB(ctx context.Context, rawPath string) (*Sqlite, error) {
	dbPath, err := ResolveStoragePath(rawPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("create conversation storage directory: %w", err)
	}

	db, err := sql.Open("sqlite3", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open conversation database: %w", err)
	}
	if err := enableWAL(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	store, err := NewSqlite(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize conversation database: %w", err)
	}
	store.ownedDB = db
	return store, nil
}

func sqliteDSN(path string) string {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(60000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Set("_txlock", "immediate")
	return (&url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: query.Encode(),
	}).String()
}

func enableWAL(ctx context.Context, db *sql.DB) error {
	walCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	for {
		var mode string
		err := db.QueryRowContext(walCtx, "PRAGMA journal_mode = WAL").Scan(&mode)
		if err == nil {
			if strings.EqualFold(mode, "wal") || strings.EqualFold(mode, "memory") {
				return nil
			}
			return fmt.Errorf("journal mode is %q", mode)
		}
		if !errors.Is(err, sqlite3.BUSY) && !errors.Is(err, sqlite3.LOCKED) {
			return err
		}
		select {
		case <-walCtx.Done():
			return fmt.Errorf("wait for database lock: %w", walCtx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
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
