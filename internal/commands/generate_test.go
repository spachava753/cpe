package commands

import (
	"context"
	"slices"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

// seedMemDB populates a MemDB with the given messages and returns the saved
// message IDs in order. Each message is saved as part of a single dialog chain.
func seedMemDB(t *testing.T, ctx context.Context, db *storage.MemDB, msgs []gai.Message) []string {
	t.Helper()
	var ids []string
	for savedMsg, err := range db.SaveDialog(ctx, slices.Values(msgs)) {
		if err != nil {
			t.Fatalf("failed to seed MemDB: %v", err)
		}
		id, ok := savedMsg.ExtraFields[storage.MessageIDKey].(string)
		if !ok || id == "" {
			t.Fatal("seeded message missing ID")
		}
		ids = append(ids, id)
	}
	return ids
}

func TestResolveInitialDialog(t *testing.T) {
	ctx := context.Background()

	t.Run("NewConversation", func(t *testing.T) {
		db := storage.NewMemDB()
		// Seed some data — should be ignored when newConversation is true
		seedMemDB(t, ctx, db, []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("hello")}},
		})

		dialog, err := ResolveInitialDialog(ctx, db, "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dialog != nil {
			t.Fatalf("expected nil dialog for new conversation, got %d messages", len(dialog))
		}
	})

	t.Run("EmptyDatabase", func(t *testing.T) {
		db := storage.NewMemDB()

		dialog, err := ResolveInitialDialog(ctx, db, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dialog != nil {
			t.Fatalf("expected nil dialog for empty database, got %d messages", len(dialog))
		}
	})

	t.Run("AutoContinueFindsAssistant", func(t *testing.T) {
		db := storage.NewMemDB()
		seedMemDB(t, ctx, db, []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("hello")}},
		})

		dialog, err := ResolveInitialDialog(ctx, db, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dialog) != 2 {
			t.Fatalf("expected 2 messages in dialog, got %d", len(dialog))
		}
		if dialog[0].Role != gai.User {
			t.Errorf("expected first message role %q, got %q", gai.User, dialog[0].Role)
		}
		if dialog[1].Role != gai.Assistant {
			t.Errorf("expected second message role %q, got %q", gai.Assistant, dialog[1].Role)
		}
	})

	t.Run("AutoContinueFindsToolResult", func(t *testing.T) {
		db := storage.NewMemDB()
		seedMemDB(t, ctx, db, []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}},
			{Role: gai.ToolResult, Blocks: []gai.Block{gai.TextBlock("result")}},
		})

		dialog, err := ResolveInitialDialog(ctx, db, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dialog) != 2 {
			t.Fatalf("expected 2 messages in dialog, got %d", len(dialog))
		}
		if dialog[1].Role != gai.ToolResult {
			t.Errorf("expected second message role %q, got %q", gai.ToolResult, dialog[1].Role)
		}
	})

	t.Run("ExplicitContinueID", func(t *testing.T) {
		db := storage.NewMemDB()
		ids := seedMemDB(t, ctx, db, []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("first")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("response1")}},
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("second")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("response2")}},
		})

		// Continue from the first assistant message (ids[1]), not the latest
		dialog, err := ResolveInitialDialog(ctx, db, ids[1], false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dialog) != 2 {
			t.Fatalf("expected 2 messages in dialog (up to first assistant), got %d", len(dialog))
		}
		if dialog[0].Role != gai.User {
			t.Errorf("expected first message role %q, got %q", gai.User, dialog[0].Role)
		}
		if dialog[1].Role != gai.Assistant {
			t.Errorf("expected second message role %q, got %q", gai.Assistant, dialog[1].Role)
		}
	})

	t.Run("NonExistentContinueID", func(t *testing.T) {
		db := storage.NewMemDB()

		_, err := ResolveInitialDialog(ctx, db, "nonexistent", false)
		if err == nil {
			t.Fatal("expected error for non-existent continue ID, got nil")
		}
	})

	t.Run("OnlyUserMessages", func(t *testing.T) {
		db := storage.NewMemDB()
		seedMemDB(t, ctx, db, []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}},
		})

		// No assistant or tool_result messages — should start new
		dialog, err := ResolveInitialDialog(ctx, db, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dialog != nil {
			t.Fatalf("expected nil dialog when no assistant messages exist, got %d messages", len(dialog))
		}
	})
}
