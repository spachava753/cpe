package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func TestACPManagementCommandsExecuteAgainstConfiguredStorage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	store, err := storage.NewConvoDB(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("NewConvoDB: %v", err)
	}
	var leafID string
	for message, err := range store.SaveDialog(t.Context(), slices.Values([]gai.Message{{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("persisted prompt")},
	}})) {
		if err != nil {
			t.Fatalf("SaveDialog: %v", err)
		}
		leafID = storage.GetMessageID(message)
	}
	if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acpsdk.SessionInfo{
			Cwd:       "/tmp/project",
			SessionID: "source-session",
			Title:     new("Source title"),
		},
		LastMessageID: leafID,
	}); err != nil {
		t.Fatalf("CreateACPSession: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close setup store: %v", err)
	}

	listOutput := executeRootCommand(t, "acp", "list", "--db-path", dbPath)
	if !strings.Contains(listOutput, "source-session") || !strings.Contains(listOutput, "Source title") {
		t.Fatalf("acp list output:\n%s", listOutput)
	}

	showOutput := executeRootCommand(t, "acp", "show", "source-session", "--db-path", dbPath)
	if !strings.Contains(showOutput, "# Source title") || !strings.Contains(showOutput, "persisted prompt") {
		t.Fatalf("acp show output:\n%s", showOutput)
	}

	forkOutput := executeRootCommand(t, "acp", "fork", "source-session", "--db-path", dbPath)
	forkID := acpsdk.SessionId(strings.TrimSpace(forkOutput))
	if forkID == "" || forkID == "source-session" {
		t.Fatalf("acp fork output = %q", forkOutput)
	}

	executeRootCommand(t, "acp", "delete", string(forkID), "--db-path", dbPath)
	store, err = storage.NewConvoDB(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("reopen conversation database: %v", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.GetACPSession(t.Context(), forkID); !errors.Is(err, storage.ErrSessionNotFound) {
		t.Fatalf("GetACPSession deleted fork error = %v, want ErrSessionNotFound", err)
	}
	if _, err := store.GetACPSession(t.Context(), "source-session"); err != nil {
		t.Fatalf("GetACPSession source after fork delete: %v", err)
	}
}

func executeRootCommand(t *testing.T, args ...string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)
	if err := rootCmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("cpe %s: %v\nstderr:\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}
