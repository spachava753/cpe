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

func newTestDB(t *testing.T) *Sqlite {
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
	return ds
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

func TestSaveDialog(t *testing.T) {
	t.Run("single root message", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "hello"),
		})

		gotID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		if gotID == "" {
			t.Fatal("expected non-empty ID")
		}
		// Root should not have parent ID
		if parentID := getExtraFieldString(saved[0].ExtraFields, MessageParentIDKey); parentID != "" {
			t.Errorf("root message should not have parent ID, got %q", parentID)
		}
	})

	t.Run("dialog chain sets parent IDs correctly", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
			makeTextMessage(gai.User, "msg3"),
		}
		saved := saveDialog(t, db, ctx, msgs)

		if len(saved) != 3 {
			t.Fatalf("expected 3 saved, got %d", len(saved))
		}

		// All IDs should be unique and non-empty
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

		// First message should have no parent
		if p := getExtraFieldString(saved[0].ExtraFields, MessageParentIDKey); p != "" {
			t.Errorf("first message should have no parent, got %q", p)
		}
		// Second message parent should be first
		if p := getExtraFieldString(saved[1].ExtraFields, MessageParentIDKey); p != ids[0] {
			t.Errorf("second message parent: expected %q, got %q", ids[0], p)
		}
		// Third message parent should be second
		if p := getExtraFieldString(saved[2].ExtraFields, MessageParentIDKey); p != ids[1] {
			t.Errorf("third message parent: expected %q, got %q", ids[1], p)
		}
	})

	t.Run("message with blocks", func(t *testing.T) {
		db := newTestDB(t)
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

		// Verify blocks are persisted by retrieving
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

	t.Run("message with title via ExtraFields", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		msg := makeTextMessage(gai.User, "titled")
		msg.ExtraFields = map[string]any{MessageTitleKey: "subagent:test:run1"}

		saved := saveDialog(t, db, ctx, []gai.Message{msg})
		gotID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		if gotID == "" {
			t.Fatal("expected non-empty ID")
		}
	})

	t.Run("existing messages are verified not re-saved", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// First save a dialog
		msgs := []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "reply"),
		}
		saved := saveDialog(t, db, ctx, msgs)

		// Now save a new dialog that includes the existing messages plus a new one
		newMsg := makeTextMessage(gai.User, "follow-up")
		fullDialog := append(saved, newMsg)

		saved2 := saveDialog(t, db, ctx, fullDialog)

		if len(saved2) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(saved2))
		}

		// First two should have the same IDs as before
		for i := 0; i < 2; i++ {
			origID := getExtraFieldString(saved[i].ExtraFields, MessageIDKey)
			newID := getExtraFieldString(saved2[i].ExtraFields, MessageIDKey)
			if origID != newID {
				t.Errorf("message %d: expected same ID %q, got %q", i, origID, newID)
			}
		}

		// Third should be new
		thirdID := getExtraFieldString(saved2[2].ExtraFields, MessageIDKey)
		if thirdID == "" {
			t.Fatal("expected non-empty ID for new message")
		}
		// Parent should be second message's ID
		thirdParent := getExtraFieldString(saved2[2].ExtraFields, MessageParentIDKey)
		secondID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)
		if thirdParent != secondID {
			t.Errorf("third message parent: expected %q, got %q", secondID, thirdParent)
		}
	})

	t.Run("first existing message with parent returns error", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Save a dialog with a parent-child chain
		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "child"),
		})

		// Try to use the child (which has a parent in DB) as the first message
		// in a new SaveDialog call — should fail because first message must be root
		var gotErr error
		for _, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{saved[1]})) {
			if err != nil {
				gotErr = err
				break
			}
		}
		if gotErr == nil {
			t.Fatal("expected error when first message has a parent in storage")
		}
	})

	t.Run("wrong parent chain for existing message returns error", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Save two separate root dialogs
		saved1 := saveDialog(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "root1")})
		saved2 := saveDialog(t, db, ctx, []gai.Message{makeTextMessage(gai.User, "root2")})

		// Try to save a dialog where root2 follows root1 — parent chain mismatch
		dialog := []gai.Message{saved1[0], saved2[0]}
		var gotErr error
		for _, err := range db.SaveDialog(ctx, slices.Values(dialog)) {
			if err != nil {
				gotErr = err
				break
			}
		}
		if gotErr == nil {
			t.Fatal("expected error for parent chain mismatch")
		}
	})

	t.Run("non-existent ID in dialog returns error", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		msg := makeTextMessage(gai.User, "ghost")
		msg.ExtraFields = map[string]any{MessageIDKey: "nonexistent"}

		var gotErr error
		for _, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{msg})) {
			if err != nil {
				gotErr = err
				break
			}
		}
		if gotErr == nil {
			t.Fatal("expected error for non-existent message ID")
		}
	})

	t.Run("dialog retrievable via GetDialogForMessage", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		msgs := []gai.Message{
			makeTextMessage(gai.User, "first"),
			makeTextMessage(gai.Assistant, "second"),
			makeTextMessage(gai.User, "third"),
		}
		saved := saveDialog(t, db, ctx, msgs)

		lastID := getExtraFieldString(saved[2].ExtraFields, MessageIDKey)
		dialog, err := GetDialogForMessage(ctx, db, lastID)
		if err != nil {
			t.Fatalf("GetDialogForMessage: %v", err)
		}
		if len(dialog) != 3 {
			t.Fatalf("expected 3 messages in dialog chain, got %d", len(dialog))
		}
	})

	t.Run("early break commits saved messages", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
			makeTextMessage(gai.User, "msg3"),
		}

		// Only consume the first 2 results
		consumed := 0
		var firstID, secondID string
		for msg, err := range db.SaveDialog(ctx, slices.Values(msgs)) {
			if err != nil {
				t.Fatalf("SaveDialog: %v", err)
			}
			id := getExtraFieldString(msg.ExtraFields, MessageIDKey)
			switch consumed {
			case 0:
				firstID = id
			case 1:
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

		// Both consumed messages should be retrievable (transaction committed on break)
		_, err := db.GetMessages(ctx, []string{firstID})
		if err != nil {
			t.Fatalf("first message should be retrievable: %v", err)
		}
		_, err = db.GetMessages(ctx, []string{secondID})
		if err != nil {
			t.Fatalf("second message should be retrievable: %v", err)
		}

		// Third message (never processed) should NOT exist — verify total count
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
		db := newTestDB(t)
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

		// Verify no messages were saved
		msgs, err := db.ListMessages(ctx, ListMessagesOptions{})
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		for range msgs {
			t.Fatal("expected no messages in DB")
		}
	})

	t.Run("transaction rolls back on save error", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Override idGenerator to cause collision after first message
		callCount := 0
		db.idGenerator = func() string {
			callCount++
			// Always return the same ID to cause collision
			return "fixed_id"
		}

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg1"),
			makeTextMessage(gai.Assistant, "msg2"),
		}

		var gotErr error
		for _, err := range db.SaveDialog(ctx, slices.Values(msgs)) {
			if err != nil {
				gotErr = err
				break
			}
		}
		if gotErr == nil {
			t.Fatal("expected error from ID collision")
		}

		// First message should NOT be retrievable (transaction rolled back)
		_, err := db.GetMessages(ctx, []string{"fixed_id"})
		if err == nil {
			t.Fatal("message should not exist after rollback")
		}
	})
}

