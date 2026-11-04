package conversation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

//go:embed schema.sql
var schema string

// schemaVersion is stored in PRAGMA user_version. CPE opens no other version.
const schemaVersion = 1

const pragmas = "PRAGMA foreign_keys = ON; PRAGMA journal_mode = DELETE; PRAGMA synchronous = EXTRA; PRAGMA secure_delete = ON;"

// SQLiteStore keeps saved sessions in one SQLite database. A save rewrites only
// the transcript suffix that differs from the stored one. It is safe for
// concurrent sessions, including sessions in other processes sharing the file.
type SQLiteStore struct {
	db *sql.DB
}

var _ Store = (*SQLiteStore)(nil)

// NewStore opens or creates the session database at path with private
// permissions. The caller closes the store.
func NewStore(ctx context.Context, path string) (*SQLiteStore, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("session database path must be absolute")
	}
	if err := os.MkdirAll(
		filepath.Dir(path),
		0o700,
	); err != nil {
		return nil, err
	}
	// The journal copies this file's mode through the modeof URI parameter, so
	// an existing database is made private too.
	file, err := os.OpenFile(
		path,
		os.O_RDWR|os.O_CREATE,
		0o600,
	)
	if err != nil {
		return nil, err
	}
	if err = errors.Join(
		file.Chmod(0o600),
		file.Close(),
	); err != nil {
		return nil, err
	}
	query := url.Values{}
	// Writers take the lock when they begin, so a busy database waits instead
	// of failing a read-to-write lock upgrade.
	query.Set(
		"_txlock",
		"immediate",
	)
	query.Set(
		"modeof",
		path,
	)
	location := filepath.ToSlash(path)
	if !strings.HasPrefix(
		location,
		"/",
	) {
		location = "/" + location
	}
	db, err := driver.Open(
		(&url.URL{
			Scheme:   "file",
			Path:     location,
			RawQuery: query.Encode(),
		}).String(),
		func(conn *sqlite3.Conn) error {
			// A rollback journal honors modeof. This VFS creates WAL shared memory
			// with default permissions, so WAL mode is not used. EXTRA also syncs
			// the directory after deleting the journal, which makes each commit
			// durable. Secure deletion zeroes replaced transcript suffixes and
			// deleted sessions.
			return conn.Exec(pragmas)
		},
	)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{
		db: db,
	}
	if err := store.initialize(ctx); err != nil {
		return nil, errors.Join(
			err,
			db.Close(),
		)
	}
	return store, nil
}

