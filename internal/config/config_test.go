package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spachava753/gai"
)

func TestRawConfig_FindModel(t *testing.T) {
	cfg := &RawConfig{
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

func TestLoadRawConfigFromFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test.yaml")
	content := `
version: "1.0"
models:
  - ref: "model"
    display_name: "Model"
    id: "id"
    type: "openai"
    api_key_env: "API_KEY"
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

	cfg, err := loadRawConfigFromFile(file)
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

func TestRawConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       RawConfig
		expectErr bool
	}{
		{
			name: "valid basic config",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{
							Ref:         "test",
							DisplayName: "Test Model",
							ID:          "id",
							Type:        "openai",
							ApiKeyEnv:   "API_KEY_ENV",
						},
					},
				},
			},
		},
		{
			name:      "missing models",
			cfg:       RawConfig{Models: []ModelConfig{}},
			expectErr: true,
		},
		{
			name: "duplicate refs",
			cfg: RawConfig{Models: []ModelConfig{
				{Model: Model{Ref: "test", DisplayName: "Test Model 1", ID: "id1", Type: "openai", ApiKeyEnv: "KEY1"}},
				{Model: Model{Ref: "test", DisplayName: "Test Model 2", ID: "id2", Type: "anthropic", ApiKeyEnv: "KEY2"}},
			}},
			expectErr: true,
		},
		{
			name: "valid default model",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "gpt4", DisplayName: "GPT-4", ID: "gpt-4", Type: "openai", ApiKeyEnv: "OPENAI_API_KEY"}},
					{Model: Model{Ref: "sonnet", DisplayName: "Sonnet", ID: "sonnet", Type: "anthropic", ApiKeyEnv: "ANTHROPIC_API_KEY"}},
				},
				Defaults: Defaults{Model: "gpt4"},
			},
		},
		{
			name: "invalid default model",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "gpt4", DisplayName: "GPT-4", ID: "gpt-4", Type: "openai", ApiKeyEnv: "OPENAI_API_KEY"}},
					{Model: Model{Ref: "sonnet", DisplayName: "Sonnet", ID: "sonnet", Type: "anthropic", ApiKeyEnv: "ANTHROPIC_API_KEY"}},
				},
				Defaults: Defaults{Model: "invalid-model"},
			},
			expectErr: true,
		},
		{
			name: "empty default model is valid",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "gpt4", DisplayName: "GPT-4", ID: "gpt-4", Type: "openai", ApiKeyEnv: "OPENAI_API_KEY"}},
				},
				Defaults: Defaults{Model: ""},
			},
		},
		{
			name: "missing required ref",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required display_name",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required id",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required api_key_env",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai"}},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "invalid-type", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "valid openai type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid anthropic type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "anthropic", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid gemini type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "gemini", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid groq type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "groq", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid cerebras type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "cerebras", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid openrouter type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openrouter", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid responses type",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "responses", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid https base_url",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", BaseUrl: "https://api.example.com"}},
				},
			},
		},
		{
			name: "valid http base_url",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", BaseUrl: "http://localhost:8080"}},
				},
			},
		},
		{
			name: "invalid base_url",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", BaseUrl: "not-a-url"}},
				},
			},
			expectErr: true,
		},
		{
			name: "valid context_window",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", ContextWindow: 8192}},
				},
			},
		},
		{
			name: "zero context_window is valid (omitempty)",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", ContextWindow: 0}},
				},
			},
		},
		{
			name: "valid max_output",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", MaxOutput: 4096}},
				},
			},
		},
		{
			name: "valid generation params - temperature",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							Temperature: 0.5,
						},
					},
				},
			},
		},
		{
			name: "invalid temperature too high",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							Temperature: 2.5,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid temperature negative",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							Temperature: -0.1,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid topP",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							TopP: 0.8,
						},
					},
				},
			},
		},
		{
			name: "invalid topP too high",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							TopP: 1.1,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid topP negative",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							TopP: -0.1,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid topK",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							TopK: 10,
						},
					},
				},
			},
		},
		{
			name: "valid MaxGenerationTokens",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							MaxGenerationTokens: 2048,
						},
					},
				},
			},
		},
		{
			name: "invalid MaxGenerationTokens negative",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							MaxGenerationTokens: -1,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid thinkingBudget minimal",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							ThinkingBudget: "minimal",
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget low",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							ThinkingBudget: "low",
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget medium",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							ThinkingBudget: "medium",
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget high",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							ThinkingBudget: "high",
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget number",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "anthropic", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							ThinkingBudget: "30000",
						},
					},
				},
			},
		},
		{
			name: "invalid thinkingBudget",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							ThinkingBudget: "invalid",
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid frequencyPenalty",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							FrequencyPenalty: 0.5,
						},
					},
				},
			},
		},
		{
			name: "invalid frequencyPenalty too high",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							FrequencyPenalty: 2.5,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid frequencyPenalty too low",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							FrequencyPenalty: -2.5,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid presencePenalty",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							PresencePenalty: 0.3,
						},
					},
				},
			},
		},
		{
			name: "invalid presencePenalty too high",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							PresencePenalty: 2.5,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid presencePenalty too low",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							PresencePenalty: -2.5,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid numberOfResponses",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							N: 1,
						},
					},
				},
			},
		},
		{
			name: "invalid numberOfResponses too high",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							N: 3,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "all valid generation params",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &gai.GenOpts{
							Temperature:         0.5,
							TopP:                0.8,
							TopK:                10,
							MaxGenerationTokens: 2048,
							ThinkingBudget:      "medium",
							FrequencyPenalty:    0.5,
							PresencePenalty:     0.3,
							N:                   1,
						},
					},
				},
			},
		},
		{
			name: "defaults with valid generation params",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					GenerationParams: &gai.GenOpts{
						Temperature: 0.5,
						TopP:        0.8,
					},
				},
			},
		},
		{
			name: "defaults with invalid temperature",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					GenerationParams: &gai.GenOpts{
						Temperature: 3.0,
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid systemPromptPath in defaults",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					SystemPromptPath: "prompts/system.txt",
				},
			},
		},
		{
			name: "empty systemPromptPath in defaults is valid",
			cfg: RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					SystemPromptPath: "",
				},
			},
		},
		{
			name: "valid systemPromptPath in model",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model:            Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						SystemPromptPath: "prompts/model-specific.txt",
					},
				},
			},
		},
		{
			name: "empty systemPromptPath in model is valid",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model:            Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						SystemPromptPath: "",
					},
				},
			},
		},
		{
			name: "systemPromptPath in both model and defaults",
			cfg: RawConfig{
				Models: []ModelConfig{
					{
						Model:            Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						SystemPromptPath: "prompts/model.txt",
					},
				},
				Defaults: Defaults{
					SystemPromptPath: "prompts/default.txt",
				},
			},
			expectErr: false,
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
			}
		})
	}
}
