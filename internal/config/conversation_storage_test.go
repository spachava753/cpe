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
		name    string
		rawPath string
		want    string
		wantErr string
	}{
		{
			name: "uses default path when not configured",
			want: DefaultConversationStoragePath,
		},
		{
			name:    "uses absolute configured path",
			rawPath: "/tmp/cpe/conversations.db",
			want:    filepath.Clean("/tmp/cpe/conversations.db"),
		},
		{
			name:    "expands home path",
			rawPath: "~/.config/cpe/conversations.db",
			want:    filepath.Join(homeDir, ".config/cpe/conversations.db"),
		},
		{
			name:    "expands windows-style home path",
			rawPath: `~\Documents\cpe.db`,
			want:    filepath.Join(homeDir, `Documents\cpe.db`),
		},
		{
			name:    "keeps relative path relative to current working directory",
			rawPath: ".history.db",
			want:    filepath.Clean(".history.db"),
		},
		{
			name:    "rejects unsupported tilde format",
			rawPath: "~other/path.db",
			wantErr: "unsupported home path format \"~other/path.db\" (use ~/...)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveConversationStoragePath(tt.rawPath)
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
