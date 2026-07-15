package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	gonanoid "github.com/matoous/go-nanoid/v2"
	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/spachava753/cpe/internal/storage/sqlcgen"
)

// DB is the database contract required by NewSqlite.
//
// It is intentionally narrow so callers can pass either *sql.DB or a test
// double. SaveDialog and DeleteMessages rely on BeginTx providing real SQL
// transaction semantics (commit or rollback boundaries).
type DB interface {
	sqlcgen.DBTX
	// ExecContext executes statements outside a transaction. NewSqlite uses it
	// for connection-level PRAGMA setup.
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// BeginTx starts atomic write and serialized schema transactions.
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

const idCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateId returns a random six-character base-62 identifier.
func GenerateId() string {
	return gonanoid.MustGenerate(idCharset, 6)
}

//go:embed schema.sql
var schemaSQL string

// SqliteOption configures a SQLite-backed message store.
type SqliteOption func(*Sqlite)

// Sqlite is the SQLite-backed MessageDB implementation.
//
// It stores messages as a parent-linked tree and reconstructs gai.Message
// values (including metadata keys in ExtraFields) on reads.
type Sqlite struct {
	db          DB
	q           *sqlcgen.Queries
	idGenerator func() string
	ownedDB     *sql.DB
}

// NewSqlite initializes a SQLite-backed message store.
//
// It enables foreign-key enforcement and initializes the current schema inside
// one serialized write transaction.
//
// The caller owns the lifecycle of db (open/close).
func NewSqlite(ctx context.Context, db DB, opts ...SqliteOption) (*Sqlite, error) {
	if err := enableForeignKeys(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}
	if err := initializeSchema(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	store := &Sqlite{
		db:          db,
		q:           sqlcgen.New(db),
		idGenerator: GenerateId,
	}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

// initializeSchema serializes first-run schema creation across processes.
func initializeSchema(ctx context.Context, db DB) error {
	tx, err := beginWriteTx(ctx, db)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply current schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema transaction: %w", err)
	}
	return nil
}

// Close closes the database opened by [NewConvoDB]. It is a no-op
// for stores initialized with [NewSqlite], where the caller owns the database.
func (s *Sqlite) Close() error {
	if s.ownedDB == nil {
		return nil
	}
	return s.ownedDB.Close()
}

// beginWriteTx acquires the write reservation before any reads in a write
// transaction. The ncruces driver maps serializable transactions to IMMEDIATE.
func beginWriteTx(ctx context.Context, db DB) (*sql.Tx, error) {
	return db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
}

func enableForeignKeys(ctx context.Context, db DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("failed to set PRAGMA foreign_keys = ON: %w", err)
	}

	var enabled int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&enabled); err != nil {
		return fmt.Errorf("failed to verify PRAGMA foreign_keys: %w", err)
	}
	if enabled != 1 {
		return fmt.Errorf("PRAGMA foreign_keys is OFF")
	}
	return nil
}
