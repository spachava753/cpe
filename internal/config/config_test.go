package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_FindModel(t *testing.T) {
	cfg := &Config{
		Models: []ModelConfig{
			{Model: Model{Ref: "gpt4", ID: "gpt-4", Type: "openai"}},
			{Model: Model{Ref: "sonnet", ID: "claude-sonnet", Type: "anthropic"}},
		},
	}

	model, found := cfg.FindModel("gpt4")
	if !found {
		t.Fatalf("expected to find model gpt4")
	}
	if model.Ref != "gpt4" {
		t.Fatalf("expected model ref gpt4, got %s", model.Ref)
	}

	if _, found := cfg.FindModel("missing"); found {
		t.Fatalf("did not expect to find model missing")
	}
}

func TestModelConfig_GetEffectiveSystemPromptPath(t *testing.T) {
	modelWithOverride := &ModelConfig{SystemPromptPath: "model.prompt"}
	modelWithoutOverride := &ModelConfig{}

	tests := []struct {
		name          string
		model         *ModelConfig
		globalDefault string
		cliOverride   string
		expected      string
	}{
		{
			name:          "cli override wins",
			model:         modelWithOverride,
			globalDefault: "global.prompt",
			cliOverride:   "cli.prompt",
			expected:      "cli.prompt",
		},
		{
			name:          "model override wins",
			model:         modelWithOverride,
			globalDefault: "global.prompt",
			cliOverride:   "",
			expected:      "model.prompt",
		},
		{
			name:          "fallback to global",
			model:         modelWithoutOverride,
			globalDefault: "global.prompt",
			cliOverride:   "",
			expected:      "global.prompt",
		},
		{
			name:          "nil model uses global",
			model:         nil,
			globalDefault: "global.prompt",
			cliOverride:   "",
			expected:      "global.prompt",
		},
		{
			name:          "cli wins even with nil model",
			model:         nil,
			globalDefault: "",
			cliOverride:   "cli.prompt",
			expected:      "cli.prompt",
		},
		{
			name:          "all empty",
			model:         nil,
			globalDefault: "",
			cliOverride:   "",
			expected:      "",
		},
	}

	for _, tt := range tests {
		if got := tt.model.GetEffectiveSystemPromptPath(tt.globalDefault, tt.cliOverride); got != tt.expected {
			t.Fatalf("%s: expected %q, got %q", tt.name, tt.expected, got)
		}
	}
}

func TestModelConfig_GetEffectiveGenerationParams(t *testing.T) {
	tempModel := 0.5
	topPModel := 0.8
	tempGlobal := 0.7
	maxTokensGlobal := 4096
	topPCli := 0.9

	model := &ModelConfig{
		GenerationDefaults: &GenerationParams{
			Temperature: &tempModel,
			TopP:        &topPModel,
		},
	}

	global := &GenerationParams{
		Temperature: &tempGlobal,
		MaxTokens:   &maxTokensGlobal,
	}

	cli := &GenerationParams{
		TopP: &topPCli,
	}

	effective := model.GetEffectiveGenerationParams(global, cli)

	if effective.Temperature == nil || *effective.Temperature != tempModel {
		t.Fatalf("expected temperature %v, got %v", tempModel, effective.Temperature)
	}
	if effective.TopP == nil || *effective.TopP != topPCli {
		t.Fatalf("expected topP %v, got %v", topPCli, effective.TopP)
	}
	if effective.MaxTokens == nil || *effective.MaxTokens != maxTokensGlobal {
		t.Fatalf("expected max tokens %d, got %v", maxTokensGlobal, effective.MaxTokens)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test.yaml")
	content := `
version: "1.0"
models:
  - ref: "model"
    display_name: "Model"
    id: "id"
    type: "openai"
defaults:
  model: "model"
`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	file, err := os.Open(configPath)
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	cfg, err := loadConfigFromFile(file)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Version != "1.0" {
		t.Fatalf("expected version 1.0, got %s", cfg.Version)
	}
	if len(cfg.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(cfg.Models))
	}
	if cfg.Models[0].Ref != "model" {
		t.Fatalf("expected model ref 'model', got %s", cfg.Models[0].Ref)
	}
	if cfg.Defaults.Model != "model" {
		t.Fatalf("expected defaults.model 'model', got %s", cfg.Defaults.Model)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name         string
		cfg          Config
		expectErr    bool
		errSubstring string
	}{
		{
			name: "valid",
			cfg:  Config{Models: []ModelConfig{{Model: Model{Ref: "test", DisplayName: "Test Model", ID: "id", Type: "openai"}}}},
		},
		{
			name:         "missing models",
			cfg:          Config{Models: []ModelConfig{}},
			expectErr:    true,
			errSubstring: "at least one model",
		},
		{
			name: "duplicate",
			cfg: Config{Models: []ModelConfig{
				{Model: Model{Ref: "test", DisplayName: "Test Model 1", ID: "id1", Type: "openai"}},
				{Model: Model{Ref: "test", DisplayName: "Test Model 2", ID: "id2", Type: "anthropic"}},
			}},
			expectErr:    true,
			errSubstring: "duplicate model ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if !tt.expectErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
					t.Fatalf("expected error containing %q, got %v", tt.errSubstring, err)
				}
			}
		})
	}
}
