package commands

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

func requireAPIKey(t *testing.T, envVar string) {
	t.Helper()
	if os.Getenv(envVar) == "" {
		t.Fatalf("required environment variable %s is not set", envVar)
	}
}

func resolveTestConfig(t *testing.T, modelType string, modelRef string) *config.Config {
	t.Helper()

	var ref, id, apiKeyEnv string
	switch modelType {
	case "anthropic":
		ref = "test-anthropic"
		id = "claude-sonnet-4-20250514"
		apiKeyEnv = "ANTHROPIC_API_KEY"
	case "gemini":
		ref = "test-gemini"
		id = "gemini-2.0-flash"
		apiKeyEnv = "GEMINI_API_KEY"
	default:
		t.Fatalf("unsupported model type: %s", modelType)
	}

	rawCfg := &config.RawConfig{
		Version: "1.0",
		Models: []config.ModelConfig{
			{
				Model: config.Model{
					Ref:         ref,
					DisplayName: "Test Model",
					ID:          id,
					Type:        modelType,
					ApiKeyEnv:   apiKeyEnv,
				},
			},
		},
		Defaults: config.Defaults{
			Model:   ref,
			Timeout: "30s",
		},
	}

	effectiveRef := modelRef
	if effectiveRef == "" {
		effectiveRef = ref
	}

	cfg, err := config.ResolveFromRaw(rawCfg, config.RuntimeOptions{
		ModelRef: effectiveRef,
	})
	if err != nil {
		t.Fatalf("failed to resolve test config: %v", err)
	}
	return cfg
}

func resolveCrossProviderConfig(t *testing.T, modelRef string) *config.Config {
	t.Helper()

	rawCfg := &config.RawConfig{
		Version: "1.0",
		Models: []config.ModelConfig{
			{
				Model: config.Model{
					Ref:         "test-anthropic",
					DisplayName: "Test Anthropic",
					ID:          "claude-sonnet-4-20250514",
					Type:        "anthropic",
					ApiKeyEnv:   "ANTHROPIC_API_KEY",
				},
			},
			{
				Model: config.Model{
					Ref:         "test-gemini",
					DisplayName: "Test Gemini",
					ID:          "gemini-2.0-flash",
					Type:        "gemini",
					ApiKeyEnv:   "GEMINI_API_KEY",
				},
			},
		},
		Defaults: config.Defaults{
			Model:   "test-anthropic",
			Timeout: "30s",
		},
	}

	cfg, err := config.ResolveFromRaw(rawCfg, config.RuntimeOptions{
		ModelRef: modelRef,
	})
	if err != nil {
		t.Fatalf("failed to resolve cross-provider config: %v", err)
	}
	return cfg
}

const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ".cpeconvo")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func countMessages(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages").Scan(&count); err != nil {
		t.Fatalf("failed to count messages: %v", err)
	}
	return count
}

type testMessage struct {
	ID       string
	ParentID sql.NullString
	Role     string
}

