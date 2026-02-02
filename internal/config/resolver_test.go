package config

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

func TestResolveFromRaw(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		opts          RuntimeOptions
		wantErr       bool
		errContains   string
	}{
		{
			name: "resolves with explicit model",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
`,
			opts:    RuntimeOptions{ModelRef: "test-model"},
			wantErr: false,
		},
		{
			name: "resolves with default model",
			configContent: `
version: "1.0"
models:
  - ref: "default-model"
    display_name: "Default Model"
    id: "default-id"
    type: "anthropic"
    api_key_env: "API_KEY"
defaults:
  model: default-model
`,
			opts:    RuntimeOptions{},
			wantErr: false,
		},
		{
			name: "error when no model specified and no default",
			configContent: `
version: "1.0"
models:
  - ref: "some-model"
    display_name: "Some Model"
    id: "some-id"
    type: "openai"
    api_key_env: "API_KEY"
`,
			opts:        RuntimeOptions{},
			wantErr:     true,
			errContains: "no model specified",
		},
		{
			name: "error when model not found",
			configContent: `
version: "1.0"
models:
  - ref: "existing-model"
    display_name: "Existing Model"
    id: "existing-id"
    type: "openai"
    api_key_env: "API_KEY"
`,
			opts:        RuntimeOptions{ModelRef: "nonexistent"},
			wantErr:     true,
			errContains: "not found in configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFS := fstest.MapFS{
				"cpe.yaml": &fstest.MapFile{Data: []byte(tt.configContent)},
			}

			file, err := testFS.Open("cpe.yaml")
			if err != nil {
				t.Fatalf("Failed to open test file: %v", err)
			}
			defer file.Close()

			rawCfg, err := loadRawConfigFromFile(file)
			if err != nil {
				t.Fatalf("Failed to load raw config: %v", err)
			}

			cfg, err := ResolveFromRaw(rawCfg, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg == nil {
				t.Fatal("expected config but got nil")
			}

			cupaloy.SnapshotT(t, cfg)
		})
	}
}

func TestResolveSystemPromptPath(t *testing.T) {
	tests := []struct {
		name     string
		model    ModelConfig
		defaults Defaults
		want     string
	}{
		{
			name: "model path takes precedence",
			model: ModelConfig{
				SystemPromptPath: "/model/prompt.md",
			},
			defaults: Defaults{
				SystemPromptPath: "/default/prompt.md",
			},
			want: "/model/prompt.md",
		},
		{
			name:  "falls back to defaults",
			model: ModelConfig{},
			defaults: Defaults{
				SystemPromptPath: "/default/prompt.md",
			},
			want: "/default/prompt.md",
		},
		{
			name:     "empty when neither set",
			model:    ModelConfig{},
			defaults: Defaults{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSystemPromptPath(tt.model, tt.defaults)
			if got != tt.want {
				t.Errorf("resolveSystemPromptPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name        string
		defaults    Defaults
		opts        RuntimeOptions
		want        time.Duration
		wantErr     bool
		errContains string
	}{
		{
			name:     "default timeout when nothing specified",
			defaults: Defaults{},
			opts:     RuntimeOptions{},
			want:     5 * time.Minute,
		},
		{
			name:     "CLI timeout takes precedence",
			defaults: Defaults{Timeout: "1m"},
			opts:     RuntimeOptions{Timeout: "30s"},
			want:     30 * time.Second,
		},
		{
			name:     "defaults timeout when no CLI",
			defaults: Defaults{Timeout: "10m"},
			opts:     RuntimeOptions{},
			want:     10 * time.Minute,
		},
		{
			name:        "invalid CLI timeout",
			defaults:    Defaults{},
			opts:        RuntimeOptions{Timeout: "invalid"},
			wantErr:     true,
			errContains: "invalid timeout value",
		},
		{
			name:        "invalid default timeout",
			defaults:    Defaults{Timeout: "invalid"},
			opts:        RuntimeOptions{},
			wantErr:     true,
			errContains: "invalid default timeout value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveTimeout(tt.defaults, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("resolveTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveCodeMode(t *testing.T) {
	tests := []struct {
		name     string
		model    ModelConfig
		defaults Defaults
		want     *CodeModeConfig
	}{
		{
			name: "model overrides defaults",
			model: ModelConfig{
				CodeMode: &CodeModeConfig{Enabled: true, ExcludedTools: []string{"model_tool"}},
			},
			defaults: Defaults{
				CodeMode: &CodeModeConfig{Enabled: false, ExcludedTools: []string{"default_tool"}},
			},
			want: &CodeModeConfig{Enabled: true, ExcludedTools: []string{"model_tool"}},
		},
		{
			name:  "falls back to defaults",
			model: ModelConfig{},
			defaults: Defaults{
				CodeMode: &CodeModeConfig{Enabled: true},
			},
			want: &CodeModeConfig{Enabled: true},
		},
		{
			name:     "nil when neither set",
			model:    ModelConfig{},
			defaults: Defaults{},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCodeMode(tt.model, tt.defaults)

			if tt.want == nil {
				if got != nil {
					t.Errorf("resolveCodeMode() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("resolveCodeMode() = nil, want non-nil")
			}

			if got.Enabled != tt.want.Enabled {
				t.Errorf("Enabled = %v, want %v", got.Enabled, tt.want.Enabled)
			}

			if len(got.ExcludedTools) != len(tt.want.ExcludedTools) {
				t.Errorf("ExcludedTools len = %d, want %d", len(got.ExcludedTools), len(tt.want.ExcludedTools))
			}
		})
	}
}

func TestResolveGenerationParams(t *testing.T) {
	tests := []struct {
		name     string
		model    ModelConfig
		defaults Defaults
		opts     RuntimeOptions
	}{
		{
			name: "CLI overrides model overrides defaults",
			model: ModelConfig{
				GenerationDefaults: &GenerationParams{Temperature: 0.7, TopP: 0.9},
			},
			defaults: Defaults{
				GenerationParams: &GenerationParams{Temperature: 0.5, TopK: 10},
			},
			opts: RuntimeOptions{
				GenParams: &gai.GenOpts{Temperature: 0.95},
			},
		},
		{
			name: "model overrides defaults",
			model: ModelConfig{
				GenerationDefaults: &GenerationParams{Temperature: 0.8},
			},
			defaults: Defaults{
				GenerationParams: &GenerationParams{Temperature: 0.5, TopP: 0.9},
			},
			opts: RuntimeOptions{},
		},
		{
			name:  "defaults only",
			model: ModelConfig{},
			defaults: Defaults{
				GenerationParams: &GenerationParams{Temperature: 0.6, MaxGenerationTokens: 2048},
			},
			opts: RuntimeOptions{},
		},
		{
			name:     "empty params when nothing set",
			model:    ModelConfig{},
			defaults: Defaults{},
			opts:     RuntimeOptions{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveGenerationParams(tt.model, tt.defaults, tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got == nil {
				t.Fatal("expected non-nil result")
			}

			cupaloy.SnapshotT(t, got)
		})
	}
}