func (s *SQLiteStore) initialize(ctx context.Context) error {
	tx, err := s.db.BeginTx(
		ctx,
		nil,
	)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(
		ctx,
		"PRAGMA user_version",
	).Scan(&version); err != nil {
		return err
	}
	switch version {
	case schemaVersion:
		return tx.Commit()
	case 0:
	default:
		return fmt.Errorf(
			"session database has unsupported schema version %d",
			version,
		)
	}
	if _, err := tx.ExecContext(
		ctx,
		schema,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		fmt.Sprintf(
			"PRAGMA user_version = %d",
			schemaVersion,
		),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// Close releases the database. Callers must not use the store afterwards.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

type storedRow struct {
	digest []byte
	data   []byte
}

func (s *SQLiteStore) Save(ctx context.Context, record Record) error {
	if record.Info.ID == "" {
		return errors.New("session ID is required")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	options, err := json.Marshal(record.Options)
	if err != nil {
		return err
	}
	usage, err := json.Marshal(record.Usage)
	if err != nil {
		return err
	}
	var compaction, parent any
	if record.Compaction != nil {
		var data []byte
		data, err = json.Marshal(record.Compaction)
		if err != nil {
			return err
		}
		compaction = string(data)
	}
	if record.Info.ParentID != nil {
		parent = string(*record.Info.ParentID)
	}
	rows := make(
		[]storedRow,
		len(record.Dialog),
	)
	for i, message := range record.Dialog {
		var data []byte
		data, err = encodeMessage(message)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		rows[i] = storedRow{
			digest: digest[:],
			data:   data,
		}
	}
	tx, err := s.db.BeginTx(
		ctx,
		nil,
	)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(
		ctx,
		`INSERT INTO sessions (id, parent_id, cwd, title, updated_at, options, usage, compaction)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			parent_id = excluded.parent_id,
			cwd = excluded.cwd,
			title = excluded.title,
			updated_at = excluded.updated_at,
			options = excluded.options,
			usage = excluded.usage,
			compaction = excluded.compaction`,
		string(record.Info.ID),
		parent,
		record.Info.Cwd,
		record.Info.Title,
		record.Info.UpdatedAt.UnixMicro(),
		string(options),
		string(usage),
		compaction,
	); err != nil {
		return err
	}
	kept, err := matchingPrefix(
		ctx,
		tx,
		record.Info.ID,
		rows,
	)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM messages WHERE session_id = ? AND position >= ?",
		string(record.Info.ID),
		kept,
	); err != nil {
		return err
	}
	if kept < len(rows) {
		insert, err := tx.PrepareContext(
			ctx,
			"INSERT INTO messages (session_id, position, digest, data) VALUES (?, ?, ?, ?)",
		)
		if err != nil {
			return err
		}
		defer insert.Close()
		for position := kept; position < len(rows); position++ {
			if _, err := insert.ExecContext(
				ctx,
				string(record.Info.ID),
				position,
				rows[position].digest,
				string(rows[position].data),
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// matchingPrefix counts leading stored messages identical to rows.
func matchingPrefix(ctx context.Context, tx *sql.Tx, id ID, rows []storedRow) (int, error) {
	stored, err := tx.QueryContext(
		ctx,
		"SELECT position, digest FROM messages WHERE session_id = ? ORDER BY position",
		string(id),
	)
	if err != nil {
		return 0, err
	}
	defer stored.Close()
	kept := 0
	for kept < len(rows) && stored.Next() {
		var position int
		var digest []byte
		if err := stored.Scan(
			&position,
			&digest,
		); err != nil {
			return 0, err
		}
		if position != kept || !bytes.Equal(
			digest,
			rows[kept].digest,
		) {
			break
		}
		kept++
	}
	if err := stored.Err(); err != nil {
		return 0, err
	}
	return kept, stored.Close()
}

func (s *SQLiteStore) Load(ctx context.Context, id ID) (Record, error) {
	tx, err := s.db.BeginTx(
		ctx,
		&sql.TxOptions{
			ReadOnly: true,
		},
	)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	record := Record{
		Info: Info{
			ID: id,
		},
	}
	var parent, compaction sql.NullString
	var updated int64
	var options, usage string
	err = tx.QueryRowContext(
		ctx,
		"SELECT parent_id, cwd, title, updated_at, options, usage, compaction FROM sessions WHERE id = ?",
		string(id),
	).Scan(
		&parent,
		&record.Info.Cwd,
		&record.Info.Title,
		&updated,
		&options,
		&usage,
		&compaction,
	)
	if errors.Is(
		err,
		sql.ErrNoRows,
	) {
		return Record{}, fmt.Errorf(
			"session %q: %w",
			id,
			fs.ErrNotExist,
		)
	}
	if err != nil {
		return Record{}, err
	}
	record.Info.UpdatedAt = time.UnixMicro(updated).UTC()
	if parent.Valid {
		record.Info.ParentID = new(ID(parent.String))
	}
	if err = json.Unmarshal(
		[]byte(options),
		&record.Options,
	); err != nil {
		return Record{}, err
	}
	if err = json.Unmarshal(
		[]byte(usage),
		&record.Usage,
	); err != nil {
		return Record{}, err
	}
	if compaction.Valid {
		record.Compaction = &Compaction{}
		if err = json.Unmarshal(
			[]byte(compaction.String),
			record.Compaction,
		); err != nil {
			return Record{}, err
		}
	}
	rows, err := tx.QueryContext(
		ctx,
		"SELECT position, data FROM messages WHERE session_id = ? ORDER BY position",
		string(id),
	)
	if err != nil {
		return Record{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var position int
		var data string
		if err := rows.Scan(
			&position,
			&data,
		); err != nil {
			return Record{}, err
		}
		if position != len(record.Dialog) {
			return Record{}, fmt.Errorf(
				"session %q is missing message %d",
				id,
				len(record.Dialog),
			)
		}
		message, err := decodeMessage([]byte(data))
		if err != nil {
			return Record{}, fmt.Errorf(
				"session %q message %d: %w",
				id,
				position,
				err,
			)
		}
		record.Dialog = append(
			record.Dialog,
			message,
		)
	}
	if err := rows.Err(); err != nil {
		return Record{}, err
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

// List returns sessions by descending update time. Sessions updated while a
// caller pages through results may move to a page it has already read.
func (s *SQLiteStore) List(ctx context.Context, query Query) (Page, error) {
	if query.Limit == 0 {
		return Page{}, errors.New("page limit must be positive")
	}
	// One extra row reports whether another page exists.
	limit := int64(min(
		uint64(query.Limit),
		math.MaxInt64-1,
	)) + 1
	statement, args, err := listStatement(
		query,
		limit,
	)
	if err != nil {
		return Page{}, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		statement,
		args...,
	)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	var page Page
	for rows.Next() {
		var info Info
		var parent sql.NullString
		var updated int64
		if err := rows.Scan(
			&info.ID,
			&parent,
			&info.Cwd,
			&info.Title,
			&updated,
		); err != nil {
			return Page{}, err
		}
		info.UpdatedAt = time.UnixMicro(updated).UTC()
		if parent.Valid {
			info.ParentID = new(ID(parent.String))
		}
		page.Sessions = append(
			page.Sessions,
			info,
		)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if uint64(len(page.Sessions)) > uint64(query.Limit) {
		page.Sessions = page.Sessions[:query.Limit]
		last := page.Sessions[len(page.Sessions)-1]
		page.NextCursor = strconv.FormatInt(
			last.UpdatedAt.UnixMicro(),
			10,
		) + ":" + string(last.ID)
	}
	return page, nil
}

// listStatement uses one plain predicate per filter so the indexes can seek to
// the directory and cursor rather than scanning from the newest session.
func listStatement(query Query, limit int64) (string, []any, error) {
	var conditions []string
	var args []any
	if query.Cwd != "" {
		conditions = append(
			conditions,
			"cwd = ?",
		)
		args = append(
			args,
			query.Cwd,
		)
	}
	if query.Cursor != "" {
		updated, id, err := parseCursor(query.Cursor)
		if err != nil {
			return "", nil, err
		}
		conditions = append(
			conditions,
			"(updated_at, id) < (?, ?)",
		)
		args = append(
			args,
			updated,
			id,
		)
	}
	statement := "SELECT id, parent_id, cwd, title, updated_at FROM sessions"
	if len(conditions) != 0 {
		statement += " WHERE " + strings.Join(
			conditions,
			" AND ",
		)
	}
	return statement + " ORDER BY updated_at DESC, id DESC LIMIT ?", append(
		args,
		limit,
	), nil
}

func parseCursor(cursor string) (int64, string, error) {
	updated, id, found := strings.Cut(
		cursor,
		":",
	)
	value, err := strconv.ParseInt(
		updated,
		10,
		64,
	)
	if !found || id == "" || err != nil {
		return 0, "", errors.New("invalid session list cursor")
	}
	return value, id, nil
}

func (s *SQLiteStore) Delete(ctx context.Context, id ID) error {
	result, err := s.db.ExecContext(
		ctx,
		"DELETE FROM sessions WHERE id = ?",
		string(id),
	)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return fmt.Errorf(
			"session %q: %w",
			id,
			fs.ErrNotExist,
		)
	}
	return nil
}
