package storage

import (
	"context"
	"slices"
	"testing"

	"github.com/spachava753/gai"
)

// extraFieldString is a test helper that extracts a string from ExtraFields,
// failing the test if the key is missing or not a string.
func extraFieldString(t *testing.T, msg gai.Message, key string) string {
	t.Helper()
	v, ok := msg.ExtraFields[key].(string)
	if !ok {
		t.Fatalf("expected ExtraFields[%q] to be a string, got %T", key, msg.ExtraFields[key])
	}
	return v
}

func TestMemDB_SaveDialog_NewConversation(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	user := makeTextMessage(gai.User, "hello")
	assistant := makeTextMessage(gai.Assistant, "hi there")

	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user, assistant})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	if len(saved) != 2 {
		t.Fatalf("expected 2 saved messages, got %d", len(saved))
	}

	// First message should be root (no parent).
	userID := extraFieldString(t, saved[0], MessageIDKey)
	if userID == "" {
		t.Fatal("expected user message to have an ID")
	}
	if _, ok := saved[0].ExtraFields[MessageParentIDKey]; ok {
		t.Error("expected user message to have no parent")
	}

	// Second message should have first as parent.
	assistantID := extraFieldString(t, saved[1], MessageIDKey)
	if assistantID == "" {
		t.Fatal("expected assistant message to have an ID")
	}
	parentID := extraFieldString(t, saved[1], MessageParentIDKey)
	if parentID != userID {
		t.Errorf("expected assistant parent to be %q, got %q", userID, parentID)
	}

	// Verify tree structure via Nodes.
	nodes := db.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestMemDB_SaveDialog_ContinueConversation(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	// Save initial dialog.
	user1 := makeTextMessage(gai.User, "hello")
	assistant1 := makeTextMessage(gai.Assistant, "hi")

	var initial []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user1, assistant1})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		initial = append(initial, msg)
	}

	// Continue the conversation by passing existing messages + a new one.
	user2 := makeTextMessage(gai.User, "how are you?")
	continued := append(initial, user2)

	var result []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values(continued)) {
		if err != nil {
			t.Fatalf("SaveDialog continue error: %v", err)
		}
		result = append(result, msg)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	// The new message should be child of assistant1.
	user2ID := extraFieldString(t, result[2], MessageIDKey)
	user2ParentID := extraFieldString(t, result[2], MessageParentIDKey)
	assistantID := extraFieldString(t, initial[1], MessageIDKey)
	if user2ParentID != assistantID {
		t.Errorf("expected user2 parent to be %q, got %q", assistantID, user2ParentID)
	}

	// Verify total nodes.
	nodes := db.Nodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// Find the assistant node and verify it has user2 as child.
	for _, n := range nodes {
		if n.ID == assistantID {
			if len(n.ChildIDs) != 1 || n.ChildIDs[0] != user2ID {
				t.Errorf("expected assistant to have child %q, got %v", user2ID, n.ChildIDs)
			}
		}
	}
}

func TestMemDB_SaveDialog_Fork(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	// Save initial dialog: user1 → assistant1
	user1 := makeTextMessage(gai.User, "hello")
	assistant1 := makeTextMessage(gai.Assistant, "hi")

	var initial []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user1, assistant1})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		initial = append(initial, msg)
	}

	// Fork: continue from assistant1 with a different user message.
	user2a := makeTextMessage(gai.User, "branch A")
	for _, err := range db.SaveDialog(ctx, slices.Values(append(initial, user2a))) {
		if err != nil {
			t.Fatalf("SaveDialog fork A error: %v", err)
		}
	}

	user2b := makeTextMessage(gai.User, "branch B")
	for _, err := range db.SaveDialog(ctx, slices.Values(append(initial, user2b))) {
		if err != nil {
			t.Fatalf("SaveDialog fork B error: %v", err)
		}
	}

	// Verify tree: assistant1 should have 2 children.
	nodes := db.Nodes()
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(nodes))
	}

	assistantID := extraFieldString(t, initial[1], MessageIDKey)
	for _, n := range nodes {
		if n.ID == assistantID {
			if len(n.ChildIDs) != 2 {
				t.Errorf("expected assistant to have 2 children, got %d", len(n.ChildIDs))
			}
		}
	}
}

