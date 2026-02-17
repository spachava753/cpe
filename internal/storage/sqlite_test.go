package storage

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"path/filepath"
	"slices"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/gai"
)

// --- Test helpers ---

// newTestDB returns a Sqlite backed by a temp SQLite file and the raw *sql.DB
// for direct manipulation. Both are cleaned up when the test finishes.
func newTestDB(t *testing.T) (*Sqlite, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ds, err := NewSqlite(context.Background(), db)
	if err != nil {
		t.Fatalf("NewSqlite: %v", err)
	}
	return ds, db
}

// newTestDBWithFK creates a test DB with foreign keys enabled (required for
// deferred-FK poison-trigger tests).
func newTestDBWithFK(t *testing.T) (*Sqlite, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	rawDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { rawDB.Close() })

	ds, err := NewSqlite(context.Background(), rawDB)
	if err != nil {
		t.Fatalf("NewSqlite: %v", err)
	}
	return ds, rawDB
}

// failDB wraps a real DB and allows injecting errors at specific points.
type failDB struct {
	DB
	failExec    bool
	failBeginTx bool
}

func (f *failDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if f.failExec {
		return nil, fmt.Errorf("injected ExecContext error")
	}
	return f.DB.ExecContext(ctx, query, args...)
}

func (f *failDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if f.failBeginTx {
		return nil, fmt.Errorf("injected BeginTx error")
	}
	return f.DB.BeginTx(ctx, opts)
}

// addBlockingTrigger creates a SQLite trigger that blocks INSERT or DELETE
// operations on the given table using RAISE(ABORT, ...).
func addBlockingTrigger(t *testing.T, rawDB *sql.DB, table, operation string) {
	t.Helper()
	name := fmt.Sprintf("block_%s_%s", operation, table)
	sql := fmt.Sprintf(
		`CREATE TRIGGER %s BEFORE %s ON %s
		 BEGIN SELECT RAISE(ABORT, '%s blocked by trigger'); END`,
		name, operation, table, operation)
	if _, err := rawDB.ExecContext(context.Background(), sql); err != nil {
		t.Fatalf("create trigger %s: %v", name, err)
	}
}

