package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

func ptr[T any](v T) *T { return &v }

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
			GenerationParams: &config.GenerationParams{
				MaxGenerationTokens: ptr(1024),
			},
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
			GenerationParams: &config.GenerationParams{
				MaxGenerationTokens: ptr(1024),
			},
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

// sortedNodes returns the MemDB nodes sorted by creation time (ascending).
func sortedNodes(memDB *storage.MemDB) []storage.MemNode {
	nodes := memDB.Nodes()
	slices.SortFunc(nodes, func(a, b storage.MemNode) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return nodes
}

// loadDialogFromMemDB loads a conversation dialog from MemDB for continuation.
// It finds the most recent assistant message and reconstructs the dialog.
func loadDialogFromMemDB(ctx context.Context, t *testing.T, memDB *storage.MemDB) gai.Dialog {
	t.Helper()
	msgs, err := memDB.ListMessages(ctx, storage.ListMessagesOptions{})
	if err != nil {
		t.Fatalf("failed to list messages: %v", err)
	}
	var continueID string
	for msg := range msgs {
		if msg.Role == gai.Assistant || msg.Role == gai.ToolResult {
			if id, ok := msg.ExtraFields[storage.MessageIDKey].(string); ok && id != "" {
				continueID = id
				break
			}
		}
	}
	if continueID == "" {
		t.Fatal("no assistant message found to continue from")
	}
	dialog, err := storage.GetDialogForMessage(ctx, memDB, continueID)
	if err != nil {
		t.Fatalf("failed to get dialog for message %s: %v", continueID, err)
	}
	return dialog
}

// loadDialogFromMemDBForID loads a conversation dialog from MemDB for a specific message ID.
func loadDialogFromMemDBForID(ctx context.Context, t *testing.T, memDB *storage.MemDB, messageID string) gai.Dialog {
	t.Helper()
	dialog, err := storage.GetDialogForMessage(ctx, memDB, messageID)
	if err != nil {
		t.Fatalf("failed to get dialog for message %s: %v", messageID, err)
	}
	return dialog
}

func TestExecuteRoot_ConversationFlow(t *testing.T) {
	requireAPIKey(t, "ANTHROPIC_API_KEY")
	requireAPIKey(t, "GEMINI_API_KEY")

	ctx := context.Background()
	memDB := storage.NewMemDB()

	// Track the first assistant message ID for the fork test later.
	var firstAssistantID string

	t.Run("NewConversation", func(t *testing.T) {
		err := ExecuteRoot(ctx, ExecuteRootOptions{
			Args:        []string{"Reply with exactly one word: hello"},
			SkipStdin:   true,
			Stdout:      io.Discard,
			Stderr:      io.Discard,
			Config:      resolveCrossProviderConfig(t, "test-anthropic"),
			DialogSaver: memDB,
		})
		if err != nil {
			t.Fatalf("ExecuteRoot returned error: %v", err)
		}

		nodes := sortedNodes(memDB)
		if len(nodes) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(nodes))
		}

		if nodes[0].Role != gai.User {
			t.Errorf("expected first message role %q, got %q", gai.User, nodes[0].Role)
		}
		if nodes[0].ParentID != "" {
			t.Errorf("expected first message to have no parent, got %q", nodes[0].ParentID)
		}
		if nodes[1].Role != gai.Assistant {
			t.Errorf("expected second message role %q, got %q", gai.Assistant, nodes[1].Role)
		}
		if nodes[1].ParentID != nodes[0].ID {
			t.Errorf("expected second message parent to be %q, got %q", nodes[0].ID, nodes[1].ParentID)
		}

		firstAssistantID = nodes[1].ID
	})

	t.Run("ContinueConversation", func(t *testing.T) {
		// Load conversation history from MemDB (auto-continue equivalent)
		initialDialog := loadDialogFromMemDB(ctx, t, memDB)

		err := ExecuteRoot(ctx, ExecuteRootOptions{
			Args:          []string{"Reply with exactly one word: goodbye"},
			SkipStdin:     true,
			Stdout:        io.Discard,
			Stderr:        io.Discard,
			Config:        resolveCrossProviderConfig(t, "test-anthropic"),
			DialogSaver:   memDB,
			InitialDialog: initialDialog,
		})
		if err != nil {
			t.Fatalf("ExecuteRoot returned error: %v", err)
		}

		nodes := sortedNodes(memDB)
		if len(nodes) != 4 {
			t.Fatalf("expected 4 messages, got %d", len(nodes))
		}

		// Verify linear chain: user1 (no parent) → assistant1 → user2 → assistant2
		if nodes[0].Role != gai.User || nodes[0].ParentID != "" {
			t.Errorf("msg[0]: expected user with no parent, got role=%q parent=%q", nodes[0].Role, nodes[0].ParentID)
		}
		for i := 1; i < len(nodes); i++ {
			if nodes[i].ParentID != nodes[i-1].ID {
				t.Errorf("msg[%d]: expected parent %q, got %q", i, nodes[i-1].ID, nodes[i].ParentID)
			}
		}
		if nodes[1].Role != gai.Assistant {
			t.Errorf("msg[1]: expected role %q, got %q", gai.Assistant, nodes[1].Role)
		}
		if nodes[2].Role != gai.User {
			t.Errorf("msg[2]: expected role %q, got %q", gai.User, nodes[2].Role)
		}
		if nodes[3].Role != gai.Assistant {
			t.Errorf("msg[3]: expected role %q, got %q", gai.Assistant, nodes[3].Role)
		}
	})

	t.Run("ContinueWithDifferentProvider", func(t *testing.T) {
		// Fork from the first assistant message using Gemini.
		// This tests cross-provider continuation and conversation forking.
		if firstAssistantID == "" {
			t.Fatal("firstAssistantID not set — NewConversation subtest must have failed")
		}

		// Load dialog up to the first assistant message
		initialDialog := loadDialogFromMemDBForID(ctx, t, memDB, firstAssistantID)

		err := ExecuteRoot(ctx, ExecuteRootOptions{
			Args:          []string{"Reply with exactly one word: world"},
			SkipStdin:     true,
			Stdout:        io.Discard,
			Stderr:        io.Discard,
			Config:        resolveCrossProviderConfig(t, "test-gemini"),
			DialogSaver:   memDB,
			InitialDialog: initialDialog,
		})
		if err != nil {
			t.Fatalf("ExecuteRoot returned error: %v", err)
		}

		// We now have 6 messages total:
		// Original chain: user1 → assistant1 → user2 → assistant2
		// Forked branch:  assistant1 → user3 → assistant3
		nodes := sortedNodes(memDB)
		if len(nodes) != 6 {
			t.Fatalf("expected 6 messages, got %d", len(nodes))
		}

		// Find the forked messages: user and assistant whose parent chain
		// goes back to firstAssistantID but are not part of the original chain.
		var forkedUser, forkedAssistant *storage.MemNode
		// The original user2 is the one at index 2 in sorted order.
		originalUser2ID := nodes[2].ID
		for i := range nodes {
			if nodes[i].Role == gai.User && nodes[i].ParentID == firstAssistantID && nodes[i].ID != originalUser2ID {
				forkedUser = &nodes[i]
				break
			}
		}
		if forkedUser == nil {
			t.Fatal("could not find forked user message")
		}

		// Find the forked assistant (child of forkedUser)
		for i := range nodes {
			if nodes[i].Role == gai.Assistant && nodes[i].ParentID == forkedUser.ID {
				forkedAssistant = &nodes[i]
				break
			}
		}
		if forkedAssistant == nil {
			t.Fatal("could not find forked assistant message")
		}

		// Verify the forked assistant has content blocks
		if len(forkedAssistant.Blocks) == 0 {
			t.Errorf("forked assistant: expected at least one block, got 0")
		}
		hasContent := false
		for _, block := range forkedAssistant.Blocks {
			if block.BlockType == gai.Content {
				hasContent = true
				break
			}
		}
		if !hasContent {
			t.Errorf("forked assistant: expected at least one content block")
		}
	})
}

