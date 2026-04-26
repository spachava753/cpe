package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
)

func TestResolveConversationDBPath_NoConfigFoundUsesDefault(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	got, err := commands.ResolveConversationDBPath("")
	if err != nil {
		t.Fatalf("ResolveConversationDBPath returned error: %v", err)
	}
	if got != config.DefaultConversationStoragePath {
		t.Fatalf("unexpected db path: got %q want %q", got, config.DefaultConversationStoragePath)
	}
}

func TestResolveConversationDBPath_UsesExplicitRelativePath(t *testing.T) {
	got, err := commands.ResolveConversationDBPath(".my-cpe.db")
	if err != nil {
		t.Fatalf("ResolveConversationDBPath returned error: %v", err)
	}
	want := filepath.Clean(".my-cpe.db")
	if got != want {
		t.Fatalf("unexpected db path: got %q want %q", got, want)
	}
}

func TestResolveConversationDBPath_InvalidPathReturnsError(t *testing.T) {
	_, err := commands.ResolveConversationDBPath("~other/path.db")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	wantErr := "failed to resolve conversation storage path: unsupported home path format \"~other/path.db\" (use ~/...)"
	if err.Error() != wantErr {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), wantErr)
	}
}