func TestGetMessages(t *testing.T) {
	t.Run("retrieve by ID with correct ExtraFields", func(t *testing.T) {
		db := newTestDB(t)
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
		db := newTestDB(t)
		ctx := context.Background()

		// Save parent and child as a dialog
		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "parent"),
			makeTextMessage(gai.Assistant, "child"),
		})
		parentID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		childID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)

		// Retrieve parent — should not have MessageParentIDKey
		msgs, err := db.GetMessages(ctx, []string{parentID})
		if err != nil {
			t.Fatalf("GetMessages (parent): %v", err)
		}
		for m := range msgs {
			if _, exists := m.ExtraFields[MessageParentIDKey]; exists {
				t.Error("root message should not have MessageParentIDKey")
			}
		}

		// Retrieve child — should have MessageParentIDKey
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
		db := newTestDB(t)
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
		db := newTestDB(t)
		ctx := context.Background()

		_, err := db.GetMessages(ctx, []string{"nonexistent"})
		if err == nil {
			t.Fatal("expected error for non-existent ID")
		}
	})

	t.Run("block ExtraFields round-trip", func(t *testing.T) {
		db := newTestDB(t)
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

func TestListMessages(t *testing.T) {
	t.Run("descending order by default", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Save messages as separate root dialogs (each gets a later created_at)
		var savedIDs []string
		for i := 0; i < 3; i++ {
			id := saveOne(t, db, ctx, makeTextMessage(gai.User, "msg"))
			savedIDs = append(savedIDs, id)
		}

		msgs, err := db.ListMessages(ctx, ListMessagesOptions{})
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
		// Descending: newest first. savedIDs[2] should be first in listed.
		if listedIDs[0] != savedIDs[2] {
			t.Errorf("expected newest first: got %q, want %q", listedIDs[0], savedIDs[2])
		}
		if listedIDs[2] != savedIDs[0] {
			t.Errorf("expected oldest last: got %q, want %q", listedIDs[2], savedIDs[0])
		}
	})

	t.Run("ascending order", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		var savedIDs []string
		for i := 0; i < 3; i++ {
			id := saveOne(t, db, ctx, makeTextMessage(gai.User, "msg"))
			savedIDs = append(savedIDs, id)
		}

		msgs, err := db.ListMessages(ctx, ListMessagesOptions{AscendingOrder: true})
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
		// Ascending: oldest first
		if listedIDs[0] != savedIDs[0] {
			t.Errorf("expected oldest first: got %q, want %q", listedIDs[0], savedIDs[0])
		}
		if listedIDs[2] != savedIDs[2] {
			t.Errorf("expected newest last: got %q, want %q", listedIDs[2], savedIDs[2])
		}
	})

	t.Run("offset skips messages", func(t *testing.T) {
		db := newTestDB(t)
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
		db := newTestDB(t)
		ctx := context.Background()

		// Save parent and child as a dialog
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

		// First (parent) should have MessageIDKey but no MessageParentIDKey
		if _, ok := listed[0].ExtraFields[MessageIDKey]; !ok {
			t.Error("parent should have MessageIDKey")
		}
		if _, ok := listed[0].ExtraFields[MessageParentIDKey]; ok {
			t.Error("parent should not have MessageParentIDKey")
		}

		// Second (child) should have both
		if _, ok := listed[1].ExtraFields[MessageIDKey]; !ok {
			t.Error("child should have MessageIDKey")
		}
		gotParent, ok := listed[1].ExtraFields[MessageParentIDKey].(string)
		if !ok || gotParent != parentID {
			t.Errorf("expected child MessageParentIDKey = %q, got %q", parentID, gotParent)
		}
	})
}

