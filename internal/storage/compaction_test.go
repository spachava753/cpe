package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/gai"
)

func saveDialogGeneric(t *testing.T, saver DialogSaver, msgs []gai.Message) []gai.Message {
	t.Helper()
	var saved []gai.Message
	for msg, err := range saver.SaveDialog(context.Background(), slices.Values(msgs)) {
		if err != nil {
			t.Fatalf("SaveDialog returned error: %v", err)
		}
		saved = append(saved, msg)
	}
	return saved
}

func TestNewSqlite_MigratesLegacyCompactionParentColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-compaction.db")
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer rawDB.Close()

	ctx := context.Background()
	if _, err := rawDB.ExecContext(ctx, `
		CREATE TABLE messages (
			id TEXT PRIMARY KEY,
			parent_id TEXT,
			is_subagent BOOLEAN NOT NULL DEFAULT 0,
			role TEXT NOT NULL,
			tool_result_error BOOLEAN NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE blocks (
			id TEXT,
			message_id TEXT NOT NULL,
			block_type TEXT NOT NULL,
			modality_type INTEGER NOT NULL,
			mime_type TEXT NOT NULL,
			content TEXT NOT NULL,
			extra_fields TEXT,
			sequence_order INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (message_id, sequence_order)
		);
	`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	if _, err := NewSqlite(ctx, rawDB); err != nil {
		t.Fatalf("NewSqlite: %v", err)
	}

	var hasColumn int
	if err := rawDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pragma_table_info('messages')
		WHERE name = 'compaction_parent_id'
	`).Scan(&hasColumn); err != nil {
		t.Fatalf("check compaction_parent_id column: %v", err)
	}
	if hasColumn != 1 {
		t.Fatalf("expected compaction_parent_id column to be created, got count=%d", hasColumn)
	}
}

func TestSqlite_CompactionParentIDRoundTrip(t *testing.T) {
	db, _ := newTestDB(t)
	assertCompactionLineageRoundTrip(t, db)
}

func TestMemDB_CompactionParentIDRoundTrip(t *testing.T) {
	db := NewMemDB()
	assertCompactionLineageRoundTrip(t, db)
}

func assertCompactionLineageRoundTrip(t *testing.T, db MessageDB) {
	t.Helper()

	const priorParentID = "prior_123"

	root := makeTextMessage(gai.User, "compacted root")
	root.ExtraFields = map[string]any{MessageCompactionParentIDKey: priorParentID}
	child := makeTextMessage(gai.Assistant, "continued reply")
	saved := saveDialogGeneric(t, db, []gai.Message{root, child})
	if len(saved) != 2 {
		t.Fatalf("expected 2 saved messages, got %d", len(saved))
	}

	rootID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
	childID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)
	if got := getExtraFieldString(saved[0].ExtraFields, MessageCompactionParentIDKey); got != priorParentID {
		t.Fatalf("saved root compaction parent: got %q want %q", got, priorParentID)
	}
	if got := getExtraFieldString(saved[1].ExtraFields, MessageCompactionParentIDKey); got != "" {
		t.Fatalf("saved child compaction parent: got %q want empty", got)
	}

	msgs, err := db.GetMessages(context.Background(), []string{rootID, childID})
	if err != nil {
		t.Fatalf("GetMessages returned error: %v", err)
	}
	var gotMsgs []gai.Message
	for msg := range msgs {
		gotMsgs = append(gotMsgs, msg)
	}
	if len(gotMsgs) != 2 {
		t.Fatalf("expected 2 loaded messages, got %d", len(gotMsgs))
	}
	if got := getExtraFieldString(gotMsgs[0].ExtraFields, MessageCompactionParentIDKey); got != priorParentID {
		t.Fatalf("loaded root compaction parent: got %q want %q", got, priorParentID)
	}
	if got := getExtraFieldString(gotMsgs[1].ExtraFields, MessageCompactionParentIDKey); got != "" {
		t.Fatalf("loaded child compaction parent: got %q want empty", got)
	}

	listed, err := db.ListMessages(context.Background(), ListMessagesOptions{AscendingOrder: true})
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	var listedMsgs []gai.Message
	for msg := range listed {
		listedMsgs = append(listedMsgs, msg)
	}
	if len(listedMsgs) != 2 {
		t.Fatalf("expected 2 listed messages, got %d", len(listedMsgs))
	}
	if got := getExtraFieldString(listedMsgs[0].ExtraFields, MessageCompactionParentIDKey); got != priorParentID {
		t.Fatalf("listed root compaction parent: got %q want %q", got, priorParentID)
	}
	if got := getExtraFieldString(listedMsgs[1].ExtraFields, MessageCompactionParentIDKey); got != "" {
		t.Fatalf("listed child compaction parent: got %q want empty", got)
	}

	dialog, err := GetDialogForMessage(context.Background(), db, childID)
	if err != nil {
		t.Fatalf("GetDialogForMessage returned error: %v", err)
	}
	if len(dialog) != 2 {
		t.Fatalf("expected 2 dialog messages, got %d", len(dialog))
	}
	if got := getExtraFieldString(dialog[0].ExtraFields, MessageCompactionParentIDKey); got != priorParentID {
		t.Fatalf("dialog root compaction parent: got %q want %q", got, priorParentID)
	}
	if got := getExtraFieldString(dialog[1].ExtraFields, MessageCompactionParentIDKey); got != "" {
		t.Fatalf("dialog child compaction parent: got %q want empty", got)
	}
}

func TestSqlite_NonRootCompactionParentIDIgnored(t *testing.T) {
	db, _ := newTestDB(t)
	assertNonRootCompactionParentIDIgnored(t, db)
}

func TestMemDB_NonRootCompactionParentIDIgnored(t *testing.T) {
	db := NewMemDB()
	assertNonRootCompactionParentIDIgnored(t, db)
}

func assertNonRootCompactionParentIDIgnored(t *testing.T, db MessageDB) {
	t.Helper()

	initial := saveDialogGeneric(t, db, []gai.Message{
		makeTextMessage(gai.User, "root"),
		makeTextMessage(gai.Assistant, "reply"),
	})

	continued := makeTextMessage(gai.User, "follow-up")
	continued.ExtraFields = map[string]any{MessageCompactionParentIDKey: "should_not_persist"}
	result := saveDialogGeneric(t, db, append(initial, continued))
	if len(result) != 3 {
		t.Fatalf("expected 3 saved messages, got %d", len(result))
	}
	if got := getExtraFieldString(result[2].ExtraFields, MessageCompactionParentIDKey); got != "" {
		t.Fatalf("non-root compaction parent should be ignored, got %q", got)
	}

	leafID := getExtraFieldString(result[2].ExtraFields, MessageIDKey)
	msgs, err := db.GetMessages(context.Background(), []string{leafID})
	if err != nil {
		t.Fatalf("GetMessages returned error: %v", err)
	}
	for msg := range msgs {
		if got := getExtraFieldString(msg.ExtraFields, MessageCompactionParentIDKey); got != "" {
			t.Fatalf("loaded non-root compaction parent should be empty, got %q", got)
		}
	}
}

func TestOrdinaryConversationHasEmptyCompactionLineage(t *testing.T) {
	for _, tt := range []struct {
		name string
		db   MessageDB
	}{
		{name: "sqlite", db: func() MessageDB { db, _ := newTestDB(t); return db }()},
		{name: "memdb", db: NewMemDB()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			saved := saveDialogGeneric(t, tt.db, []gai.Message{makeTextMessage(gai.User, "hello")})
			if got := getExtraFieldString(saved[0].ExtraFields, MessageCompactionParentIDKey); got != "" {
				t.Fatalf("expected empty compaction lineage, got %q", got)
			}
		})
	}
}
