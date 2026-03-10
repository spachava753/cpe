package commands

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"iter"
	"path/filepath"
	"slices"
	"testing"

	_ "github.com/mattn/go-sqlite3"
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

type failingListResolver struct {
	listErr error
}

func (r failingListResolver) ListMessages(ctx context.Context, opts storage.ListMessagesOptions) (iter.Seq[gai.Message], error) {
	return nil, r.listErr
}

func (r failingListResolver) GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
	return nil, errors.New("not implemented")
}

func newTestSqliteResolver(t *testing.T, ctx context.Context) (*storage.Sqlite, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "generate-test.db")
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { rawDB.Close() })

	sqliteDB, err := storage.NewSqlite(ctx, rawDB)
	if err != nil {
		t.Fatalf("NewSqlite: %v", err)
	}
	return sqliteDB, rawDB
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

	t.Run("AutoContinueListMessagesError", func(t *testing.T) {
		expectedErr := errors.New("list failed")
		_, err := ResolveInitialDialog(ctx, failingListResolver{listErr: expectedErr}, "", false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected wrapped list error %v, got %v", expectedErr, err)
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

	t.Run("AutoContinueIgnoresSubagentMessages", func(t *testing.T) {
		db := storage.NewMemDB()
		regularIDs := seedMemDB(t, ctx, db, []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("parent user")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("parent assistant")}},
		})

		_ = seedMemDB(t, ctx, db, []gai.Message{
			{
				Role:        gai.User,
				Blocks:      []gai.Block{gai.TextBlock("subagent user")},
				ExtraFields: map[string]any{storage.MessageIsSubagentKey: true},
			},
			{
				Role:        gai.Assistant,
				Blocks:      []gai.Block{gai.TextBlock("subagent assistant")},
				ExtraFields: map[string]any{storage.MessageIsSubagentKey: true},
			},
		})

		dialog, err := ResolveInitialDialog(ctx, db, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dialog) != 2 {
			t.Fatalf("expected 2 messages in parent dialog, got %d", len(dialog))
		}
		if dialog[1].Role != gai.Assistant {
			t.Fatalf("expected last dialog message to be assistant, got %q", dialog[1].Role)
		}

		continuedID, ok := dialog[1].ExtraFields[storage.MessageIDKey].(string)
		if !ok || continuedID == "" {
			t.Fatalf("expected assistant message to include ID, got %v", dialog[1].ExtraFields[storage.MessageIDKey])
		}
		if continuedID != regularIDs[1] {
			t.Fatalf("expected auto-continue to pick parent assistant ID %q, got %q", regularIDs[1], continuedID)
		}
		if got, _ := dialog[1].ExtraFields[storage.MessageIsSubagentKey].(bool); got {
			t.Fatal("expected continued message to not be marked as subagent")
		}
	})

	t.Run("AutoContinueWithSqliteTieBreakAndSubagentFilter", func(t *testing.T) {
		db, rawDB := newTestSqliteResolver(t, ctx)

		// Save a regular parent conversation first.
		regular := []gai.Message{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("parent user")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("parent assistant")}},
		}
		var regularAssistantID string
		for msg, err := range db.SaveDialog(ctx, slices.Values(regular)) {
			if err != nil {
				t.Fatalf("SaveDialog regular: %v", err)
			}
			if msg.Role == gai.Assistant {
				regularAssistantID, _ = msg.ExtraFields[storage.MessageIDKey].(string)
			}
		}
		if regularAssistantID == "" {
			t.Fatal("expected regular assistant ID")
		}

		// Save a newer subagent trace that should be ignored by auto-continue.
		subagent := []gai.Message{
			{
				Role:        gai.User,
				Blocks:      []gai.Block{gai.TextBlock("subagent user")},
				ExtraFields: map[string]any{storage.MessageIsSubagentKey: true},
			},
			{
				Role:        gai.Assistant,
				Blocks:      []gai.Block{gai.TextBlock("subagent assistant")},
				ExtraFields: map[string]any{storage.MessageIsSubagentKey: true},
			},
		}
		for _, err := range db.SaveDialog(ctx, slices.Values(subagent)) {
			if err != nil {
				t.Fatalf("SaveDialog subagent: %v", err)
			}
		}

		// Force identical timestamps to exercise deterministic tie-break ordering.
		if _, err := rawDB.ExecContext(ctx, "UPDATE messages SET created_at = '2026-01-01 00:00:00'"); err != nil {
			t.Fatalf("UPDATE created_at: %v", err)
		}

		dialog, err := ResolveInitialDialog(ctx, db, "", false)
		if err != nil {
			t.Fatalf("ResolveInitialDialog: %v", err)
		}
		if len(dialog) != 2 {
			t.Fatalf("expected parent dialog length 2, got %d", len(dialog))
		}

		continuedID, ok := dialog[1].ExtraFields[storage.MessageIDKey].(string)
		if !ok || continuedID == "" {
			t.Fatalf("expected assistant ID in dialog, got %v", dialog[1].ExtraFields[storage.MessageIDKey])
		}
		if continuedID != regularAssistantID {
			t.Fatalf("expected continued ID %q, got %q", regularAssistantID, continuedID)
		}
		if got, _ := dialog[1].ExtraFields[storage.MessageIsSubagentKey].(bool); got {
			t.Fatal("expected continued assistant to be non-subagent")
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

type stubDialogGenerator struct {
	err error
}

func (s stubDialogGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	return dialog, s.err
}

type hintedErr struct {
	cause error
	hint  string
}

func (e hintedErr) Error() string {
	return e.cause.Error()
}

func (e hintedErr) Unwrap() error {
	return e.cause
}

func (e hintedErr) GenerationHint() string {
	return e.hint
}

func TestGenerateFormatsGeneratorErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr string
	}{
		{
			name: "api auth error",
			err: &gai.ApiErr{
				Provider:   gai.ProviderOpenAI,
				Kind:       gai.APIErrorKindAuthentication,
				StatusCode: 401,
				Message:    "invalid_api_key",
			},
			wantErr: "Error generating response: openai authentication (401): invalid_api_key. Check the configured credentials and provider access\n",
		},
		{
			name:    "context length exceeded",
			err:     gai.ContextLengthExceededErr,
			wantErr: "Error generating response: context length exceeded. Shorten the prompt, reduce attached input, or compact the conversation\n",
		},
		{
			name:    "content policy violation",
			err:     gai.ContentPolicyErr("blocked by provider"),
			wantErr: "Error generating response: content policy violation: blocked by provider. Adjust the prompt or inputs to comply with the provider policy\n",
		},
		{
			name: "api auth error with trace hint",
			err: hintedErr{
				cause: &gai.ApiErr{
					Provider:   gai.ProviderOpenAI,
					Kind:       gai.APIErrorKindAuthentication,
					StatusCode: 401,
					Message:    "invalid_api_key",
				},
				hint: "Flight trace saved to /tmp/cpe-generator.trace",
			},
			wantErr: "Error generating response: openai authentication (401): invalid_api_key. Check the configured credentials and provider access. Flight trace saved to /tmp/cpe-generator.trace\n",
		},
		{
			name:    "generic error",
			err:     errors.New("boom"),
			wantErr: "Error generating response: boom\n",
		},
		{
			name: "generic error with trace hint",
			err: hintedErr{
				cause: errors.New("boom"),
				hint:  "Flight trace saved to /tmp/cpe-generator.trace",
			},
			wantErr: "Error generating response: boom. Flight trace saved to /tmp/cpe-generator.trace\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			err := Generate(context.Background(), GenerateOptions{
				UserBlocks: []gai.Block{gai.TextBlock("hello")},
				GenOptsFunc: func(dialog gai.Dialog) *gai.GenOpts {
					return nil
				},
				Generator: stubDialogGenerator{err: tt.err},
				Stderr:    &stderr,
			})
			if err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
			if stderr.String() != tt.wantErr {
				t.Fatalf("stderr mismatch:\n got: %q\nwant: %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestGenerateSuppressesContextCanceled(t *testing.T) {
	var stderr bytes.Buffer
	err := Generate(context.Background(), GenerateOptions{
		UserBlocks: []gai.Block{gai.TextBlock("hello")},
		GenOptsFunc: func(dialog gai.Dialog) *gai.GenOpts {
			return nil
		},
		Generator: stubDialogGenerator{err: context.Canceled},
		Stderr:    &stderr,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}
