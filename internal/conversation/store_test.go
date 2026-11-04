package conversation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/models"
)

func openStore(t *testing.T, path string) *SQLiteStore {
	t.Helper()
	store, err := NewStore(
		t.Context(),
		path,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func testRecord(id ID) Record {
	return Record{
		Compaction: &Compaction{
			Summary:       "Greeting received.",
			AfterMessages: 1,
		},
		Info: Info{
			ID:       id,
			ParentID: new(ID("parent")),
			Cwd:      "/work",
			Title:    "first",
			UpdatedAt: time.Date(
				2026,
				11,
				3,
				12,
				0,
				0,
				123456000,
				time.UTC,
			),
		},
		Options: models.Selection{
			Model:     "one",
			Reasoning: "high",
		},
		Usage: TokenUsage{
			InputTokens: new(uint64(10)),
		},
		Dialog: gai.Dialog{
			{
				Role: gai.User,
				Blocks: []gai.Block{
					gai.TextBlock("hello"),
					gai.ImageBlock(
						[]byte("binary"),
						"image/png",
					),
				},
				ExtraFields: map[string]any{
					"signature": "keep me",
				},
			},
		},
	}
}

func textMessage(role gai.Role, text string) gai.Message {
	return gai.Message{
		Role: role,
		Blocks: []gai.Block{
			gai.TextBlock(text),
		},
	}
}

func dialogText(record Record) []string {
	var texts []string
	for _, message := range record.Dialog {
		texts = append(
			texts,
			message.Blocks[0].Content.String(),
		)
	}
	return texts
}

// recordWrites logs the positions of message rows that saves insert or delete.
func recordWrites(t *testing.T, store *SQLiteStore) {
	t.Helper()
	if _, err := store.db.ExecContext(
		t.Context(),
		`CREATE TABLE writes (operation TEXT, position INTEGER);
		CREATE TRIGGER log_insert AFTER INSERT ON messages BEGIN INSERT INTO writes VALUES ('insert', new.position); END;
		CREATE TRIGGER log_delete AFTER DELETE ON messages BEGIN INSERT INTO writes VALUES ('delete', old.position); END;`,
	); err != nil {
		t.Fatal(err)
	}
}

func writtenPositions(t *testing.T, store *SQLiteStore, operation string) []int {
	t.Helper()
	rows, err := store.db.QueryContext(
		t.Context(),
		"SELECT position FROM writes WHERE operation = ? ORDER BY position",
		operation,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var positions []int
	for rows.Next() {
		var position int
		if err := rows.Scan(&position); err != nil {
			t.Fatal(err)
		}
		positions = append(
			positions,
			position,
		)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return positions
}

func TestStoreRoundTrip(t *testing.T) {
	store := openStore(
		t,
		filepath.Join(
			t.TempDir(),
			"sessions.db",
		),
	)
	record := testRecord("a")
	if err := store.Save(
		t.Context(),
		record,
	); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(
		t.Context(),
		"a",
	)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Options != record.Options || loaded.Info.Cwd != "/work" || loaded.Info.Title != "first" || *loaded.Info.ParentID != "parent" || !loaded.Info.UpdatedAt.Equal(record.Info.UpdatedAt) || *loaded.Usage.InputTokens != 10 || loaded.Usage.OutputTokens != nil {
		t.Fatalf(
			"round trip: %#v",
			loaded,
		)
	}
	if loaded.Dialog[0].Blocks[0].Content.String() != "hello" || loaded.Dialog[0].Blocks[1].Content.String() != record.Dialog[0].Blocks[1].Content.String() || loaded.Dialog[0].Blocks[1].MimeType != "image/png" || loaded.Dialog[0].ExtraFields["signature"] != "keep me" {
		t.Fatalf(
			"round trip dialog: %#v",
			loaded.Dialog,
		)
	}
	if loaded.Compaction == nil || *loaded.Compaction != *record.Compaction {
		t.Fatal("compaction was not persisted")
	}
	cloned, err := loaded.Clone()
	if err != nil {
		t.Fatal(err)
	}
	cloned.Dialog[0].Blocks[0] = gai.TextBlock("changed")
	cloned.Dialog[0].ExtraFields["signature"] = "changed"
	cloned.Compaction.Summary = "changed"
	*cloned.Info.ParentID = "changed"
	if loaded.Dialog[0].Blocks[0].Content.String() != "hello" || loaded.Dialog[0].ExtraFields["signature"] != "keep me" || loaded.Compaction.Summary != "Greeting received." || *loaded.Info.ParentID != "parent" {
		t.Fatal("clone borrowed record data")
	}
	again, err := store.Load(
		t.Context(),
		"a",
	)
	if err != nil || again.Dialog[0].Blocks[0].Content.String() != "hello" {
		t.Fatalf(
			"reload: %v",
			err,
		)
	}
	empty := Record{
		Info: Info{
			ID: "empty",
		},
	}
	if err = store.Save(
		t.Context(),
		empty,
	); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(
		t.Context(),
		"empty",
	)
	if err != nil || loaded.Dialog != nil || loaded.Compaction != nil || loaded.Info.ParentID != nil || !loaded.Info.UpdatedAt.IsZero() {
		t.Fatalf(
			"empty record: %#v %v",
			loaded,
			err,
		)
	}
}

func TestStoreSavesChangedSuffix(t *testing.T) {
	for _, test := range []struct {
		name string
		// edit changes the saved three-message dialog.
		edit func(gai.Dialog) gai.Dialog
		want []string
		// deleted and inserted are the message positions the second save rewrites.
		deleted  []int
		inserted []int
	}{
		{
			name: "unchanged",
			edit: func(dialog gai.Dialog) gai.Dialog {
				return dialog
			},
			want: []string{
				"one",
				"two",
				"three",
			},
		},
		{
			name: "appended",
			edit: func(dialog gai.Dialog) gai.Dialog {
				return append(
					dialog,
					textMessage(
						gai.User,
						"four",
					),
				)
			},
			want: []string{
				"one",
				"two",
				"three",
				"four",
			},
			inserted: []int{
				3,
			},
		},
		{
			name: "middle replaced",
			edit: func(dialog gai.Dialog) gai.Dialog {
				dialog[1] = textMessage(
					gai.Assistant,
					"changed",
				)
				return dialog
			},
			want: []string{
				"one",
				"changed",
				"three",
			},
			deleted: []int{
				1,
				2,
			},
			inserted: []int{
				1,
				2,
			},
		},
		{
			name: "truncated",
			edit: func(dialog gai.Dialog) gai.Dialog {
				return dialog[:1]
			},
			want: []string{
				"one",
			},
			deleted: []int{
				1,
				2,
			},
		},
		{
			name: "emptied",
			edit: func(gai.Dialog) gai.Dialog {
				return nil
			},
			want: nil,
			deleted: []int{
				0,
				1,
				2,
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				store := openStore(
					t,
					filepath.Join(
						t.TempDir(),
						"sessions.db",
					),
				)
				record := Record{
					Info: Info{
						ID: "a",
					},
					Dialog: gai.Dialog{
						textMessage(
							gai.User,
							"one",
						),
						textMessage(
							gai.Assistant,
							"two",
						),
						textMessage(
							gai.User,
							"three",
						),
					},
				}
				if err := store.Save(
					t.Context(),
					record,
				); err != nil {
					t.Fatal(err)
				}
				recordWrites(
					t,
					store,
				)
				record.Dialog = test.edit(record.Dialog)
				if err := store.Save(
					t.Context(),
					record,
				); err != nil {
					t.Fatal(err)
				}
				loaded, err := store.Load(
					t.Context(),
					"a",
				)
				if err != nil {
					t.Fatal(err)
				}
				if got := dialogText(loaded); fmt.Sprint(got) != fmt.Sprint(test.want) {
					t.Fatalf(
						"dialog: %q",
						got,
					)
				}
				for operation, want := range map[string][]int{
					"delete": test.deleted,
					"insert": test.inserted,
				} {
					if got := writtenPositions(
						t,
						store,
						operation,
					); fmt.Sprint(got) != fmt.Sprint(want) {
						t.Fatalf(
							"%s positions: %v",
							operation,
							got,
						)
					}
				}
			},
		)
	}
}

func TestStoreRejectsInvalidSaves(t *testing.T) {
	store := openStore(
		t,
		filepath.Join(
			t.TempDir(),
			"sessions.db",
		),
	)
	record := testRecord("a")
	if err := store.Save(
		t.Context(),
		record,
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	replacement := testRecord("a")
	replacement.Info.Title = "replacement"
	replacement.Dialog = append(
		replacement.Dialog,
		textMessage(
			gai.Assistant,
			"more",
		),
	)
	if err := store.Save(
		ctx,
		replacement,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf(
			"cancelled save: %v",
			err,
		)
	}
	replacement.Compaction.AfterMessages = 5
	if err := store.Save(
		t.Context(),
		replacement,
	); err == nil {
		t.Fatal("accepted an invalid compaction boundary")
	}
	if err := store.Save(
		t.Context(),
		Record{},
	); err == nil {
		t.Fatal("accepted an empty session ID")
	}
	loaded, err := store.Load(
		t.Context(),
		"a",
	)
	if err != nil || loaded.Info.Title != "first" || len(loaded.Dialog) != 1 {
		t.Fatalf(
			"rejected save changed the session: %#v %v",
			loaded,
			err,
		)
	}
}

func TestStoreList(t *testing.T) {
	store := openStore(
		t,
		filepath.Join(
			t.TempDir(),
			"sessions.db",
		),
	)
	base := time.Date(
		2026,
		11,
		3,
		0,
		0,
		0,
		0,
		time.UTC,
	)
	for _, session := range []struct {
		id      ID
		cwd     string
		updated time.Duration
	}{
		{
			id:      "old",
			cwd:     "/work",
			updated: time.Minute,
		},
		{
			id:      "newest",
			cwd:     "/work",
			updated: 3 * time.Minute,
		},
		{
			id:      "elsewhere",
			cwd:     "/other",
			updated: 2 * time.Minute,
		},
		{
			id:      "tied",
			cwd:     "/work",
			updated: time.Minute,
		},
	} {
		if err := store.Save(
			t.Context(),
			Record{
				Info: Info{
					ID:        session.id,
					Cwd:       session.cwd,
					UpdatedAt: base.Add(session.updated),
				},
			},
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name  string
		cwd   string
		want  []string
		limit uint
	}{
		{
			name:  "all sessions",
			limit: 2,
			want: []string{
				"newest",
				"elsewhere",
				"tied",
				"old",
			},
		},
		{
			name:  "one directory",
			cwd:   "/work",
			limit: 1,
			want: []string{
				"newest",
				"tied",
				"old",
			},
		},
		{
			name:  "single page",
			cwd:   "/other",
			limit: 5,
			want: []string{
				"elsewhere",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var got []string
				cursor := ""
				for pages := 0; ; pages++ {
					if pages > len(test.want) {
						t.Fatal("pagination did not finish")
					}
					page, err := store.List(
						t.Context(),
						Query{
							Cwd:    test.cwd,
							Cursor: cursor,
							Limit:  test.limit,
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					if uint(len(page.Sessions)) > test.limit {
						t.Fatalf(
							"page has %d sessions",
							len(page.Sessions),
						)
					}
					for _, info := range page.Sessions {
						got = append(
							got,
							string(info.ID),
						)
					}
					if page.NextCursor == "" {
						break
					}
					cursor = page.NextCursor
				}
				if fmt.Sprint(got) != fmt.Sprint(test.want) {
					t.Fatalf(
						"sessions: %q",
						got,
					)
				}
			},
		)
	}
	for _, query := range []Query{
		{
			Limit: 0,
		},
		{
			Limit:  1,
			Cursor: "not-a-cursor",
		},
		{
			Limit:  1,
			Cursor: "12:",
		},
	} {
		if _, err := store.List(
			t.Context(),
			query,
		); err == nil {
			t.Fatalf(
				"accepted query %#v",
				query,
			)
		}
	}
}

func TestStoreDelete(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"sessions.db",
	)
	store := openStore(
		t,
		path,
	)
	record := testRecord("a")
	record.Dialog = append(
		record.Dialog,
		textMessage(
			gai.Assistant,
			"secret-marker-7f3c",
		),
	)
	if err := store.Save(
		t.Context(),
		record,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(
		t.Context(),
		"a",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(
		t.Context(),
		"a",
	); !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		t.Fatalf(
			"load deleted: %v",
			err,
		)
	}
	if err := store.Delete(
		t.Context(),
		"a",
	); !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		t.Fatalf(
			"delete missing: %v",
			err,
		)
	}
	var messages int
	if err := store.db.QueryRowContext(
		t.Context(),
		"SELECT count(*) FROM messages",
	).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf(
			"messages left after delete: %d %v",
			messages,
			err,
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(
		data,
		[]byte("secret-marker-7f3c"),
	) {
		t.Fatal("deleted transcript remains in the database file")
	}
}

func TestStoreRejectsCorruptMessages(t *testing.T) {
	for _, test := range []struct {
		name   string
		update string
	}{
		{
			name:   "undecodable message",
			update: "UPDATE messages SET data = '{broken' WHERE position = 0",
		},
		{
			name:   "missing message",
			update: "UPDATE messages SET position = 4 WHERE position = 0",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				store := openStore(
					t,
					filepath.Join(
						t.TempDir(),
						"sessions.db",
					),
				)
				if err := store.Save(
					t.Context(),
					testRecord("a"),
				); err != nil {
					t.Fatal(err)
				}
				if _, err := store.db.ExecContext(
					t.Context(),
					test.update,
				); err != nil {
					t.Fatal(err)
				}
				if _, err := store.Load(
					t.Context(),
					"a",
				); err == nil {
					t.Fatal("accepted a corrupt transcript")
				}
			},
		)
	}
}

func TestStoreReopensAndShares(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"sessions.db",
	)
	first := openStore(
		t,
		path,
	)
	// A second handle stands in for another CPE process using the same file.
	second := openStore(
		t,
		path,
	)
	var wait sync.WaitGroup
	errs := make(
		chan error,
		20,
	)
	for i := range 20 {
		store := first
		if i%2 == 1 {
			store = second
		}
		wait.Go(func() {
			record := testRecord(ID(fmt.Sprintf(
				"s%d",
				i,
			)))
			for turn := range 3 {
				record.Dialog = append(
					record.Dialog,
					textMessage(
						gai.Assistant,
						fmt.Sprint(turn),
					),
				)
				if err := store.Save(
					t.Context(),
					record,
				); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(
		t,
		path,
	)
	page, err := reopened.List(
		t.Context(),
		Query{
			Limit: 100,
		},
	)
	if err != nil || len(page.Sessions) != 20 {
		t.Fatalf(
			"reopened list: %d %v",
			len(page.Sessions),
			err,
		)
	}
	loaded, err := reopened.Load(
		t.Context(),
		"s7",
	)
	if err != nil || len(loaded.Dialog) != 4 {
		t.Fatalf(
			"reopened load: %v",
			err,
		)
	}
	if _, err := reopened.db.ExecContext(
		t.Context(),
		"PRAGMA user_version = 99",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(
		t.Context(),
		path,
	); err == nil {
		t.Fatal("opened an unsupported schema version")
	}
	if _, err := NewStore(
		t.Context(),
		"relative.db",
	); err == nil {
		t.Fatal("accepted a relative path")
	}
}

func TestStoreConnectionSettings(t *testing.T) {
	store := openStore(
		t,
		filepath.Join(
			t.TempDir(),
			"sessions.db",
		),
	)
	for _, test := range []struct {
		pragma string
		want   string
	}{
		{
			pragma: "journal_mode",
			want:   "delete",
		},
		{
			// EXTRA syncs the directory after the journal is deleted at commit.
			pragma: "synchronous",
			want:   "3",
		},
		{
			pragma: "foreign_keys",
			want:   "1",
		},
		{
			pragma: "secure_delete",
			want:   "1",
		},
	} {
		var got string
		if err := store.db.QueryRowContext(
			t.Context(),
			"PRAGMA "+test.pragma,
		).Scan(&got); err != nil || got != test.want {
			t.Fatalf(
				"%s: %q %v",
				test.pragma,
				got,
				err,
			)
		}
	}
}

func TestStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows protects the database with ACLs")
	}
	path := filepath.Join(
		t.TempDir(),
		"nested",
		"sessions.db",
	)
	if err := os.MkdirAll(
		filepath.Dir(path),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		path,
		nil,
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(
		path,
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	store := openStore(
		t,
		path,
	)
	if err := store.Save(
		t.Context(),
		testRecord("a"),
	); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(
		t.Context(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(
		t.Context(),
		"UPDATE sessions SET title = 'changed'",
	); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{
		path,
		path + "-journal",
	} {
		info, statErr := os.Stat(file)
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf(
				"%s permissions: %v %v",
				file,
				info,
				statErr,
			)
		}
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil || directory.Mode().Perm() != 0o700 {
		t.Fatalf(
			"directory permissions: %v %v",
			directory,
			err,
		)
	}
}

func queryPlan(t *testing.T, store *SQLiteStore, statement string, args []any) []string {
	t.Helper()
	rows, err := store.db.QueryContext(
		t.Context(),
		"EXPLAIN QUERY PLAN "+statement,
		args...,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(
			&id,
			&parent,
			&unused,
			&detail,
		); err != nil {
			t.Fatal(err)
		}
		plan = append(
			plan,
			detail,
		)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestStoreListSeeksIndexes(t *testing.T) {
	store := openStore(
		t,
		filepath.Join(
			t.TempDir(),
			"sessions.db",
		),
	)
	for _, query := range []Query{
		{
			Cwd: "/work",
		},
		{
			Cursor: "12:a",
		},
		{
			Cwd:    "/work",
			Cursor: "12:a",
		},
	} {
		statement, args, err := listStatement(
			query,
			2,
		)
		if err != nil {
			t.Fatal(err)
		}
		plan := queryPlan(
			t,
			store,
			statement,
			args,
		)
		if text := strings.Join(
			plan,
			"; ",
		); !strings.HasPrefix(
			text,
			"SEARCH sessions USING INDEX",
		) || strings.Contains(
			text,
			"TEMP B-TREE",
		) {
			t.Fatalf(
				"query %#v plan: %s",
				query,
				text,
			)
		}
	}
}