func TestDeleteMessages(t *testing.T) {
	t.Run("delete leaf message non-recursively", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		leafID := saveOne(t, db, ctx, makeTextMessage(gai.User, "leaf"))

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{leafID},
			Recursive:  false,
		})
		if err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		// Verify it's gone
		_, err = db.GetMessages(ctx, []string{leafID})
		if err == nil {
			t.Fatal("expected error retrieving deleted message")
		}
	})

	t.Run("non-recursive delete of parent with children fails", func(t *testing.T) {
		db := newTestDB(t)
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

		// Verify child still exists
		_, err = db.GetMessages(ctx, []string{childID})
		if err != nil {
			t.Fatalf("child should still exist: %v", err)
		}
	})

	t.Run("recursive delete of parent removes parent and child", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "parent"),
			makeTextMessage(gai.Assistant, "child"),
		})
		parentID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		childID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{parentID},
			Recursive:  true,
		})
		if err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		// Both should be gone
		_, err = db.GetMessages(ctx, []string{parentID})
		if err == nil {
			t.Error("parent should be deleted")
		}
		_, err = db.GetMessages(ctx, []string{childID})
		if err == nil {
			t.Error("child should be deleted")
		}
	})

	t.Run("recursive delete of tree", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// root → child → grandchild
		saved := saveDialog(t, db, ctx, []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "child"),
			makeTextMessage(gai.User, "grandchild"),
		})
		rootID := getExtraFieldString(saved[0].ExtraFields, MessageIDKey)
		childID := getExtraFieldString(saved[1].ExtraFields, MessageIDKey)
		grandchildID := getExtraFieldString(saved[2].ExtraFields, MessageIDKey)

		err := db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{rootID},
			Recursive:  true,
		})
		if err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		// All three should be gone
		for _, id := range []string{rootID, childID, grandchildID} {
			_, err = db.GetMessages(ctx, []string{id})
			if err == nil {
				t.Errorf("message %s should be deleted", id)
			}
		}
	})

	t.Run("delete non-existent ID", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Should not panic; may or may not return error depending on implementation
		_ = db.DeleteMessages(ctx, DeleteMessagesOptions{
			MessageIDs: []string{"nonexistent"},
			Recursive:  false,
		})
	})
}