func TestMemDB_GetMessages(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	user := makeTextMessage(gai.User, "hello")
	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	id := extraFieldString(t, saved[0], MessageIDKey)
	msgs, err := db.GetMessages(ctx, []string{id})
	if err != nil {
		t.Fatalf("GetMessages error: %v", err)
	}

	var got []gai.Message
	for m := range msgs {
		got = append(got, m)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	gotID := extraFieldString(t, got[0], MessageIDKey)
	if gotID != id {
		t.Errorf("expected message ID %q, got %q", id, gotID)
	}
}

func TestMemDB_ListMessages(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	user := makeTextMessage(gai.User, "hello")
	assistant := makeTextMessage(gai.Assistant, "hi")

	for _, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user, assistant})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
	}

	// Default: descending order.
	msgs, err := db.ListMessages(ctx, ListMessagesOptions{})
	if err != nil {
		t.Fatalf("ListMessages error: %v", err)
	}

	var got []gai.Message
	for m := range msgs {
		got = append(got, m)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}

	// Most recent should be first (assistant).
	if got[0].Role != gai.Assistant {
		t.Errorf("expected first message to be assistant (newest), got %v", got[0].Role)
	}
}

func TestMemDB_DeleteMessages(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	user := makeTextMessage(gai.User, "hello")
	assistant := makeTextMessage(gai.Assistant, "hi")

	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user, assistant})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	userID := extraFieldString(t, saved[0], MessageIDKey)
	assistantID := extraFieldString(t, saved[1], MessageIDKey)

	// Deleting parent without recursive should fail.
	err := db.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{userID},
		Recursive:  false,
	})
	if err == nil {
		t.Fatal("expected error deleting parent with children, got nil")
	}

	// Delete leaf (assistant).
	err = db.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{assistantID},
		Recursive:  false,
	})
	if err != nil {
		t.Fatalf("DeleteMessages error: %v", err)
	}

	nodes := db.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node after deletion, got %d", len(nodes))
	}
}

func TestMemDB_DeleteMessages_Recursive(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	user := makeTextMessage(gai.User, "hello")
	assistant := makeTextMessage(gai.Assistant, "hi")
	user2 := makeTextMessage(gai.User, "followup")

	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user, assistant, user2})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	userID := extraFieldString(t, saved[0], MessageIDKey)

	// Recursive delete from root should delete everything.
	err := db.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{userID},
		Recursive:  true,
	})
	if err != nil {
		t.Fatalf("DeleteMessages recursive error: %v", err)
	}

	nodes := db.Nodes()
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes after recursive deletion, got %d", len(nodes))
	}
}

func TestMemDB_GetMessages_NotFound(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	_, err := db.GetMessages(ctx, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent message ID, got nil")
	}
}

func TestMemDB_SaveDialog_InvalidParentChain(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	// Save one message first.
	user := makeTextMessage(gai.User, "hello")
	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	// Create a message that claims to exist but has a wrong parent.
	fakeMsg := makeTextMessage(gai.Assistant, "fake")
	fakeMsg.ExtraFields = map[string]any{
		MessageIDKey: "nonexistent_id",
	}

	var gotErr error
	for _, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{fakeMsg})) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected error for nonexistent claimed message ID, got nil")
	}
}

func TestMemDB_DeleteMessages_Atomic(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	// Save a single message.
	user := makeTextMessage(gai.User, "hello")
	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	userID := extraFieldString(t, saved[0], MessageIDKey)

	// Try to delete [existing, nonexistent] — should fail atomically.
	err := db.DeleteMessages(ctx, DeleteMessagesOptions{
		MessageIDs: []string{userID, "nonexistent"},
		Recursive:  false,
	})
	if err == nil {
		t.Fatal("expected error deleting nonexistent message, got nil")
	}

	// The existing message should NOT have been deleted (atomicity).
	nodes := db.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (atomicity preserved), got %d", len(nodes))
	}
}

func TestMemDB_GetDialogForMessage(t *testing.T) {
	db := NewMemDB()
	ctx := context.Background()

	user := makeTextMessage(gai.User, "hello")
	assistant := makeTextMessage(gai.Assistant, "hi")
	user2 := makeTextMessage(gai.User, "how are you?")

	var saved []gai.Message
	for msg, err := range db.SaveDialog(ctx, slices.Values([]gai.Message{user, assistant, user2})) {
		if err != nil {
			t.Fatalf("SaveDialog error: %v", err)
		}
		saved = append(saved, msg)
	}

	// Get dialog for the last message — should reconstruct full chain.
	lastID := extraFieldString(t, saved[2], MessageIDKey)
	dialog, err := GetDialogForMessage(ctx, db, lastID)
	if err != nil {
		t.Fatalf("GetDialogForMessage error: %v", err)
	}

	if len(dialog) != 3 {
		t.Fatalf("expected 3 messages in dialog, got %d", len(dialog))
	}

	// Verify order: user → assistant → user2
	if dialog[0].Role != gai.User {
		t.Errorf("expected dialog[0] to be user, got %v", dialog[0].Role)
	}
	if dialog[1].Role != gai.Assistant {
		t.Errorf("expected dialog[1] to be assistant, got %v", dialog[1].Role)
	}
	if dialog[2].Role != gai.User {
		t.Errorf("expected dialog[2] to be user, got %v", dialog[2].Role)
	}
}
