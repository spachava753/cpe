package cmd

import (
	"os"
	"path/filepath"
	"testing"

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

	got, err := resolveConversationDBPath("")
	if err != nil {
		t.Fatalf("resolveConversationDBPath returned error: %v", err)
	}
	if got != config.DefaultConversationStoragePath {
		t.Fatalf("unexpected db path: got %q want %q", got, config.DefaultConversationStoragePath)
	}
}

func TestResolveConversationDBPath_UsesConfiguredRelativePath(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cpe.yaml")
	cfgData := `version: "1.0"
models:
  - ref: test-model
    display_name: Test Model
    id: gpt-4o-mini
    type: openai
    api_key_env: OPENAI_API_KEY
    context_window: 128000
    max_output: 16384
defaults:
  model: test-model
  conversationStoragePath: .my-cpe.db
`
	if err := os.WriteFile(cfgPath, []byte(cfgData), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	got, err := resolveConversationDBPath(cfgPath)
	if err != nil {
		t.Fatalf("resolveConversationDBPath returned error: %v", err)
	}
	want := filepath.Join(tmpDir, ".my-cpe.db")
	if got != want {
		t.Fatalf("unexpected db path: got %q want %q", got, want)
	}
}

func TestResolveConversationDBPath_ExplicitMissingConfigReturnsError(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := resolveConversationDBPath(missingPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	wantErr := "specified config file does not exist: " + missingPath
	if err.Error() != wantErr {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), wantErr)
	}
}
