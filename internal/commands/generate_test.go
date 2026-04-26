package commands

import (
	"bytes"
	"context"
	"errors"
	"iter"
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

type failingListResolver struct {
	listErr error
}

func (r failingListResolver) ListMessages(ctx context.Context, opts storage.ListMessagesOptions) (iter.Seq[gai.Message], error) {
	return nil, r.listErr
}

func (r failingListResolver) GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
	return nil, errors.New("not implemented")
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
