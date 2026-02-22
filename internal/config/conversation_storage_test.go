package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConversationStoragePath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to resolve home dir for test: %v", err)
	}

	tests := []struct {
		name           string
		defaults       Defaults
		configFilePath string
		want           string
		wantErr        string
	}{
		{
			name:     "uses default path when not configured",
			defaults: Defaults{},
			want:     DefaultConversationStoragePath,
		},
		{
			name:     "uses absolute configured path",
			defaults: Defaults{ConversationStoragePath: "/tmp/cpe/conversations.db"},
			want:     filepath.Clean("/tmp/cpe/conversations.db"),
		},
		{
			name:     "expands home path",
			defaults: Defaults{ConversationStoragePath: "~/.config/cpe/conversations.db"},
			want:     filepath.Join(homeDir, ".config/cpe/conversations.db"),
		},
		{
			name:     "expands windows-style home path",
			defaults: Defaults{ConversationStoragePath: `~\Documents\cpe.db`},
			want:     filepath.Join(homeDir, `Documents\cpe.db`),
		},
		{
			name:           "resolves relative path against config file directory",
			defaults:       Defaults{ConversationStoragePath: ".history.db"},
			configFilePath: "/tmp/project/cpe.yaml",
			want:           filepath.Clean("/tmp/project/.history.db"),
		},
		{
			name:     "keeps relative path when config file path unknown",
			defaults: Defaults{ConversationStoragePath: ".history.db"},
			want:     filepath.Clean(".history.db"),
		},
		{
			name:     "rejects unsupported tilde format",
			defaults: Defaults{ConversationStoragePath: "~other/path.db"},
			wantErr:  "unsupported home path format \"~other/path.db\" (use ~/...)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveConversationStoragePath(tt.defaults, tt.configFilePath)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("unexpected error: got %q want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected path: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestResolveConfig_ConversationStoragePathUsesConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "cpe.yaml")

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
  conversationStoragePath: .history.db
`
	if err := os.WriteFile(cfgPath, []byte(cfgData), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := ResolveConfig(cfgPath, RuntimeOptions{})
	if err != nil {
		t.Fatalf("ResolveConfig returned error: %v", err)
	}

	want := filepath.Join(cfgDir, ".history.db")
	if cfg.ConversationStoragePath != want {
		t.Fatalf("unexpected conversation storage path: got %q want %q", cfg.ConversationStoragePath, want)
	}
}

func TestResolveFromRaw_DefaultConversationStoragePath(t *testing.T) {
	rawCfg := &RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test",
				ID:            "gpt-4o-mini",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 128000,
				MaxOutput:     16384,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
	}

	cfg, err := ResolveFromRaw(rawCfg, RuntimeOptions{})
	if err != nil {
		t.Fatalf("ResolveFromRaw returned error: %v", err)
	}
	if cfg.ConversationStoragePath != DefaultConversationStoragePath {
		t.Fatalf("unexpected default conversation path: got %q want %q", cfg.ConversationStoragePath, DefaultConversationStoragePath)
	}
}