func TestExecuteRoot_ContextCancellation(t *testing.T) {
	requireAPIKey(t, "ANTHROPIC_API_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	memDB := storage.NewMemDB()

	// Use a prompt designed to produce a long response
	_ = ExecuteRoot(ctx, ExecuteRootOptions{
		Args:        []string{"Write a 2000 word essay about the history of computing"},
		SkipStdin:   true,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Config:      resolveTestConfig(t, "anthropic", ""),
		DialogSaver: memDB,
	})
	// Error is acceptable (context.Canceled or context.DeadlineExceeded) — don't check it

	nodes := memDB.Nodes()
	if len(nodes) < 1 {
		t.Fatalf("expected at least 1 message (user), got %d", len(nodes))
	}
}

func TestExecuteRoot_IncognitoMode(t *testing.T) {
	requireAPIKey(t, "ANTHROPIC_API_KEY")
	t.Chdir(t.TempDir())

	ctx := context.Background()
	err := ExecuteRoot(ctx, ExecuteRootOptions{
		Args:      []string{"Reply with exactly one word: hello"},
		SkipStdin: true,
		Stdout:    io.Discard,
		Stderr:    io.Discard,
		Config:    resolveTestConfig(t, "anthropic", ""),
		// DialogSaver is nil — no storage (incognito mode)
	})
	if err != nil {
		t.Fatalf("ExecuteRoot returned error: %v", err)
	}

	_, statErr := os.Stat(".cpeconvo")
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected .cpeconvo to not exist, but got err: %v", statErr)
	}
}