// addPoisonTrigger creates a deferred FK constraint that causes tx.Commit()
// to fail. The trigger fires on the given operation (INSERT or DELETE) on messages.
func addPoisonTrigger(t *testing.T, rawDB *sql.DB, operation string) {
	t.Helper()
	_, err := rawDB.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS ref_parent (id TEXT PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS poison (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ref_id TEXT REFERENCES ref_parent(id) DEFERRABLE INITIALLY DEFERRED
		);
		CREATE TRIGGER poison_on_msg_%s AFTER %s ON messages
		BEGIN
		  INSERT INTO poison (ref_id) VALUES ('nonexistent_ref');
		END;
	`, operation, operation))
	if err != nil {
		t.Fatalf("create poison trigger for %s: %v", operation, err)
	}
}

func makeTextMessage(role gai.Role, text string) gai.Message {
	return gai.Message{
		Role: role,
		Blocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(text),
			},
		},
	}
}

// saveDialog is a test helper that saves a dialog and returns the saved messages.
func saveDialog(t *testing.T, db *Sqlite, ctx context.Context, msgs []gai.Message) []gai.Message {
	t.Helper()
	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values(msgs)) {
		if err != nil {
			t.Fatalf("SaveDialog: %v", err)
		}
		saved = append(saved, msg)
	}
	if len(saved) != len(msgs) {
		t.Fatalf("expected %d saved messages, got %d", len(msgs), len(saved))
	}
	return saved
}

// saveOne is a test helper that saves a single message as a root dialog and returns its ID.
func saveOne(t *testing.T, db *Sqlite, ctx context.Context, msg gai.Message) string {
	t.Helper()
	saved := saveDialog(t, db, ctx, []gai.Message{msg})
	id := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	return id
}

// expectSaveDialogError runs SaveDialog and expects it to return an error.
func expectSaveDialogError(t *testing.T, db *Sqlite, ctx context.Context, msgs []gai.Message) {
	t.Helper()
	var gotErr error
	for _, err := range db.SaveDialog(ctx, slices.Values(msgs)) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected SaveDialog error, got nil")
	}
}

// TestSaveDialog verifies the SaveDialog iterator's core contract: assigning IDs,
// building parent chains, handling pre-existing messages, and maintaining transaction
// atomicity (commit on success/early-break, rollback on error).
func TestSaveDialog(t *testing.T) {
	t.Run("single root message", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "hello"),
		})

		gotID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		if gotID == "" {
			t.Fatal("expected non-empty ID")
		}
		if parentID := getExtraFieldString(saved[0].ExtraFields, MessageParentIDKey); parentID != "" {
			t.Errorf("root message should not have parent ID, got %q", parentID)
		}
	})

	t.Run("dialog chain sets parent IDs correctly", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
			makeTextMessage(gai.User, "msg3"),
		}
		saved := saveDialog(t, db, ctx, msgs)

		ids := make([]string, 3)
		for i, s := range saved {
			ids[i] = getExtraFieldString(s.ExtraFields, MessageIDKey)
			if ids[i] == "" {
				t.Fatalf("saved[%d]: expected non-empty ID", i)
			}
		}
		if ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] {
			t.Fatalf("IDs should be unique: %v", ids)
		}

		if p := getExtraFieldString(saved[0].ExtraFields, MessageParentIDKey); p != "" {
			t.Errorf("first message should have no parent, got %q", p)
		}
		if p := getExtraFieldString(saved[1].ExtraFields, MessageParentIDKey); p != ids[0] {
			t.Errorf("second message parent: expected %q, got %q", ids[0], p)
		}
		if p := getExtraFieldString(saved[2].ExtraFields, MessageParentIDKey); p != ids[1] {
			t.Errorf("third message parent: expected %q, got %q", ids[1], p)
		}
	})

	t.Run("message with blocks persists and retrieves", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msg := gai.Message{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("text content"),
				},
				{
					ID:           "tool-call-1",
					BlockType:    gai.ToolCall,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(`{"name":"test","parameters":{}}`),
					ExtraFields:  map[string]any{"provider": "anthropic"},
				},
			},
		}

		saved := saveDialog(t, db, ctx, []gai.Message{msg})
		savedID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)

		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		var retrieved gai.Message
		for m := range msgs {
			retrieved = m
		}
		if len(retrieved.Blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(retrieved.Blocks))
		}
	})

	t.Run("message with title round-trips", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msg := makeTextMessage(gai.User, "titled")
		msg.ExtraFields = map[string]any{MessageTitleKey: "subagent:test:run1"}

		saved := saveDialog(t, db, ctx, []gai.Message{msg})
		savedID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		if savedID == "" {
			t.Fatal("expected non-empty ID")
		}

		// Verify title is retrievable
		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		for m := range msgs {
			got, ok := m.ExtraFields[MessageTitleKey].(string)
			if !ok || got != "subagent:test:run1" {
				t.Errorf("expected title %q, got %v", "subagent:test:run1", m.ExtraFields[MessageTitleKey])
			}
		}
	})

	t.Run("existing messages are verified not re-saved", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "reply"),
		})

		newMsg := makeTextMessage(gai.User, "follow-up")
		fullDialog := append(saved, newMsg)
		saved2 := saveDialog(t, db, ctx, fullDialog)

		if len(saved2) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(saved2))
		}
		for i := 0; i < 2; i++ {
			origID := getExtraFieldString(saved[i].ExtraFields, MessageIDKey)
			newID := getExtraFieldString(saved2[i].ExtraFields, MessageIDKey)
			if origID != newID {
				t.Errorf("message %d: expected same ID %q, got %q", i, origID, newID)
			}
		}

		thirdID := getExtraFieldString(saved2[2].ExtraFields, MessageIDKey)
		if thirdID == "" {
			t.Fatal("expected non-empty ID for new message")
		}
		thirdParent := getExtraFieldString(saved2[2].ExtraFields, MessageParentIDKey)
		secondID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)
		if thirdParent != secondID {
			t.Errorf("third message parent: expected %q, got %q", secondID, thirdParent)
		}
	})

	t.Run("existing message validation errors", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(t *testing.T, db *Sqlite, ctx context.Context) []gai.Message
		}{
			{
				name: "first existing message with parent",
				setup: func(t *testing.T, db *Sqlite, ctx context.Context) []gai.Message {
					saved := saveDialog(t, db, ctx, []gai.Message{
						makeTextMessage(gai.User, "root"),
						makeTextMessage(gai.Assistant, "child"),
					})
					return []gai.Message{saved[1]}
				},
			},
			{
				name: "wrong parent chain",
				setup: func(t *testing.T, db *Sqlite, ctx context.Context) []gai.Message {
					saved1 := saveDialog(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "root1")})
					saved2 := saveDialog(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "root2")})
					return []gai.Message{saved1[0], saved2[0]}
				},
			},
			{
				name: "non-existent ID",
				setup: func(t *testing.T, db *Sqlite, ctx context.Context) []gai.Message {
					msg := makeTextMessage(gai.User, "ghost")
					msg.ExtraFields = map[string]any{MessageIDKey: "nonexistent"}
					return []gai.Message{msg}
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db, _ := newTestDB(t)
				ctx := context.Background()
				msgs := tt.setup(t, db, ctx)
				expectSaveDialogError(t, db, ctx, msgs)
			})
		}
	})

	t.Run("dialog retrievable via GetDialogForMessage", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "first"),
			makeTextMessage(gai.Assistant, "second"),
			makeTextMessage(gai.User, "third"),
		})

		lastID := getExtraFieldString(saved[2].ExtraFields, MessageIDKey)
		dialog, err := GetDialogForMessage(ctx, db, lastID)
		if err != nil {
			t.Fatalf("GetDialogForMessage: %v", err)
		}
		if len(dialog) != 3 {
			t.Fatalf("expected 3 messages in dialog chain, got %d", len(dialog))
		}
	})

		// Verifies that breaking out of the SaveDialog iterator early commits the
		// transaction for messages already yielded, rather than rolling back.
	t.Run("early break commits saved messages", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
			makeTextMessage(gai.User, "msg3"),
		}

		consumed := 0
		var firstID, secondID string
		for msg, err := range db.SaveDialog(ctx, slices.Values(msgs)) {
			if err != nil {
				t.Fatalf("SaveDialog: %v", err)
			}
			id := getExtraFieldString(msg.ExtraFields, MessageIDKey)
			if consumed == 0 {
				firstID = id
			} else if consumed == 1 {
				secondID = id
			}
			consumed++
			if consumed == 2 {
				break
			}
		}
		if consumed != 2 {
			t.Fatalf("expected to consume 2, got %d", consumed)
		}

		for _, id := range []string{firstID, secondID} {
			if _, err := db.GetMessages(ctx, []string{id}); err != nil {
				t.Fatalf("message %s should be retrievable: %v", id, err)
			}
		}

		allMsgs, err := db.ListMessages(ctx, ListMessagesOptions{})
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		count := 0
		for range allMsgs {
			count++
		}
		if count != 2 {
			t.Fatalf("expected exactly 2 messages in DB, got %d", count)
		}
	})

	t.Run("empty iterator is a no-op", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		var count int
		for _, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{})) {
			if err != nil {
				t.Fatalf("SaveDialog: %v", err)
			}
			count++
		}
		if count != 0 {
			t.Fatalf("expected 0 messages yielded, got %d", count)
		}
	})

	t.Run("transaction rolls back on save error", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		db.idGenerator = func() string { return "fixed_id" }

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
		}
		expectSaveDialogError(t, db, ctx, msgs)

		if _, err := db.GetMessages(ctx, []string{"fixed_id"}); err == nil {
			t.Fatal("message should not exist after rollback")
		}
	})
}

// TestGetMessages verifies message retrieval by ID, including correct population of
// ExtraFields (MessageIDKey, MessageParentIDKey), preservation of Role and ToolResultError
// fields, and round-trip of block-level ExtraFields. Also confirms non-existent IDs
// produce errors.
func TestGetMessages(t *testing.T) {
	t.Run("retrieve by ID with correct ExtraFields", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		savedID := saveOne(t, db, ctx, makeTextMessage(gai.User, "hello"))

		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		var got gai.Message
		for m := range msgs {
			got = m
		}
		gotID, ok := got.ExtraFields[MessageIDKey].(string)
		if !ok || gotID != savedID {
			t.Fatalf("expected ExtraFields[%q] = %q, got %q", MessageIDKey, savedID, gotID)
		}
	})

	t.Run("parent ID in ExtraFields", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "parent"),
			makeTextMessage(gai.Assistant, "child"),
		})
		parentID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		childID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)

		// Root should not have MessageParentIDKey
		msgs, err := db.GetMessages(ctx, []string{parentID})
		if err != nil {
			t.Fatalf("GetMessages (parent): %v", err)
		}
		for m := range msgs {
			if _, exists := m.ExtraFields[MessageParentIDKey]; exists {
				t.Error("root message should not have MessageParentIDKey")
			}
		}

		// Child should have MessageParentIDKey
		msgs, err = db.GetMessages(ctx, []string{childID})
		if err != nil {
			t.Fatalf("GetMessages (child): %v", err)
		}
		for m := range msgs {
			gotParent, ok := m.ExtraFields[MessageParentIDKey].(string)
			if !ok || gotParent != parentID {
				t.Fatalf("expected ExtraFields[%q] = %q, got %q", MessageParentIDKey, parentID, gotParent)
			}
		}
	})

	t.Run("round-trip Role and ToolResultError", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msg := gai.Message{
			Role:            gai.ToolResult,
			ToolResultError: true,
			Blocks: []gai.Block{
				{
					ID:           "tool-1",
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("error result"),
				},
			},
		}
		savedID := saveOne(t, db, ctx, msg)

		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		for m := range msgs {
			if m.Role != gai.ToolResult {
				t.Errorf("expected role ToolResult, got %v", m.Role)
			}
			if !m.ToolResultError {
				t.Error("expected ToolResultError = true")
			}
		}
	})

	t.Run("non-existent ID returns error", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		_, err := db.GetMessages(ctx, []string{"nonexistent"})
		if err == nil {
			t.Fatal("expected error for non-existent ID")
		}
	})

	t.Run("block ExtraFields round-trip", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msg := gai.Message{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					ID:           "block-1",
					BlockType:    gai.ToolCall,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(`{"name":"test"}`),
					ExtraFields:  map[string]any{"key1": "value1", "key2": float64(42)},
				},
			},
		}
		savedID := saveOne(t, db, ctx, msg)

		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		for m := range msgs {
			if len(m.Blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(m.Blocks))
			}
			block := m.Blocks[0]
			if block.ExtraFields == nil {
				t.Fatal("expected block ExtraFields to be non-nil")
			}
			if v, ok := block.ExtraFields["key1"].(string); !ok || v != "value1" {
				t.Errorf("expected block ExtraFields[key1] = value1, got %v", block.ExtraFields["key1"])
			}
			if v, ok := block.ExtraFields["key2"].(float64); !ok || v != 42 {
				t.Errorf("expected block ExtraFields[key2] = 42, got %v", block.ExtraFields["key2"])
			}
		}
	})
}

// TestListMessages verifies message listing with ordering (ascending/descending by
// created_at), pagination via offset, and correct ExtraFields population for
// parent-child relationships.
func TestListMessages(t *testing.T) {
	t.Run("ordering", func(t *testing.T) {
		tests := []struct {
			name      string
			ascending bool
			wantFirst int // index in savedIDs expected as first result
			wantLast  int // index in savedIDs expected as last result
		}{
			{"descending (default)", false, 2, 0},
			{"ascending", true, 0, 2},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db, _ := newTestDB(t)
				ctx := context.Background()

				var savedIDs []string
				for i := 0; i < 3; i++ {
					id := saveOne(t, db, ctx, makeTextMessage(gai.User, "msg"))
					savedIDs = append(savedIDs, id)
				}

				msgs, err := db.ListMessages(ctx, ListMessagesOptions{AscendingOrder: tt.ascending})
				if err != nil {
					t.Fatalf("ListMessages: %v", err)
				}
				var listedIDs []string
				for m := range msgs {
					id, _ := m.ExtraFields[MessageIDKey].(string)
					listedIDs = append(listedIDs, id)
				}
				if len(listedIDs) != 3 {
					t.Fatalf("expected 3 messages, got %d", len(listedIDs))
				}
				if listedIDs[0] != savedIDs[tt.wantFirst] {
					t.Errorf("first: got %q, want %q", listedIDs[0], savedIDs[tt.wantFirst])
				}
				if listedIDs[2] != savedIDs[tt.wantLast] {
					t.Errorf("last: got %q, want %q", listedIDs[2], savedIDs[tt.wantLast])
				}
			})
		}
	})

	t.Run("offset skips messages", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		for i := 0; i < 5; i++ {
			saveOne(t, db, ctx, makeTextMessage(gai.User, "msg"))
		}

		msgs, err := db.ListMessages(ctx, ListMessagesOptions{Offset: 3})
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		count := 0
		for range msgs {
			count++
		}
		if count != 2 {
			t.Fatalf("expected 2 messages after offset 3, got %d", count)
		}
	})

	t.Run("messages have ID and parent ID in ExtraFields", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "parent"),
			makeTextMessage(gai.Assistant, "child"),
		})
		parentID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)

		msgs, err := db.ListMessages(ctx, ListMessagesOptions{AscendingOrder: true})
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}

		var listed []gai.Message
		for m := range msgs {
			listed = append(listed, m)
		}
		if len(listed) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(listed))
		}

		if _, ok := listed[0].ExtraFields[MessageIDKey]; !ok {
			t.Error("parent should have MessageIDKey")
		}
		if _, ok := listed[0].ExtraFields[MessageParentIDKey]; ok {
			t.Error("parent should not have MessageParentIDKey")
		}

		if _, ok := listed[1].ExtraFields[MessageIDKey]; !ok {
			t.Error("child should have MessageIDKey")
		}
		gotParent, ok := listed[1].ExtraFields[MessageParentIDKey].(string)
		if !ok || gotParent != parentID {
			t.Errorf("expected child MessageParentIDKey = %q, got %q", parentID, gotParent)
		}
	})
}

// TestDeleteMessages verifies DeleteMessages enforces referential integrity by
// preventing non-recursive deletion of messages with children, while allowing
// recursive deletion to remove entire subtrees atomically.
func TestDeleteMessages(t *testing.T) {
	t.Run("delete leaf message non-recursively", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		leafID := saveOne(t, db, ctx, makeTextMessage(gai.User, "leaf"))

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{leafID},
			Recursive:  false,
		})
		if err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		if _, err = db.GetMessages(ctx, []string{leafID}); err == nil {
			t.Fatal("expected error retrieving deleted message")
		}
	})

	t.Run("non-recursive delete of parent with children fails", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "parent"),
			makeTextMessage(gai.Assistant, "child"),
		})
		parentID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		childID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{parentID},
			Recursive:  false,
		})
		if err == nil {
			t.Fatal("expected error deleting parent with children non-recursively")
		}

		// Child should still exist
		if _, err = db.GetMessages(ctx, []string{childID}); err != nil {
			t.Fatalf("child should still exist: %v", err)
		}
	})

	t.Run("recursive delete removes entire tree", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		// root → child → grandchild
		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "child"),
			makeTextMessage(gai.User, "grandchild"),
		})

		var allIDs []string
		for _, s := range saved {
			allIDs = append(allIDs, getExtraFieldString(s.ExtraFields, MessageIDKey))
		}

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{allIDs[0]},
			Recursive:  true,
		})
		if err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		for _, id := range allIDs {
			if _, err = db.GetMessages(ctx, []string{id}); err == nil {
				t.Errorf("message %s should be deleted", id)
			}
		}
	})
}

// TestGetDialogForMessage verifies that conversation history reconstruction
// correctly walks the parent chain from any message back to the root, returning
// messages in chronological order with proper parent ID linkage.
func TestGetDialogForMessage(t *testing.T) {
	t.Run("full chain from leaf to root with correct IDs and parents", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "msg1"),
			makeTextMessage(gai.User, "msg2"),
			makeTextMessage(gai.Assistant, "msg3"),
		})

		var allIDs []string
		for _, s := range saved {
			allIDs = append(allIDs, getExtraFieldString(s.ExtraFields, MessageIDKey))
		}

		dialog, err := GetDialogForMessage(ctx, db, allIDs[3])
		if err != nil {
			t.Fatalf("GetDialogForMessage: %v", err)
		}
		if len(dialog) != 4 {
			t.Fatalf("expected 4 messages in dialog, got %d", len(dialog))
		}

		// Verify order and parent chain
		for i, msg := range dialog {
			gotID, ok := msg.ExtraFields[MessageIDKey].(string)
			if !ok || gotID != allIDs[i] {
				t.Errorf("dialog[%d]: expected ID %q, got %q", i, allIDs[i], gotID)
			}
			if i == 0 {
				if _, ok := msg.ExtraFields[MessageParentIDKey]; ok {
					t.Error("root message should not have MessageParentIDKey")
				}
			} else {
				gotParent, ok := msg.ExtraFields[MessageParentIDKey].(string)
				if !ok || gotParent != allIDs[i-1] {
					t.Errorf("dialog[%d]: expected parent %q, got %q", i, allIDs[i-1], gotParent)
				}
			}
		}
	})

	t.Run("root message returns single-element dialog", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		rootID := saveOne(t, db, ctx, makeTextMessage(gai.User, "root"))

		dialog, err := GetDialogForMessage(ctx, db, rootID)
		if err != nil {
			t.Fatalf("GetDialogForMessage: %v", err)
		}
		if len(dialog) != 1 {
			t.Fatalf("expected 1 message, got %d", len(dialog))
		}
	})

	t.Run("non-existent ID returns error", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		_, err := GetDialogForMessage(ctx, db, "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent ID")
		}
	})
}

// TestSaveAndGetRoundTrip is a comprehensive persistence test that verifies all
// message fields survive a save/retrieve cycle: Role, ToolResultError, multiple blocks
// with different types and modalities, and block-level ExtraFields including nested values.
func TestSaveAndGetRoundTrip(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	msg := gai.Message{
		Role:            gai.ToolResult,
		ToolResultError: true,
		Blocks: []gai.Block{
			{
				ID:           "block-text",
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("hello world"),
			},
			{
				ID:           "block-tool",
				BlockType:    gai.ToolCall,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(`{"name":"tool","parameters":{"key":"value"}}`),
				ExtraFields:  map[string]any{"provider": "test", "version": float64(1)},
			},
			{
				ID:           "block-image",
				BlockType:    gai.Content,
				ModalityType: gai.Image,
				MimeType:     "image/png",
				Content:      gai.Str("base64encodeddata"),
			},
		},
	}

	savedID := saveOne(t, db, ctx, msg)

	msgs, err := db.GetMessages(ctx, []string{savedID})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	var got gai.Message
	for m := range msgs {
		got = m
	}

	if got.Role != gai.ToolResult {
		t.Errorf("expected role ToolResult, got %v", got.Role)
	}
	if !got.ToolResultError {
		t.Error("expected ToolResultError = true")
	}
	if len(got.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(got.Blocks))
	}

	// Verify each block
	wantBlocks := []struct {
		id       string
		btype    string
		modality gai.Modality
		mime     string
		content  string
	}{
		{"block-text", gai.Content, gai.Text, "text/plain", "hello world"},
		{"block-tool", gai.ToolCall, gai.Text, "text/plain", `{"name":"tool","parameters":{"key":"value"}}`},
		{"block-image", gai.Content, gai.Image, "image/png", "base64encodeddata"},
	}
	for i, want := range wantBlocks {
		b := got.Blocks[i]
		if b.ID != want.id {
			t.Errorf("block[%d].ID: expected %q, got %q", i, want.id, b.ID)
		}
		if b.BlockType != want.btype {
			t.Errorf("block[%d].BlockType: expected %q, got %q", i, want.btype, b.BlockType)
		}
		if b.ModalityType != want.modality {
			t.Errorf("block[%d].ModalityType: expected %v, got %v", i, want.modality, b.ModalityType)
		}
		if b.MimeType != want.mime {
			t.Errorf("block[%d].MimeType: expected %q, got %q", i, want.mime, b.MimeType)
		}
		if b.Content.String() != want.content {
			t.Errorf("block[%d].Content: expected %q, got %q", i, want.content, b.Content.String())
		}
	}

	// Verify ExtraFields on tool call block
	b1 := got.Blocks[1]
	if b1.ExtraFields == nil {
		t.Fatal("block[1].ExtraFields should not be nil")
	}
	if v, ok := b1.ExtraFields["provider"].(string); !ok || v != "test" {
		t.Errorf("block[1].ExtraFields[provider]: expected %q, got %v", "test", b1.ExtraFields["provider"])
	}
	if v, ok := b1.ExtraFields["version"].(float64); !ok || v != 1 {
		t.Errorf("block[1].ExtraFields[version]: expected %v, got %v", float64(1), b1.ExtraFields["version"])
	}
}

// TestNewSqlite_SchemaError verifies that NewSqlite returns an error when schema
// initialization fails. Uses an injected ExecContext failure to exercise the error
// path in the DDL execution.
func TestNewSqlite_SchemaError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	realDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { realDB.Close() })

	fdb := &failDB{DB: realDB, failExec: true}
	if _, err = NewSqlite(context.Background(), fdb); err == nil {
		t.Fatal("expected error from failed schema init")
	}
}

// TestRoleConversion verifies edge cases in role-to-string and string-to-role
// conversion functions that protect against data corruption during serialization.
func TestRoleConversion(t *testing.T) {
	t.Run("unknown role to string", func(t *testing.T) {
		if got := roleToString(gai.Role(999)); got != "unknown" {
			t.Errorf("expected %q, got %q", "unknown", got)
		}
	})

	t.Run("invalid string to role", func(t *testing.T) {
		if _, err := stringToRole("invalid_role"); err == nil {
			t.Fatal("expected error for invalid role string")
		}
	})
}

// TestGetMessages_CorruptData verifies that GetMessages returns errors when the
// database contains malformed data. Each case exercises a different corruption
// scenario that could occur from database tampering or bugs in older versions.
func TestGetMessages_CorruptData(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, rawDB *sql.DB)
		msgID string
	}{
		{
			name: "invalid role in database",
			setup: func(t *testing.T, rawDB *sql.DB) {
				_, err := rawDB.ExecContext(context.Background(),
					`INSERT INTO messages (id, role, tool_result_error) VALUES (?, ?, ?)`,
					"bad-role-msg", "bogus_role", false)
				if err != nil {
					t.Fatalf("insert: %v", err)
				}
			},
			msgID: "bad-role-msg",
		},
		{
			name: "corrupt block ExtraFields JSON",
			setup: func(t *testing.T, rawDB *sql.DB) {
				_, err := rawDB.ExecContext(context.Background(),
					`INSERT INTO messages (id, role, tool_result_error) VALUES (?, ?, ?)`,
					"corrupt-ef-msg", "user", false)
				if err != nil {
					t.Fatalf("insert message: %v", err)
				}
				_, err = rawDB.ExecContext(context.Background(),
					`INSERT INTO blocks (message_id, block_type, modality_type, mime_type, content, extra_fields, sequence_order)
					 VALUES (?, ?, ?, ?, ?, ?, ?)`,
					"corrupt-ef-msg", "content", 0, "text/plain", "hello", "not-valid-json{{{", 0)
				if err != nil {
					t.Fatalf("insert block: %v", err)
				}
			},
			msgID: "corrupt-ef-msg",
		},
		{
			name: "GetBlocksByMessage fails (dropped blocks table)",
			setup: func(t *testing.T, rawDB *sql.DB) {
				_, err := rawDB.ExecContext(context.Background(),
					`INSERT INTO messages (id, role, tool_result_error) VALUES (?, ?, ?)`,
					"blocks-err-msg", "user", false)
				if err != nil {
					t.Fatalf("insert: %v", err)
				}
				if _, err = rawDB.ExecContext(context.Background(), "DROP TABLE blocks"); err != nil {
					t.Fatalf("drop blocks: %v", err)
				}
			},
			msgID: "blocks-err-msg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, rawDB := newTestDB(t)
			ctx := context.Background()
			tt.setup(t, rawDB)
			if _, err := db.GetMessages(ctx, []string{tt.msgID}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// TestGenerateUniqueIDInTx_Exhaustion verifies that ID generation fails gracefully
// after exhausting the retry limit (10 attempts) when collisions occur on every try.
func TestGenerateUniqueIDInTx_Exhaustion(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	db.idGenerator = func() string { return "always_same" }
	saveDialog(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "first")})

	// Now every ID attempt collides — should fail after 10 attempts
	expectSaveDialogError(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "second")})
}

// TestBeginTxError verifies that BeginTx failures are properly propagated from
// transactional operations, ensuring database connection issues don't cause silent
// failures or data corruption.
func TestBeginTxError(t *testing.T) {
	tests := []struct {
		name string
		call func(ds *Sqlite) error
	}{
		{
			name: "SaveDialog",
			call: func(ds *Sqlite) error {
				for _, err := range ds.SaveDialog(context.Background(), slices.Values([]gai.Message{
					makeTextMessage(gai.User, "test"),
				})) {
					if err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			name: "DeleteMessages",
			call: func(ds *Sqlite) error {
				return ds.DeleteMessages(context.Background(), DeleteMessagesOptions{
					MessageIDs: []string{"any"},
					Recursive:  false,
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, rawDB := newTestDB(t)
			fdb := &failDB{DB: rawDB, failBeginTx: true}
			ds.db = fdb
			if err := tt.call(ds); err == nil {
				t.Fatal("expected error from BeginTx failure")
			}
		})
	}
}

// TestIteratorEarlyBreak verifies that iterator-returning methods handle early
// consumer breaks correctly. Ensures no panics, resource leaks, or incorrect
// behavior when callers break out of range loops before exhausting results.
func TestIteratorEarlyBreak(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, db *Sqlite, ctx context.Context)
		iterate func(db *Sqlite, ctx context.Context) int
	}{
		{
			name: "SaveDialog break during existing message verification",
			setup: func(t *testing.T, db *Sqlite, ctx context.Context) {
				saveDialog(t, db, ctx, []gai.Message{
					makeTextMessage(gai.User, "msg1"),
					makeTextMessage(gai.Assistant, "msg2"),
					makeTextMessage(gai.User, "msg3"),
				})
			},
			iterate: func(db *Sqlite, ctx context.Context) int {
				// Re-list all messages to get their saved forms, then re-submit
				msgs, _ := db.ListMessages(ctx, ListMessagesOptions{AscendingOrder: true})
				var saved []gai.Message
				for m := range msgs {
					saved = append(saved, m)
				}
				consumed := 0
				for _, err := range db.SaveDialog(ctx, slices.Values(saved)) {
					if err != nil {
						return -1
					}
					consumed++
					if consumed == 1 {
						break
					}
				}
				return consumed
			},
		},
		{
			name: "ListMessages",
			setup: func(t *testing.T, db *Sqlite, ctx context.Context) {
				for i := 0; i < 3; i++ {
					saveOne(t, db, ctx, makeTextMessage(gai.User, fmt.Sprintf("msg%d", i)))
				}
			},
			iterate: func(db *Sqlite, ctx context.Context) int {
				msgs, _ := db.ListMessages(ctx, ListMessagesOptions{})
				consumed := 0
				for range msgs {
					consumed++
					if consumed == 1 {
						break
					}
				}
				return consumed
			},
		},
		{
			name: "GetMessages",
			setup: func(t *testing.T, db *Sqlite, ctx context.Context) {
				// Save two separate root messages
				saveOne(t, db, ctx, makeTextMessage(gai.User, "msg1"))
				saveOne(t, db, ctx, makeTextMessage(gai.User, "msg2"))
			},
			iterate: func(db *Sqlite, ctx context.Context) int {
				// Get all message IDs
				allMsgs, _ := db.ListMessages(ctx, ListMessagesOptions{})
				var ids []string
				for m := range allMsgs {
					ids = append(ids, getExtraFieldString(m.ExtraFields, MessageIDKey))
				}
				msgs, _ := db.GetMessages(ctx, ids)
				consumed := 0
				for range msgs {
					consumed++
					if consumed == 1 {
						break
					}
				}
				return consumed
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := newTestDB(t)
			ctx := context.Background()
			tt.setup(t, db, ctx)
			if got := tt.iterate(db, ctx); got != 1 {
				t.Fatalf("expected to consume 1, got %d", got)
			}
		})
	}
}

// TestSaveDialog_CommitError verifies that transaction commit failures are handled
// correctly depending on iterator consumption state. Uses a deferred FK violation
// to trigger commit errors.
func TestSaveDialog_CommitError(t *testing.T) {
	t.Run("consumer still active", func(t *testing.T) {
		ds, rawDB := newTestDBWithFK(t)
		addPoisonTrigger(t, rawDB, "insert")

		expectSaveDialogError(t, ds, context.Background(), []gai.Message{
			makeTextMessage(gai.User, "commit-test"),
		})
	})

	t.Run("consumer stopped", func(t *testing.T) {
		ds, rawDB := newTestDBWithFK(t)
		addPoisonTrigger(t, rawDB, "insert")

		// Break after first message — commit fails silently (consumer stopped)
		consumed := 0
		for _, err := range ds.SaveDialog(context.Background(), slices.Values([]gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
		})) {
			if err != nil {
				t.Fatalf("unexpected error during iteration: %v", err)
			}
			consumed++
			if consumed == 1 {
				break
			}
		}
		// No panic = success. The commit fails after consumer broke out of the
		// loop, so the error cannot be propagated via yield. The deferred
		// Rollback cleans up silently.
	})
}

// TestDeleteMessages_CommitError verifies that commit failures during DeleteMessages
// are properly propagated to the caller. Uses a deferred FK violation triggered on
// DELETE.
func TestDeleteMessages_CommitError(t *testing.T) {
	ds, rawDB := newTestDBWithFK(t)
	ctx := context.Background()

	id := saveOne(t, ds, ctx, makeTextMessage(gai.User, "to-delete"))
	addPoisonTrigger(t, rawDB, "delete")

	err := ds.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{id},
		Recursive:  false,
	})
	if err == nil {
		t.Fatal("expected commit error from deferred FK violation")
	}
}

// TestDeleteMessages_ErrorPaths exercises error handling for each database operation
// in DeleteMessages by breaking underlying tables or blocking operations with
// triggers.
func TestDeleteMessages_ErrorPaths(t *testing.T) {
	t.Run("HasChildren error (renamed table)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		id := saveOne(t, db, ctx, makeTextMessage(gai.User, "leaf"))

		if _, err := rawDB.ExecContext(ctx, "ALTER TABLE messages RENAME TO messages_old"); err != nil {
			t.Fatalf("rename: %v", err)
		}

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{id},
			Recursive:  false,
		})
		if err == nil {
			t.Fatal("expected error from HasChildren on renamed table")
		}
	})

	t.Run("DeleteMessage error (trigger blocks delete)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		_, err := rawDB.ExecContext(ctx,
			`INSERT INTO messages (id, role, tool_result_error) VALUES (?, ?, ?)`,
			"delete-err-msg", "user", false)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}

		addBlockingTrigger(t, rawDB, "messages", "DELETE")

		err = db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{"delete-err-msg"},
			Recursive:  false,
		})
		if err == nil {
			t.Fatal("expected error from DeleteMessage blocked by trigger")
		}
	})

	t.Run("ListMessagesByParent error (renamed table)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "child"),
		})
		rootID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)

		if _, err := rawDB.ExecContext(ctx, "ALTER TABLE messages RENAME TO messages_old"); err != nil {
			t.Fatalf("rename: %v", err)
		}

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{rootID},
			Recursive:  true,
		})
		if err == nil {
			t.Fatal("expected error from ListMessagesByParent on renamed table")
		}
	})

	t.Run("recursive child deletion error (trigger blocks delete)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "child"),
		})
		rootID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)

		addBlockingTrigger(t, rawDB, "messages", "DELETE")

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{rootID},
			Recursive:  true,
		})
		if err == nil {
			t.Fatal("expected error from recursive child deletion")
		}
	})
}

// TestListMessages_ErrorPaths verifies error handling when the initial query fails
// (closed DB) or when getMessage fails during result iteration (invalid role data
// in storage).
func TestListMessages_ErrorPaths(t *testing.T) {
	t.Run("query fails on closed DB", func(t *testing.T) {
		tests := []struct {
			name      string
			ascending bool
		}{
			{"descending", false},
			{"ascending", true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dbPath := filepath.Join(t.TempDir(), "test.db")
				rawDB, err := sql.Open("sqlite3", dbPath)
				if err != nil {
					t.Fatalf("sql.Open: %v", err)
				}
				ds, err := NewSqlite(context.Background(), rawDB)
				if err != nil {
					t.Fatalf("NewSqlite: %v", err)
				}
				rawDB.Close()

				if _, err = ds.ListMessages(context.Background(), ListMessagesOptions{AscendingOrder: tt.ascending}); err == nil {
					t.Fatal("expected error from closed DB")
				}
			})
		}
	})

	t.Run("getMessage fails during iteration", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		// Insert message with invalid role — ListMessages finds it but getMessage fails
		if _, err := rawDB.ExecContext(ctx,
			`INSERT INTO messages (id, role, tool_result_error) VALUES (?, ?, ?)`,
			"bad-role-list", "not_a_role", false); err != nil {
			t.Fatalf("insert: %v", err)
		}

		if _, err := db.ListMessages(ctx, ListMessagesOptions{}); err == nil {
			t.Fatal("expected error from getMessage with invalid role during ListMessages")
		}
	})
}

// TestSaveDialog_SaveErrors exercises error paths during message persistence:
// blocked INSERT, missing blocks table, unmarshallable ExtraFields, and ID
// generation on missing tables.
func TestSaveDialog_SaveErrors(t *testing.T) {
	t.Run("CreateMessage error (trigger blocks insert)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		rootID := saveOne(t, db, ctx, makeTextMessage(gai.User, "root"))

		addBlockingTrigger(t, rawDB, "messages", "INSERT")

		rootMsg := makeTextMessage(gai.User, "root")
		rootMsg.ExtraFields = map[string]any{MessageIDKey: rootID}
		expectSaveDialogError(t, db, ctx, []gai.Message{rootMsg, makeTextMessage(gai.Assistant, "should-fail")})
	})

	t.Run("CreateBlock error (dropped blocks table)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		if _, err := rawDB.ExecContext(ctx, "DROP TABLE blocks"); err != nil {
			t.Fatalf("drop blocks table: %v", err)
		}

		expectSaveDialogError(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "should-fail")})
	})

	t.Run("ExtraFields marshal error", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		msg := gai.Message{
			Role: gai.User,
			Blocks: []gai.Block{{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("test"),
				ExtraFields:  map[string]any{"bad": make(chan int)},
			}},
		}
		expectSaveDialogError(t, db, ctx, []gai.Message{msg})
	})

	t.Run("CheckMessageIDExists error (dropped tables)", func(t *testing.T) {
		db, rawDB := newTestDB(t)
		ctx := context.Background()

		if _, err := rawDB.ExecContext(ctx, "DROP TABLE blocks; DROP TABLE messages;"); err != nil {
			t.Fatalf("drop tables: %v", err)
		}

		expectSaveDialogError(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "test")})
	})
}

// TestGetExtraFieldString verifies the helper returns empty string for nil maps,
// missing keys, and non-string values, and returns the value only when the key
// exists with string type.
func TestGetExtraFieldString(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{"nil map", nil, "key", ""},
		{"missing key", map[string]any{"other": "val"}, "key", ""},
		{"wrong type", map[string]any{"key": 42}, "key", ""},
		{"present", map[string]any{"key": "value"}, "key", "value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getExtraFieldString(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getExtraFieldString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// mockGetter implements MessagesGetter, returning only messages in its map.
// Missing IDs silently return nothing (no error), testing the "!found" branch.
type mockGetter struct {
	messages map[string]gai.Message
}

func (m *mockGetter) GetMessages(_ context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
	var msgs []gai.Message
	for _, id := range messageIDs {
		if msg, ok := m.messages[id]; ok {
			msgs = append(msgs, msg)
		}
	}
	return slices.Values(msgs), nil
}

// TestCollectAncestorMessages verifies GetDialogForMessage returns errors when the
// target message doesn't exist or when a parent in the ancestor chain is missing.
func TestCollectAncestorMessages(t *testing.T) {
	t.Run("message not found", func(t *testing.T) {
		getter := &mockGetter{messages: map[string]gai.Message{}}
		if _, err := GetDialogForMessage(context.Background(), getter, "nonexistent"); err == nil {
			t.Fatal("expected error for message not found")
		}
	})

	t.Run("parent not found", func(t *testing.T) {
		getter := &mockGetter{
			messages: map[string]gai.Message{
				"child": {
					Role: gai.User,
					ExtraFields: map[string]any{
						MessageIDKey:       "child",
						MessageParentIDKey: "missing-parent",
					},
				},
			},
		}
		if _, err := GetDialogForMessage(context.Background(), getter, "child"); err == nil {
			t.Fatal("expected error for missing parent")
		}
	})
}

// TestBlockOrderPreservation verifies that blocks within a message are returned in
// the same order they were saved. Catches regressions in sequence_order handling.
func TestBlockOrderPreservation(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	// Save a message with many blocks in a specific order
	blocks := make([]gai.Block, 10)
	for i := range blocks {
		blocks[i] = gai.Block{
			ID:           fmt.Sprintf("block-%d", i),
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str(fmt.Sprintf("content-%d", i)),
		}
	}
	msg := gai.Message{Role: gai.User, Blocks: blocks}
	savedID := saveOne(t, db, ctx, msg)

	msgs, err := db.GetMessages(ctx, []string{savedID})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	var got gai.Message
	for m := range msgs {
		got = m
	}
	if len(got.Blocks) != 10 {
		t.Fatalf("expected 10 blocks, got %d", len(got.Blocks))
	}
	for i, b := range got.Blocks {
		wantID := fmt.Sprintf("block-%d", i)
		wantContent := fmt.Sprintf("content-%d", i)
		if b.ID != wantID {
			t.Errorf("block[%d].ID: expected %q, got %q", i, wantID, b.ID)
		}
		if b.Content.String() != wantContent {
			t.Errorf("block[%d].Content: expected %q, got %q", i, wantContent, b.Content.String())
		}
	}
}

// TestSpecialCharacterRoundTrip verifies that content with special characters
// survives storage and retrieval without corruption or SQL injection vulnerabilities.
func TestSpecialCharacterRoundTrip(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		content string
	}{
		{"unicode emoji", "Hello 🎉🚀 World"},
		{"CJK characters", "你好世界"},
		{"newlines", "line1\nline2\r\nline3"},
		{"SQL metacharacters", "'; DROP TABLE messages; --"},
		{"empty string", ""},
		{"backslashes and quotes", `back\slash "double" 'single'`},
		{"NUL byte", "before\x00after"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := gai.Message{
				Role: gai.User,
				Blocks: []gai.Block{{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(tt.content),
				}},
			}
			savedID := saveOne(t, db, ctx, msg)

			msgs, err := db.GetMessages(ctx, []string{savedID})
			if err != nil {
				t.Fatalf("GetMessages: %v", err)
			}
			for m := range msgs {
				got := m.Blocks[0].Content.String()
				if got != tt.content {
					t.Errorf("content mismatch: expected %q, got %q", tt.content, got)
				}
			}
		})
	}
}

// TestCreatedAtPopulated verifies that the database's DEFAULT CURRENT_TIMESTAMP
// populates MessageCreatedAtKey in ExtraFields on retrieval.
func TestCreatedAtPopulated(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	savedID := saveOne(t, db, ctx, makeTextMessage(gai.User, "hello"))

	msgs, err := db.GetMessages(ctx, []string{savedID})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for m := range msgs {
		ts, ok := m.ExtraFields[MessageCreatedAtKey]
		if !ok {
			t.Fatal("expected MessageCreatedAtKey in ExtraFields")
		}
		// The value should be non-zero (SQLite DEFAULT CURRENT_TIMESTAMP)
		if ts == nil {
			t.Fatal("expected non-nil timestamp")
		}
	}
}

// TestGetDialogForMessage_MidChain verifies that GetDialogForMessage returns only
// the ancestor chain up to the specified message, not descendants beyond it.
func TestGetDialogForMessage_MidChain(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	saved := saveDialog(t, db, ctx, []gai.Message{
		makeTextMessage(gai.User, "A"),
		makeTextMessage(gai.Assistant, "B"),
		makeTextMessage(gai.User, "C"),
		makeTextMessage(gai.Assistant, "D"),
	})

	// Get dialog from B (mid-chain) — should return [A, B]
	bID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)
	dialog, err := GetDialogForMessage(ctx, db, bID)
	if err != nil {
		t.Fatalf("GetDialogForMessage: %v", err)
	}
	if len(dialog) != 2 {
		t.Fatalf("expected 2 messages (A → B), got %d", len(dialog))
	}

	aID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
	if gotID := getExtraFieldString(dialog[0].ExtraFields, MessageIDKey); gotID != aID {
		t.Errorf("dialog[0]: expected %q, got %q", aID, gotID)
	}
	if gotID := getExtraFieldString(dialog[1].ExtraFields, MessageIDKey); gotID != bID {
		t.Errorf("dialog[1]: expected %q, got %q", bID, gotID)
	}
}

// TestTreeBranching verifies conversation tree semantics: multiple children can
// share the same parent, each branch maintains independent dialog history, and
// deleting one branch does not affect siblings or the shared root.
func TestTreeBranching(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	// Save root → child1
	saved1 := saveDialog(t, db, ctx, []gai.Message{
		makeTextMessage(gai.User, "root"),
		makeTextMessage(gai.Assistant, "child1"),
	})
	rootID := getExtraFieldString(saved1[0].ExtraFields, MessageIDKey)
	child1ID := getExtraFieldString(saved1[1].ExtraFields, MessageIDKey)

	// Save root → child2 (branching from the same root)
	saved2 := saveDialog(t, db, ctx, []gai.Message{
		saved1[0], // re-use existing root
		makeTextMessage(gai.Assistant, "child2"),
	})
	child2ID := getExtraFieldString(saved2[1].ExtraFields, MessageIDKey)

	// Both children should have different IDs
	if child1ID == child2ID {
		t.Fatal("branch children should have different IDs")
	}

	// Both children should share the same parent
	child2Parent := getExtraFieldString(saved2[1].ExtraFields, MessageParentIDKey)
	if child2Parent != rootID {
		t.Errorf("child2 parent: expected %q, got %q", rootID, child2Parent)
	}

	// GetDialogForMessage from child1 should return [root, child1]
	dialog1, err := GetDialogForMessage(ctx, db, child1ID)
	if err != nil {
		t.Fatalf("GetDialogForMessage(child1): %v", err)
	}
	if len(dialog1) != 2 {
		t.Fatalf("expected 2 messages in child1 dialog, got %d", len(dialog1))
	}

	// GetDialogForMessage from child2 should return [root, child2]
	dialog2, err := GetDialogForMessage(ctx, db, child2ID)
	if err != nil {
		t.Fatalf("GetDialogForMessage(child2): %v", err)
	}
	if len(dialog2) != 2 {
		t.Fatalf("expected 2 messages in child2 dialog, got %d", len(dialog2))
	}

	// Both dialogs should share the same root
	if d1Root := getExtraFieldString(dialog1[0].ExtraFields, MessageIDKey); d1Root != rootID {
		t.Errorf("child1 dialog root: expected %q, got %q", rootID, d1Root)
	}
	if d2Root := getExtraFieldString(dialog2[0].ExtraFields, MessageIDKey); d2Root != rootID {
		t.Errorf("child2 dialog root: expected %q, got %q", rootID, d2Root)
	}

	// Deleting child1 non-recursively should leave child2 and root intact
	err = db.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{child1ID},
		Recursive:  false,
	})
	if err != nil {
		t.Fatalf("DeleteMessages(child1): %v", err)
	}

	// child2 dialog should still work
	dialog2After, err := GetDialogForMessage(ctx, db, child2ID)
	if err != nil {
		t.Fatalf("GetDialogForMessage(child2) after delete: %v", err)
	}
	if len(dialog2After) != 2 {
		t.Fatalf("expected 2 messages in child2 dialog after deleting child1, got %d", len(dialog2After))
	}
}

// TestMessageWithZeroBlocks verifies that messages with no content blocks can be
// saved and retrieved without error. Guards against nil slice handling bugs.
func TestMessageWithZeroBlocks(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	msg := gai.Message{Role: gai.User, Blocks: nil}
	savedID := saveOne(t, db, ctx, msg)

	msgs, err := db.GetMessages(ctx, []string{savedID})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for m := range msgs {
		if len(m.Blocks) != 0 {
			t.Errorf("expected 0 blocks, got %d", len(m.Blocks))
		}
		if m.Role != gai.User {
			t.Errorf("expected role User, got %v", m.Role)
		}
	}
}

// TestGetMessages_EmptyIDs verifies that an empty ID slice returns an empty
// iterator without error, rather than failing or returning all messages.
func TestGetMessages_EmptyIDs(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	msgs, err := db.GetMessages(ctx, []string{})
	if err != nil {
		t.Fatalf("GetMessages(empty): %v", err)
	}
	count := 0
	for range msgs {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 messages for empty ID list, got %d", count)
	}
}

// TestGetMessages_DuplicateIDs documents that the current implementation fetches
// each ID independently, so duplicate IDs produce duplicate messages in the result.
func TestGetMessages_DuplicateIDs(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	id := saveOne(t, db, ctx, makeTextMessage(gai.User, "hello"))

	msgs, err := db.GetMessages(ctx, []string{id, id})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	count := 0
	for range msgs {
		count++
	}
	// Current implementation fetches each ID independently, so duplicates produce duplicates
	if count != 2 {
		t.Errorf("expected 2 messages for duplicate IDs, got %d", count)
	}
}

// TestListMessages_OffsetBeyondTotal verifies that an offset exceeding the total
// message count returns an empty result gracefully rather than erroring.
func TestListMessages_OffsetBeyondTotal(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		saveOne(t, db, ctx, makeTextMessage(gai.User, "msg"))
	}

	msgs, err := db.ListMessages(ctx, ListMessagesOptions{Offset: 100})
	if err != nil {
		t.Fatalf("ListMessages with large offset: %v", err)
	}
	count := 0
	for range msgs {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 messages with offset beyond total, got %d", count)
	}
}

// TestDeleteMiddleMessage_OrphansChild verifies that GetDialogForMessage correctly
// fails when traversing through a deleted parent, detecting orphaned messages.
func TestDeleteMiddleMessage_OrphansChild(t *testing.T) {
	// If we delete a message in the middle of a chain (using raw SQL to bypass
	// the RESTRICT constraint since FKs are off by default), the child becomes
	// an orphan. GetDialogForMessage should error when traversing to the
	// missing parent.
	db, rawDB := newTestDB(t)
	ctx := context.Background()

	saved := saveDialog(t, db, ctx, []gai.Message{
		makeTextMessage(gai.User, "A"),
		makeTextMessage(gai.Assistant, "B"),
		makeTextMessage(gai.User, "C"),
	})
	bID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)
	cID := getExtraFieldString(saved[2].ExtraFields, MessageIDKey)

	// Delete B directly (FKs off by default, so no RESTRICT enforcement)
	_, err := rawDB.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", bID)
	if err != nil {
		t.Fatalf("direct delete: %v", err)
	}

	// C still exists but its parent (B) is gone
	_, err = GetDialogForMessage(ctx, db, cID)
	if err == nil {
		t.Fatal("expected error traversing to deleted parent")
	}
}

// TestBlockCascadeDeleteViaRawSQL verifies that the ON DELETE CASCADE foreign key
// constraint on the blocks table works correctly when a message is deleted.
func TestBlockCascadeDeleteViaRawSQL(t *testing.T) {
	// Verify that ON DELETE CASCADE on blocks works when FKs are enabled
	db, rawDB := newTestDB(t)
	ctx := context.Background()

	// Enable foreign keys
	if _, err := rawDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	msg := gai.Message{
		Role: gai.User,
		Blocks: []gai.Block{
			{BlockType: gai.Content, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("text")},
			{BlockType: gai.Content, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("more")},
		},
	}
	savedID := saveOne(t, db, ctx, msg)

	// Verify blocks exist
	var blockCount int
	err := rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocks WHERE message_id = ?", savedID).Scan(&blockCount)
	if err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if blockCount != 2 {
		t.Fatalf("expected 2 blocks before delete, got %d", blockCount)
	}

	// Delete the message directly
	_, err = rawDB.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", savedID)
	if err != nil {
		t.Fatalf("delete message: %v", err)
	}

	// Blocks should be cascade-deleted
	err = rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM blocks WHERE message_id = ?", savedID).Scan(&blockCount)
	if err != nil {
		t.Fatalf("count blocks after: %v", err)
	}
	if blockCount != 0 {
		t.Errorf("expected 0 blocks after cascade delete, got %d", blockCount)
	}
}

// TestMessageExtraFields verifies that only known keys (id, parent_id, created_at,
// title) stored in dedicated columns are persisted and returned; arbitrary custom
// keys are dropped.
func TestMessageExtraFields(t *testing.T) {
	t.Run("known keys returned, custom keys dropped", func(t *testing.T) {
		// Arbitrary message-level ExtraFields are intentionally not persisted.
		// Only known keys (ID, parent, created_at, title) stored as dedicated
		// columns are returned on retrieval.
		db, _ := newTestDB(t)
		ctx := context.Background()

		msg := makeTextMessage(gai.User, "test")
		msg.ExtraFields = map[string]any{
			"custom_key":    "custom_value",
			MessageTitleKey: "my-title",
		}

		saved := saveDialog(t, db, ctx, []gai.Message{msg})
		savedID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)

		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		for m := range msgs {
			if _, ok := m.ExtraFields[MessageIDKey]; !ok {
				t.Error("expected MessageIDKey")
			}
			if _, ok := m.ExtraFields[MessageCreatedAtKey]; !ok {
				t.Error("expected MessageCreatedAtKey")
			}
			if got, ok := m.ExtraFields[MessageTitleKey].(string); !ok || got != "my-title" {
				t.Errorf("expected MessageTitleKey = %q, got %v", "my-title", m.ExtraFields[MessageTitleKey])
			}
			if _, ok := m.ExtraFields["custom_key"]; ok {
				t.Error("custom_key should not be persisted")
			}
		}
	})

	t.Run("title absent when not set", func(t *testing.T) {
		db, _ := newTestDB(t)
		ctx := context.Background()

		savedID := saveOne(t, db, ctx, makeTextMessage(gai.User, "no title"))

		msgs, err := db.GetMessages(ctx, []string{savedID})
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		for m := range msgs {
			if _, ok := m.ExtraFields[MessageTitleKey]; ok {
				t.Error("MessageTitleKey should not be present when no title was set")
			}
		}
	})
}

// TestSaveDialog_InterleavedExistingAndNew verifies that SaveDialog detects parent
// chain mismatches when existing messages from different chains are mixed with new
// messages.
func TestSaveDialog_InterleavedExistingAndNew(t *testing.T) {
	// When existing messages followed by new messages followed by more existing
	// messages are submitted, the parent chain validation should catch the
	// mismatch on the second existing message.
	db, _ := newTestDB(t)
	ctx := context.Background()

	// Save two separate chains
	chainA := saveDialog(t, db, ctx, []gai.Message{
		makeTextMessage(gai.User, "A1"),
		makeTextMessage(gai.Assistant, "A2"),
	})
	chainB := saveDialog(t, db, ctx, []gai.Message{
		makeTextMessage(gai.User, "B1"),
	})

	// Try: [A1(existing), NEW, B1(existing)]
	// After A1 and NEW are processed, prevID = NEW's ID.
	// B1's parent in DB is NULL (it's a root), but prevID is NEW's ID.
	// This should fail parent chain validation for B1.
	dialog := []gai.Message{
		chainA[0],
		makeTextMessage(gai.Assistant, "new-middle"),
		chainB[0],
	}

	expectSaveDialogError(t, db, ctx, dialog)
}

// TestDeleteMessages_MultipleIDs verifies that DeleteMessages can atomically delete
// multiple leaf messages in a single call while leaving other messages intact.
func TestDeleteMessages_MultipleIDs(t *testing.T) {
	// Verify deleting multiple leaf messages in one call works
	db, _ := newTestDB(t)
	ctx := context.Background()

	id1 := saveOne(t, db, ctx, makeTextMessage(gai.User, "leaf1"))
	id2 := saveOne(t, db, ctx, makeTextMessage(gai.User, "leaf2"))
	id3 := saveOne(t, db, ctx, makeTextMessage(gai.User, "leaf3"))

	err := db.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{id1, id3},
		Recursive:  false,
	})
	if err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	// id1 and id3 should be gone
	for _, id := range []string{id1, id3} {
		if _, err := db.GetMessages(ctx, []string{id}); err == nil {
			t.Errorf("message %s should be deleted", id)
		}
	}
	// id2 should still exist
	if _, err := db.GetMessages(ctx, []string{id2}); err != nil {
		t.Fatalf("message %s should still exist: %v", id2, err)
	}
}

// TestListMessages_CreatedAtOrdering verifies that ListMessages returns messages
// sorted by created_at timestamps in the requested order.
func TestListMessages_CreatedAtOrdering(t *testing.T) {
	// Verify that ListMessages ordering actually uses created_at timestamps
	// and that the returned messages have timestamps in the correct order.
	db, _ := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		saveOne(t, db, ctx, makeTextMessage(gai.User, fmt.Sprintf("msg-%d", i)))
	}

	// Ascending: timestamps should be non-decreasing
	msgs, err := db.ListMessages(ctx, ListMessagesOptions{AscendingOrder: true})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	var prev string
	for m := range msgs {
		ts := fmt.Sprintf("%v", m.ExtraFields[MessageCreatedAtKey])
		if prev != "" && ts < prev {
			t.Errorf("timestamps not ascending: %q came after %q", ts, prev)
		}
		prev = ts
	}
}
