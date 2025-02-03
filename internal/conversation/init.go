package conversation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// InitDB initializes the SQLite database
func InitDB(dbPath string) error {
	// Create database directory if it doesn't exist
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create tables
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			parent_id TEXT REFERENCES conversations(id),
			user_message TEXT NOT NULL,
			executor_data BLOB NOT NULL,
			created_at TIMESTAMP NOT NULL,
			model TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	return nil
}