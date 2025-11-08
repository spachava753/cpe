package config

import (
	"os"
	"path/filepath"
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

func ptr[T any](v T) *T {
	return &v
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		expectErr bool
	}{
		{
			name: "valid basic config",
			cfg: Config{
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
			cfg:       Config{Models: []ModelConfig{}},
			expectErr: true,
		},
		{
			name: "duplicate refs",
			cfg: Config{Models: []ModelConfig{
				{Model: Model{Ref: "test", DisplayName: "Test Model 1", ID: "id1", Type: "openai", ApiKeyEnv: "KEY1"}},
				{Model: Model{Ref: "test", DisplayName: "Test Model 2", ID: "id2", Type: "anthropic", ApiKeyEnv: "KEY2"}},
			}},
			expectErr: true,
		},
		{
			name: "valid default model",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "gpt4", DisplayName: "GPT-4", ID: "gpt-4", Type: "openai", ApiKeyEnv: "OPENAI_API_KEY"}},
					{Model: Model{Ref: "sonnet", DisplayName: "Sonnet", ID: "sonnet", Type: "anthropic", ApiKeyEnv: "ANTHROPIC_API_KEY"}},
				},
				Defaults: Defaults{Model: "gpt4"},
			},
		},
		{
			name: "invalid default model",
			cfg: Config{
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
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "gpt4", DisplayName: "GPT-4", ID: "gpt-4", Type: "openai", ApiKeyEnv: "OPENAI_API_KEY"}},
				},
				Defaults: Defaults{Model: ""},
			},
		},
		{
			name: "missing required ref",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required display_name",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required id",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "missing required api_key_env",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai"}},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "invalid-type", ApiKeyEnv: "KEY"}},
				},
			},
			expectErr: true,
		},
		{
			name: "valid openai type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid anthropic type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "anthropic", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid gemini type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "gemini", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid groq type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "groq", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid cerebras type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "cerebras", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid openrouter type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openrouter", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid responses type",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "responses", ApiKeyEnv: "KEY"}},
				},
			},
		},
		{
			name: "valid https base_url",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", BaseUrl: "https://api.example.com"}},
				},
			},
		},
		{
			name: "valid http base_url",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", BaseUrl: "http://localhost:8080"}},
				},
			},
		},
		{
			name: "invalid base_url",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", BaseUrl: "not-a-url"}},
				},
			},
			expectErr: true,
		},
		{
			name: "valid context_window",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", ContextWindow: 8192}},
				},
			},
		},
		{
			name: "zero context_window is valid (omitempty)",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", ContextWindow: 0}},
				},
			},
		},
		{
			name: "valid max_output",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY", MaxOutput: 4096}},
				},
			},
		},
		{
			name: "valid generation params - temperature",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							Temperature: ptr(0.5),
						},
					},
				},
			},
		},
		{
			name: "invalid temperature too high",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							Temperature: ptr(2.5),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid temperature negative",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							Temperature: ptr(-0.1),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid topP",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							TopP: ptr(0.8),
						},
					},
				},
			},
		},
		{
			name: "invalid topP too high",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							TopP: ptr(1.1),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid topP negative",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							TopP: ptr(-0.1),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid topK",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							TopK: ptr(10),
						},
					},
				},
			},
		},
		{
			name: "invalid topK negative",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							TopK: ptr(-1),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid maxTokens",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							MaxTokens: ptr(2048),
						},
					},
				},
			},
		},
		{
			name: "invalid maxTokens negative",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							MaxTokens: ptr(-1),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid thinkingBudget minimal",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							ThinkingBudget: ptr("minimal"),
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget low",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							ThinkingBudget: ptr("low"),
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget medium",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							ThinkingBudget: ptr("medium"),
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget high",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							ThinkingBudget: ptr("high"),
						},
					},
				},
			},
		},
		{
			name: "valid thinkingBudget number",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "anthropic", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							ThinkingBudget: ptr("30000"),
						},
					},
				},
			},
		},
		{
			name: "invalid thinkingBudget",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							ThinkingBudget: ptr("invalid"),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid frequencyPenalty",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							FrequencyPenalty: ptr(0.5),
						},
					},
				},
			},
		},
		{
			name: "invalid frequencyPenalty too high",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							FrequencyPenalty: ptr(2.5),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid frequencyPenalty too low",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							FrequencyPenalty: ptr(-2.5),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid presencePenalty",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							PresencePenalty: ptr(0.3),
						},
					},
				},
			},
		},
		{
			name: "invalid presencePenalty too high",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							PresencePenalty: ptr(2.5),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid presencePenalty too low",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							PresencePenalty: ptr(-2.5),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid numberOfResponses",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							NumberOfResponses: ptr(1),
						},
					},
				},
			},
		},
		{
			name: "invalid numberOfResponses too high",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							NumberOfResponses: ptr(3),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid numberOfResponses negative",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							NumberOfResponses: ptr(-1),
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "all valid generation params",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						GenerationDefaults: &GenerationParams{
							Temperature:       ptr(0.5),
							TopP:              ptr(0.8),
							TopK:              ptr(10),
							MaxTokens:         ptr(2048),
							ThinkingBudget:    ptr("medium"),
							FrequencyPenalty:  ptr(0.5),
							PresencePenalty:   ptr(0.3),
							NumberOfResponses: ptr(1),
						},
					},
				},
			},
		},
		{
			name: "defaults with valid generation params",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					GenerationParams: &GenerationParams{
						Temperature: ptr(0.5),
						TopP:        ptr(0.8),
					},
				},
			},
		},
		{
			name: "defaults with invalid temperature",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					GenerationParams: &GenerationParams{
						Temperature: ptr(3.0),
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid systemPromptPath in defaults",
			cfg: Config{
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
			cfg: Config{
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
			cfg: Config{
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
			cfg: Config{
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
			cfg: Config{
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
		},
		{
			name: "invalid systemPromptPath in defaults",
			cfg: Config{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					SystemPromptPath: "https://api.example.com",
				},
			},
			expectErr: true,
		},
		{
			name: "invalid systemPromptPath in model",
			cfg: Config{
				Models: []ModelConfig{
					{
						Model:            Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						SystemPromptPath: "https://api.example.com"},
				},
			},
			expectErr: true,
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