func TestGetDialogForMessage(t *testing.T) {
	t.Run("full chain from leaf to root", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Save chain: root → msg1 → msg2 → msg3
		msgs := []gai.Message{
			makeTextMessage(gai.User, "root"),
			makeTextMessage(gai.Assistant, "msg1"),
			makeTextMessage(gai.User, "msg2"),
			makeTextMessage(gai.Assistant, "msg3"),
		}
		saved := saveDialog(t, db, ctx, msgs)

		var allIDs []string
		for _, s := range saved {
			allIDs = append(allIDs, getExtraFieldString(s.ExtraFields, MessageIDKey))
		}

		// Get dialog from the last message
		dialog, err := GetDialogForMessage(ctx, db, allIDs[3])
		if err != nil {
			t.Fatalf("GetDialogForMessage: %v", err)
		}

		if len(dialog) != 4 {
			t.Fatalf("expected 4 messages in dialog, got %d", len(dialog))
		}

		// Verify order: root first, msg3 last
		for i, msg := range dialog {
			gotID, ok := msg.ExtraFields[MessageIDKey].(string)
			if !ok || gotID != allIDs[i] {
				t.Errorf("dialog[%d]: expected ID %q, got %q", i, allIDs[i], gotID)
			}
		}
	})

	t.Run("correct parent IDs in dialog", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		msgs := []gai.Message{
			makeTextMessage(gai.User, "msg0"),
			makeTextMessage(gai.Assistant, "msg1"),
			makeTextMessage(gai.User, "msg2"),
		}
		saved := saveDialog(t, db, ctx, msgs)

		var allIDs []string
		for _, s := range saved {
			allIDs = append(allIDs, getExtraFieldString(s.ExtraFields, MessageIDKey))
		}

		dialog, err := GetDialogForMessage(ctx, db, allIDs[2])
		if err != nil {
			t.Fatalf("GetDialogForMessage: %v", err)
		}

		// Root should not have parent
		if _, ok := dialog[0].ExtraFields[MessageParentIDKey]; ok {
			t.Error("root message should not have MessageParentIDKey")
		}

		// Others should have parent
		for i := 1; i < len(dialog); i++ {
			gotParent, ok := dialog[i].ExtraFields[MessageParentIDKey].(string)
			if !ok || gotParent != allIDs[i-1] {
				t.Errorf("dialog[%d]: expected parent %q, got %q", i, allIDs[i-1], gotParent)
			}
		}
	})

	t.Run("root message returns single-element dialog", func(t *testing.T) {
		db := newTestDB(t)
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
		db := newTestDB(t)
		ctx := context.Background()

		_, err := GetDialogForMessage(ctx, db, "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent ID")
		}
	})
}

func TestSaveAndGetRoundTrip(t *testing.T) {
	t.Run("varied block types round-trip", func(t *testing.T) {
		db := newTestDB(t)
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

		// Verify message-level fields
		if got.Role != gai.ToolResult {
			t.Errorf("expected role ToolResult, got %v", got.Role)
		}
		if !got.ToolResultError {
			t.Error("expected ToolResultError = true")
		}

		// Verify blocks
		if len(got.Blocks) != 3 {
			t.Fatalf("expected 3 blocks, got %d", len(got.Blocks))
		}

		// Block 0: text content
		b0 := got.Blocks[0]
		if b0.ID != "block-text" {
			t.Errorf("block[0].ID: expected %q, got %q", "block-text", b0.ID)
		}
		if b0.BlockType != gai.Content {
			t.Errorf("block[0].BlockType: expected %q, got %q", gai.Content, b0.BlockType)
		}
		if b0.ModalityType != gai.Text {
			t.Errorf("block[0].ModalityType: expected %v, got %v", gai.Text, b0.ModalityType)
		}
		if b0.MimeType != "text/plain" {
			t.Errorf("block[0].MimeType: expected %q, got %q", "text/plain", b0.MimeType)
		}
		if b0.Content.String() != "hello world" {
			t.Errorf("block[0].Content: expected %q, got %q", "hello world", b0.Content.String())
		}

		// Block 1: tool call with ExtraFields
		b1 := got.Blocks[1]
		if b1.ID != "block-tool" {
			t.Errorf("block[1].ID: expected %q, got %q", "block-tool", b1.ID)
		}
		if b1.BlockType != gai.ToolCall {
			t.Errorf("block[1].BlockType: expected %q, got %q", gai.ToolCall, b1.BlockType)
		}
		if b1.ExtraFields == nil {
			t.Fatal("block[1].ExtraFields should not be nil")
		}
		if v, ok := b1.ExtraFields["provider"].(string); !ok || v != "test" {
			t.Errorf("block[1].ExtraFields[provider]: expected %q, got %v", "test", b1.ExtraFields["provider"])
		}
		if v, ok := b1.ExtraFields["version"].(float64); !ok || v != 1 {
			t.Errorf("block[1].ExtraFields[version]: expected %v, got %v", float64(1), b1.ExtraFields["version"])
		}

		// Block 2: image
		b2 := got.Blocks[2]
		if b2.ID != "block-image" {
			t.Errorf("block[2].ID: expected %q, got %q", "block-image", b2.ID)
		}
		if b2.ModalityType != gai.Image {
			t.Errorf("block[2].ModalityType: expected %v, got %v", gai.Image, b2.ModalityType)
		}
		if b2.MimeType != "image/png" {
			t.Errorf("block[2].MimeType: expected %q, got %q", "image/png", b2.MimeType)
		}
		if b2.Content.String() != "base64encodeddata" {
			t.Errorf("block[2].Content: expected %q, got %q", "base64encodeddata", b2.Content.String())
		}
	})
}
