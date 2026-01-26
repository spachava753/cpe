package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateModelAuth(t *testing.T) {
	tests := []struct {
		name    string
		model   ModelConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "oauth with anthropic is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "id",
					Type:        "anthropic",
					AuthMethod:  "oauth",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth with openai is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "id",
					Type:        "openai",
					AuthMethod:  "oauth",
					ApiKeyEnv:   "KEY",
				},
			},
			wantErr: true,
			errMsg:  "auth_method 'oauth' is only supported for anthropic provider",
		},
		{
			name: "oauth with gemini is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "id",
					Type:        "gemini",
					AuthMethod:  "oauth",
					ApiKeyEnv:   "KEY",
				},
			},
			wantErr: true,
			errMsg:  "auth_method 'oauth' is only supported for anthropic provider",
		},
		{
			name: "apikey auth method is valid for any provider",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "id",
					Type:        "openai",
					AuthMethod:  "apikey",
					ApiKeyEnv:   "KEY",
				},
			},
			wantErr: false,
		},
		{
			name: "empty auth method is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "id",
					Type:        "openai",
					ApiKeyEnv:   "KEY",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelAuth(tt.model)

			if tt.wantErr {
				if err == nil {
					t.Error("validateModelAuth() expected error, got nil")
					return
				}
				if err.Error() != tt.errMsg {
					t.Errorf("validateModelAuth() error = %q, want %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("validateModelAuth() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateSubagentConfig(t *testing.T) {
	tempDir := t.TempDir()

	validSchemaPath := filepath.Join(tempDir, "valid_schema.json")
	if err := os.WriteFile(validSchemaPath, []byte(`{"type": "object"}`), 0644); err != nil {
		t.Fatalf("failed to create valid schema file: %v", err)
	}

	invalidSchemaPath := filepath.Join(tempDir, "invalid_schema.json")
	if err := os.WriteFile(invalidSchemaPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to create invalid schema file: %v", err)
	}

	tests := []struct {
		name        string
		cfg         RawConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid outputSchemaPath",
			cfg: RawConfig{
				Subagent: &SubagentConfig{
					Name:             "test",
					Description:      "desc",
					OutputSchemaPath: validSchemaPath,
				},
			},
			wantErr: false,
		},
		{
			name: "nonexistent outputSchemaPath",
			cfg: RawConfig{
				Subagent: &SubagentConfig{
					Name:             "test",
					Description:      "desc",
					OutputSchemaPath: "/nonexistent/path.json",
				},
			},
			wantErr:     true,
			errContains: "file does not exist",
		},
		{
			name: "invalid JSON in outputSchemaPath",
			cfg: RawConfig{
				Subagent: &SubagentConfig{
					Name:             "test",
					Description:      "desc",
					OutputSchemaPath: invalidSchemaPath,
				},
			},
			wantErr:     true,
			errContains: "invalid JSON schema",
		},
		{
			name: "empty outputSchemaPath is valid",
			cfg: RawConfig{
				Subagent: &SubagentConfig{
					Name:        "test",
					Description: "desc",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validateSubagentConfig()

			if tt.wantErr {
				if err == nil {
					t.Error("validateSubagentConfig() expected error, got nil")
					return
				}
				if tt.errContains != "" {
					if got := err.Error(); !strings.Contains(got, tt.errContains) {
						t.Errorf("validateSubagentConfig() error = %q, want containing %q", got, tt.errContains)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("validateSubagentConfig() unexpected error: %v", err)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         RawConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid minimal config",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "m", DisplayName: "M", ID: "i", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			wantErr: false,
		},
		{
			name: "missing required model fields fails validation",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "m"}}, // missing DisplayName, ID, Type, ApiKeyEnv
				},
			},
			wantErr:     true,
			errContains: "invalid configuration",
		},
		{
			name: "default model not in models list",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "m", DisplayName: "M", ID: "i", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{Model: "nonexistent"},
			},
			wantErr:     true,
			errContains: "not found in models list",
		},
		{
			name: "valid config with default model",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "m", DisplayName: "M", ID: "i", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{Model: "m"},
			},
			wantErr: false,
		},
		{
			name: "oauth auth method with non-anthropic fails",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "m", DisplayName: "M", ID: "i", Type: "openai", AuthMethod: "oauth", ApiKeyEnv: "KEY"}},
				},
			},
			wantErr:     true,
			errContains: "oauth",
		},
		{
			name: "oauth auth method with anthropic succeeds",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "m", DisplayName: "M", ID: "i", Type: "anthropic", AuthMethod: "oauth"}},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()

			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}
