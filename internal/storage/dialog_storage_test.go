package storage

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/gai"
)

func newTestDB(t *testing.T) *DialogStorage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ds, err := InitDialogStorage(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("InitDialogStorage: %v", err)
	}
	t.Cleanup(func() { ds.Close() })
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

func TestSaveMessages(t *testing.T) {
	t.Run("single root message", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "hello"), ParentID: "", Title: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var gotID string
		for id := range ids {
			gotID = id
		}
		if gotID == "" {
			t.Fatal("expected non-empty ID")
		}
	})

	t.Run("multiple messages in one call", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		opts := []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "msg1"), ParentID: ""},
			{Message: makeTextMessage(gai.User, "msg2"), ParentID: ""},
			{Message: makeTextMessage(gai.User, "msg3"), ParentID: ""},
		}
		ids, err := db.SaveMessages(ctx, opts)
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var gotIDs []string
		for id := range ids {
			gotIDs = append(gotIDs, id)
		}
		if len(gotIDs) != 3 {
			t.Fatalf("expected 3 IDs, got %d", len(gotIDs))
		}
		// Verify all IDs are unique
		seen := make(map[string]bool)
		for _, id := range gotIDs {
			if id == "" {
				t.Fatal("expected non-empty ID")
			}
			if seen[id] {
				t.Fatalf("duplicate ID: %s", id)
			}
			seen[id] = true
		}
	})

	t.Run("message with parent", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		// Save parent
		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "parent"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages (parent): %v", err)
		}
		var parentID string
		for id := range ids {
			parentID = id
		}

		// Save child
		ids, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.Assistant, "child"), ParentID: parentID},
		})
		if err != nil {
			t.Fatalf("SaveMessages (child): %v", err)
		}
		var childID string
		for id := range ids {
			childID = id
		}
		if childID == "" {
			t.Fatal("expected non-empty child ID")
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

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: msg, ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var savedID string
		for id := range ids {
			savedID = id
		}

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

	t.Run("message with title", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "titled"), ParentID: "", Title: "subagent:test:run1"},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var gotID string
		for id := range ids {
			gotID = id
		}
		if gotID == "" {
			t.Fatal("expected non-empty ID")
		}
	})

	t.Run("atomicity positive case", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		opts := []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "atom1"), ParentID: ""},
			{Message: makeTextMessage(gai.User, "atom2"), ParentID: ""},
			{Message: makeTextMessage(gai.User, "atom3"), ParentID: ""},
		}
		ids, err := db.SaveMessages(ctx, opts)
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var savedIDs []string
		for id := range ids {
			savedIDs = append(savedIDs, id)
		}

		// Verify all 3 can be retrieved
		msgs, err := db.GetMessages(ctx, savedIDs)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		count := 0
		for range msgs {
			count++
		}
		if count != 3 {
			t.Fatalf("expected 3 messages, got %d", count)
		}
	})
}

func TestGetMessages(t *testing.T) {
	t.Run("retrieve by ID with correct ExtraFields", func(t *testing.T) {
		db := newTestDB(t)
		ctx := context.Background()

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "hello"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var savedID string
		for id := range ids {
			savedID = id
		}

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

		// Save parent
		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "parent"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages (parent): %v", err)
		}
		var parentID string
		for id := range ids {
			parentID = id
		}

		// Save child
		ids, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.Assistant, "child"), ParentID: parentID},
		})
		if err != nil {
			t.Fatalf("SaveMessages (child): %v", err)
		}
		var childID string
		for id := range ids {
			childID = id
		}

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
		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: msg, ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var savedID string
		for id := range ids {
			savedID = id
		}

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
		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: msg, ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var savedID string
		for id := range ids {
			savedID = id
		}

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

		// Save messages sequentially (each gets a later created_at)
		var savedIDs []string
		for i := 0; i < 3; i++ {
			ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
				{Message: makeTextMessage(gai.User, "msg"), ParentID: ""},
			})
			if err != nil {
				t.Fatalf("SaveMessages: %v", err)
			}
			for id := range ids {
				savedIDs = append(savedIDs, id)
			}
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
			ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
				{Message: makeTextMessage(gai.User, "msg"), ParentID: ""},
			})
			if err != nil {
				t.Fatalf("SaveMessages: %v", err)
			}
			for id := range ids {
				savedIDs = append(savedIDs, id)
			}
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
			_, err := db.SaveMessages(ctx, []SaveMessageOptions{
				{Message: makeTextMessage(gai.User, "msg"), ParentID: ""},
			})
			if err != nil {
				t.Fatalf("SaveMessages: %v", err)
			}
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

		// Save parent
		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "parent"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var parentID string
		for id := range ids {
			parentID = id
		}

		// Save child
		_, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.Assistant, "child"), ParentID: parentID},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}

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

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "leaf"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var leafID string
		for id := range ids {
			leafID = id
		}

		err = db.DeleteMessages(ctx, DeleteMessagesOptions{
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

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "parent"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages (parent): %v", err)
		}
		var parentID string
		for id := range ids {
			parentID = id
		}

		ids, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.Assistant, "child"), ParentID: parentID},
		})
		if err != nil {
			t.Fatalf("SaveMessages (child): %v", err)
		}
		var childID string
		for id := range ids {
			childID = id
		}

		err = db.DeleteMessages(ctx, DeleteMessagesOptions{
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

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "parent"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages (parent): %v", err)
		}
		var parentID string
		for id := range ids {
			parentID = id
		}

		ids, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.Assistant, "child"), ParentID: parentID},
		})
		if err != nil {
			t.Fatalf("SaveMessages (child): %v", err)
		}
		var childID string
		for id := range ids {
			childID = id
		}

		err = db.DeleteMessages(ctx, DeleteMessagesOptions{
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
		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "root"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages (root): %v", err)
		}
		var rootID string
		for id := range ids {
			rootID = id
		}

		ids, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.Assistant, "child"), ParentID: rootID},
		})
		if err != nil {
			t.Fatalf("SaveMessages (child): %v", err)
		}
		var childID string
		for id := range ids {
			childID = id
		}

		ids, err = db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "grandchild"), ParentID: childID},
		})
		if err != nil {
			t.Fatalf("SaveMessages (grandchild): %v", err)
		}
		var grandchildID string
		for id := range ids {
			grandchildID = id
		}

		err = db.DeleteMessages(ctx, DeleteMessagesOptions{
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
		var prevID string
		var allIDs []string
		roles := []gai.Role{gai.User, gai.Assistant, gai.User, gai.Assistant}
		texts := []string{"root", "msg1", "msg2", "msg3"}

		for i := 0; i < 4; i++ {
			ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
				{Message: makeTextMessage(roles[i], texts[i]), ParentID: prevID},
			})
			if err != nil {
				t.Fatalf("SaveMessages %d: %v", i, err)
			}
			for id := range ids {
				allIDs = append(allIDs, id)
				prevID = id
			}
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

		var prevID string
		var allIDs []string
		for i := 0; i < 3; i++ {
			ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
				{Message: makeTextMessage(gai.User, "msg"), ParentID: prevID},
			})
			if err != nil {
				t.Fatalf("SaveMessages %d: %v", i, err)
			}
			for id := range ids {
				allIDs = append(allIDs, id)
				prevID = id
			}
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

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: makeTextMessage(gai.User, "root"), ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var rootID string
		for id := range ids {
			rootID = id
		}

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

		ids, err := db.SaveMessages(ctx, []SaveMessageOptions{
			{Message: msg, ParentID: ""},
		})
		if err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
		var savedID string
		for id := range ids {
			savedID = id
		}

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
