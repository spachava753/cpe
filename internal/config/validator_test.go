package config

import (
	"strings"
	"testing"
)

func TestValidateModelAuth(t *testing.T) {
	tests := []struct {
		name      string
		model     ModelConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name: "apikey auth for openai is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-4",
					Type:        "openai",
					ApiKeyEnv:   "OPENAI_API_KEY",
					AuthMethod:  "apikey",
				},
			},
			wantErr: false,
		},
		{
			name: "no auth_method is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-4",
					Type:        "openai",
					ApiKeyEnv:   "OPENAI_API_KEY",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth for anthropic is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "claude-sonnet",
					Type:        "anthropic",
					AuthMethod:  "oauth",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth for responses is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-5.2-codex",
					Type:        "responses",
					AuthMethod:  "oauth",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth for openai type is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-4",
					Type:        "openai",
					AuthMethod:  "oauth",
				},
			},
			wantErr:   true,
			errSubstr: "only supported for anthropic and responses",
		},
		{
			name: "oauth for gemini is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gemini-pro",
					Type:        "gemini",
					AuthMethod:  "oauth",
				},
			},
			wantErr:   true,
			errSubstr: "only supported for anthropic and responses",
		},
		{
			name: "oauth for openrouter is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "some-model",
					Type:        "openrouter",
					AuthMethod:  "oauth",
				},
			},
			wantErr:   true,
			errSubstr: "only supported for anthropic and responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelAuth(tt.model)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