func queryMessages(ctx context.Context, t *testing.T, db *sql.DB) []testMessage {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT id, parent_id, role FROM messages ORDER BY created_at")
	if err != nil {
		t.Fatalf("failed to query messages: %v", err)
	}
	defer rows.Close()

	var msgs []testMessage
	for rows.Next() {
		var m testMessage
		if err := rows.Scan(&m.ID, &m.ParentID, &m.Role); err != nil {
			t.Fatalf("failed to scan message: %v", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}
	return msgs
}

func newTestSqliteDB(t *testing.T, ctx context.Context) storage.MessageDB {
	t.Helper()
	db := openTestDB(t)
	sqliteStorage, err := storage.NewSqlite(ctx, db)
	if err != nil {
		t.Fatalf("failed to initialize dialog storage: %v", err)
	}
	return sqliteStorage
}

func TestExecuteRoot_ConversationFlow(t *testing.T) {
	requireAPIKey(t, "ANTHROPIC_API_KEY")
	requireAPIKey(t, "GEMINI_API_KEY")
	t.Chdir(t.TempDir())

	ctx := context.Background()
	db := openTestDB(t)
	messageDB := newTestSqliteDB(t, ctx)

	// Track the first assistant message ID for the fork test later.
	var firstAssistantID string

	t.Run("NewConversation", func(t *testing.T) {
		err := ExecuteRoot(ctx, ExecuteRootOptions{
			Args:            []string{"Reply with exactly one word: hello"},
			NewConversation: true,
			SkipStdin:       true,
			Stdout:          io.Discard,
			Stderr:          io.Discard,
			Config:          resolveCrossProviderConfig(t, "test-anthropic"),
			MessageDB:       messageDB,
		})
		if err != nil {
			t.Fatalf("ExecuteRoot returned error: %v", err)
		}

		count := countMessages(ctx, t, db)
		if count != 2 {
			t.Fatalf("expected 2 messages, got %d", count)
		}

		msgs := queryMessages(ctx, t, db)
		if msgs[0].Role != roleUser {
			t.Errorf("expected first message role %q, got %q", roleUser, msgs[0].Role)
		}
		if msgs[0].ParentID.Valid {
			t.Errorf("expected first message to have no parent, got %q", msgs[0].ParentID.String)
		}
		if msgs[1].Role != roleAssistant {
			t.Errorf("expected second message role %q, got %q", roleAssistant, msgs[1].Role)
		}
		if !msgs[1].ParentID.Valid || msgs[1].ParentID.String != msgs[0].ID {
			t.Errorf("expected second message parent to be %q, got %v", msgs[0].ID, msgs[1].ParentID)
		}

		firstAssistantID = msgs[1].ID
	})

	t.Run("ContinueConversation", func(t *testing.T) {
		// Auto-continues from the most recent assistant message.
		err := ExecuteRoot(ctx, ExecuteRootOptions{
			Args:      []string{"Reply with exactly one word: goodbye"},
			SkipStdin: true,
			Stdout:    io.Discard,
			Stderr:    io.Discard,
			Config:    resolveCrossProviderConfig(t, "test-anthropic"),
			MessageDB: messageDB,
		})
		if err != nil {
			t.Fatalf("ExecuteRoot returned error: %v", err)
		}

		count := countMessages(ctx, t, db)
		if count != 4 {
			t.Fatalf("expected 4 messages, got %d", count)
		}

		msgs := queryMessages(ctx, t, db)

		// Verify linear chain: user1 (no parent) → assistant1 → user2 → assistant2
		if msgs[0].Role != roleUser || msgs[0].ParentID.Valid {
			t.Errorf("msg[0]: expected user with no parent, got role=%q parent=%v", msgs[0].Role, msgs[0].ParentID)
		}
		for i := 1; i < len(msgs); i++ {
			if !msgs[i].ParentID.Valid || msgs[i].ParentID.String != msgs[i-1].ID {
				t.Errorf("msg[%d]: expected parent %q, got %v", i, msgs[i-1].ID, msgs[i].ParentID)
			}
		}
		if msgs[1].Role != roleAssistant {
			t.Errorf("msg[1]: expected role %q, got %q", roleAssistant, msgs[1].Role)
		}
		if msgs[2].Role != roleUser {
			t.Errorf("msg[2]: expected role %q, got %q", roleUser, msgs[2].Role)
		}
		if msgs[3].Role != roleAssistant {
			t.Errorf("msg[3]: expected role %q, got %q", roleAssistant, msgs[3].Role)
		}
	})

	t.Run("ContinueWithDifferentProvider", func(t *testing.T) {
		// Fork from the first assistant message using Gemini.
		// This tests cross-provider continuation and conversation forking.
		if firstAssistantID == "" {
			t.Fatal("firstAssistantID not set — NewConversation subtest must have failed")
		}

		err := ExecuteRoot(ctx, ExecuteRootOptions{
			Args:       []string{"Reply with exactly one word: world"},
			ContinueID: firstAssistantID,
			SkipStdin:  true,
			Stdout:     io.Discard,
			Stderr:     io.Discard,
			Config:     resolveCrossProviderConfig(t, "test-gemini"),
			MessageDB:  messageDB,
		})
		if err != nil {
			t.Fatalf("ExecuteRoot returned error: %v", err)
		}

		// We now have 6 messages total:
		// Original chain: user1 → assistant1 → user2 → assistant2
		// Forked branch:  assistant1 → user3 → assistant3
		count := countMessages(ctx, t, db)
		if count != 6 {
			t.Fatalf("expected 6 messages, got %d", count)
		}

		msgs := queryMessages(ctx, t, db)

		// Find the forked messages: user and assistant whose parent chain
		// goes back to firstAssistantID but are not part of the original chain.
		var forkedUser, forkedAssistant *testMessage
		for i := range msgs {
			if msgs[i].Role == roleUser && msgs[i].ParentID.Valid && msgs[i].ParentID.String == firstAssistantID {
				// Could be the original user2 or the forked user3.
				// The original user2 is msgs[2] from the linear chain.
				// Check if this is a different message by looking at the next one.
				if i >= 4 {
					forkedUser = &msgs[i]
				}
			}
		}
		if forkedUser == nil {
			// Try another approach: the forked user is the one with parent=firstAssistantID
			// that is NOT the third message in the original chain.
			originalUser2ID := msgs[2].ID
			for i := range msgs {
				if msgs[i].Role == roleUser && msgs[i].ParentID.Valid &&
					msgs[i].ParentID.String == firstAssistantID && msgs[i].ID != originalUser2ID {
					forkedUser = &msgs[i]
					break
				}
			}
		}
		if forkedUser == nil {
			t.Fatal("could not find forked user message")
		}

		// Find the forked assistant (child of forkedUser)
		for i := range msgs {
			if msgs[i].Role == roleAssistant && msgs[i].ParentID.Valid &&
				msgs[i].ParentID.String == forkedUser.ID {
				forkedAssistant = &msgs[i]
				break
			}
		}
		if forkedAssistant == nil {
			t.Fatal("could not find forked assistant message")
		}

		// Verify the forked assistant has content blocks
		var blockCount int
		err = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM blocks WHERE message_id = ? AND block_type = 'content'",
			forkedAssistant.ID,
		).Scan(&blockCount)
		if err != nil {
			t.Fatalf("failed to count blocks for forked assistant: %v", err)
		}
		if blockCount == 0 {
			t.Errorf("forked assistant: expected at least one content block, got 0")
		}
	})
}

func TestExecuteRoot_ContextCancellation(t *testing.T) {
	requireAPIKey(t, "ANTHROPIC_API_KEY")
	t.Chdir(t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	messageDB := newTestSqliteDB(t, ctx)

	// Use a prompt designed to produce a long response
	_ = ExecuteRoot(ctx, ExecuteRootOptions{
		Args:            []string{"Write a 2000 word essay about the history of computing"},
		NewConversation: true,
		SkipStdin:       true,
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		Config:          resolveTestConfig(t, "anthropic", ""),
		MessageDB:       messageDB,
	})
	// Error is acceptable (context.Canceled or context.DeadlineExceeded) — don't check it

	db := openTestDB(t)
	count := countMessages(ctx, t, db)
	if count < 1 {
		t.Fatalf("expected at least 1 message (user), got %d", count)
	}
}

func TestExecuteRoot_IncognitoMode(t *testing.T) {
	requireAPIKey(t, "ANTHROPIC_API_KEY")
	t.Chdir(t.TempDir())

	ctx := context.Background()
	err := ExecuteRoot(ctx, ExecuteRootOptions{
		Args:            []string{"Reply with exactly one word: hello"},
		NewConversation: true,
		SkipStdin:       true,
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		Config:          resolveTestConfig(t, "anthropic", ""),
		// MessageDB is nil — no storage (incognito mode)
	})
	if err != nil {
		t.Fatalf("ExecuteRoot returned error: %v", err)
	}

	_, statErr := os.Stat(".cpeconvo")
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected .cpeconvo to not exist, but got err: %v", statErr)
	}
}
