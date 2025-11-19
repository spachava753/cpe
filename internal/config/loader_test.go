package config

import (
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"dario.cat/mergo"
	"github.com/spachava753/gai"
)

func TestLoadRawConfigFromFileFormats(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		filename   string
		expectName string
		expectType string
		wantErr    bool
	}{
		{
			name: "YAML config",
			content: `
version: "1.0"
models:
  - ref: "test-yaml"
    display_name: "Test YAML"
    id: "test-yaml-id"
    type: "openai"
    context_window: 8192
    max_output: 4096
`,
			filename:   "config.yaml",
			expectName: "test-yaml",
			expectType: "openai",
			wantErr:    false,
		},
		{
			name: "JSON config",
			content: `{
  "version": "1.0",
  "models": [
    {
      "ref": "test-json",
      "display_name": "Test JSON",
      "id": "test-json-id", 
      "type": "anthropic",
      "context_window": 16384,
      "max_output": 8192
    }
  ]
}`,
			filename:   "config.json",
			expectName: "test-json",
			expectType: "anthropic",
			wantErr:    false,
		},
		{
			name: "YML extension",
			content: `
version: "1.0"
models:
  - ref: "test-yml"
    display_name: "Test YML"
    id: "test-yml-id"
    type: "gemini"
`,
			filename:   "config.yml",
			expectName: "test-yml",
			expectType: "gemini",
			wantErr:    false,
		},
		{
			name: "No extension fallback to YAML",
			content: `
version: "1.0"
models:
  - ref: "test-noext"
    display_name: "Test No Extension"
    id: "test-noext-id"
    type: "groq"
`,
			filename:   "config",
			expectName: "test-noext",
			expectType: "groq",
			wantErr:    false,
		},
		{
			name: "Invalid JSON",
			content: `{
  "version": "1.0"
  "models": [  // missing comma
    {
      "name": "test"
    }
  ]
}`,
			filename: "config.json",
			wantErr:  true,
		},
		{
			name: "Invalid YAML",
			content: `
version: 1.0
models:
  - name: test
    invalid: [unclosed array
`,
			filename: "config.yaml",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test filesystem with the config file
			testFS := fstest.MapFS{
				tt.filename: &fstest.MapFile{
					Data: []byte(tt.content),
				},
			}

			file, err := testFS.Open(tt.filename)
			if err != nil {
				t.Fatalf("Failed to open test file: %v", err)
			}
			defer file.Close()

			config, err := loadRawConfigFromFile(file)

			if tt.wantErr {
				if err == nil {
					t.Errorf("loadRawConfigFromFile() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("loadRawConfigFromFile() unexpected error: %v", err)
				return
			}

			if len(config.Models) == 0 {
				t.Fatal("Expected at least one model")
			}

			model := config.Models[0]
			if model.Ref != tt.expectName {
				t.Errorf("Expected model ref %s, got %s", tt.expectName, model.Ref)
			}

			if model.Type != tt.expectType {
				t.Errorf("Expected model type %s, got %s", tt.expectType, model.Type)
			}
		})
	}
}

func TestResolveConfig_GenerationDefaultsMerging(t *testing.T) {
	tests := []struct {
		name             string
		configContent    string
		runtimeOpts      RuntimeOptions
		validate         func(t *testing.T, cfg *Config)
		expectErr        bool
		expectErrMessage string
	}{
		{
			name: "model-specific generation defaults loaded",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    generationDefaults:
      temperature: 0.7
      topP: 0.9
      maxGenerationTokens: 2048
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.GenerationDefaults.Temperature != 0.7 {
					t.Errorf("expected temperature 0.7, got %f", cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopP != 0.9 {
					t.Errorf("expected topP 0.9, got %f", cfg.GenerationDefaults.TopP)
				}
				if cfg.GenerationDefaults.MaxGenerationTokens != 2048 {
					t.Errorf("expected maxGenerationTokens 2048, got %d", cfg.GenerationDefaults.MaxGenerationTokens)
				}
			},
		},
		{
			name: "global defaults loaded when no model-specific defaults",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
defaults:
  generationParams:
    temperature: 0.5
    topK: 20
    frequencyPenalty: 0.3
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.GenerationDefaults.Temperature != 0.5 {
					t.Errorf("expected temperature 0.5, got %f", cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopK != 20 {
					t.Errorf("expected topK 20, got %d", cfg.GenerationDefaults.TopK)
				}
				if cfg.GenerationDefaults.FrequencyPenalty != 0.3 {
					t.Errorf("expected frequencyPenalty 0.3, got %f", cfg.GenerationDefaults.FrequencyPenalty)
				}
			},
		},
		{
			name: "model-specific defaults override global defaults",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    generationDefaults:
      temperature: 0.8
      topP: 0.95
defaults:
  generationParams:
    temperature: 0.5
    topP: 0.9
    topK: 10
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.GenerationDefaults.Temperature != 0.8 {
					t.Errorf("expected temperature 0.8 (model override), got %f", cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopP != 0.95 {
					t.Errorf("expected topP 0.95 (model override), got %f", cfg.GenerationDefaults.TopP)
				}
				if cfg.GenerationDefaults.TopK != 10 {
					t.Errorf("expected topK 10 (from global), got %d", cfg.GenerationDefaults.TopK)
				}
			},
		},
		{
			name: "CLI overrides take precedence over model-specific defaults",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    generationDefaults:
      temperature: 0.7
      topP: 0.9
defaults:
  generationParams:
    temperature: 0.5
    maxGenerationTokens: 1024
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
				GenParams: &gai.GenOpts{
					Temperature: 0.9,
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.GenerationDefaults.Temperature != 0.9 {
					t.Errorf("expected temperature 0.9 (CLI override), got %f", cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopP != 0.9 {
					t.Errorf("expected topP 0.9 (model default), got %f", cfg.GenerationDefaults.TopP)
				}
				if cfg.GenerationDefaults.MaxGenerationTokens != 1024 {
					t.Errorf("expected maxGenerationTokens 1024 (global default), got %d", cfg.GenerationDefaults.MaxGenerationTokens)
				}
			},
		},
		{
			name: "all three levels merge correctly",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "anthropic"
    api_key_env: "API_KEY"
    generationDefaults:
      temperature: 0.7
      topP: 0.9
defaults:
  generationParams:
    temperature: 0.3
    topP: 0.8
    topK: 15
    maxGenerationTokens: 2048
    thinkingBudget: "medium"
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
				GenParams: &gai.GenOpts{
					Temperature: 0.95,
					TopK:        25,
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.GenerationDefaults.Temperature != 0.95 {
					t.Errorf("expected temperature 0.95 (CLI override), got %f", cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopP != 0.9 {
					t.Errorf("expected topP 0.9 (model default), got %f", cfg.GenerationDefaults.TopP)
				}
				if cfg.GenerationDefaults.TopK != 25 {
					t.Errorf("expected topK 25 (CLI override), got %d", cfg.GenerationDefaults.TopK)
				}
				if cfg.GenerationDefaults.MaxGenerationTokens != 2048 {
					t.Errorf("expected maxGenerationTokens 2048 (global default), got %d", cfg.GenerationDefaults.MaxGenerationTokens)
				}
				if cfg.GenerationDefaults.ThinkingBudget != "medium" {
					t.Errorf("expected thinkingBudget 'medium' (global default), got %s", cfg.GenerationDefaults.ThinkingBudget)
				}
			},
		},
		{
			name: "empty generation defaults result in zero values",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.GenerationDefaults.Temperature != 0 {
					t.Errorf("expected temperature 0 (zero value), got %f", cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopP != 0 {
					t.Errorf("expected topP 0 (zero value), got %f", cfg.GenerationDefaults.TopP)
				}
				if cfg.GenerationDefaults.MaxGenerationTokens != 0 {
					t.Errorf("expected maxGenerationTokens 0 (zero value), got %d", cfg.GenerationDefaults.MaxGenerationTokens)
				}
			},
		},
		{
			name: "complex generation parameters preserved",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    generationDefaults:
      temperature: 0.6
      topP: 0.85
      topK: 40
      maxGenerationTokens: 4096
      frequencyPenalty: 0.5
      presencePenalty: 0.2
      n: 1
      thinkingBudget: "high"
`,
			runtimeOpts: RuntimeOptions{
				ModelRef: "test-model",
			},
			validate: func(t *testing.T, cfg *Config) {
				expected := &gai.GenOpts{
					Temperature:         0.6,
					TopP:                0.85,
					TopK:                40,
					MaxGenerationTokens: 4096,
					FrequencyPenalty:    0.5,
					PresencePenalty:     0.2,
					N:                   1,
					ThinkingBudget:      "high",
				}
				if cfg.GenerationDefaults.Temperature != expected.Temperature {
					t.Errorf("expected temperature %f, got %f", expected.Temperature, cfg.GenerationDefaults.Temperature)
				}
				if cfg.GenerationDefaults.TopP != expected.TopP {
					t.Errorf("expected topP %f, got %f", expected.TopP, cfg.GenerationDefaults.TopP)
				}
				if cfg.GenerationDefaults.TopK != expected.TopK {
					t.Errorf("expected topK %d, got %d", expected.TopK, cfg.GenerationDefaults.TopK)
				}
				if cfg.GenerationDefaults.MaxGenerationTokens != expected.MaxGenerationTokens {
					t.Errorf("expected maxGenerationTokens %d, got %d", expected.MaxGenerationTokens, cfg.GenerationDefaults.MaxGenerationTokens)
				}
				if cfg.GenerationDefaults.FrequencyPenalty != expected.FrequencyPenalty {
					t.Errorf("expected frequencyPenalty %f, got %f", expected.FrequencyPenalty, cfg.GenerationDefaults.FrequencyPenalty)
				}
				if cfg.GenerationDefaults.PresencePenalty != expected.PresencePenalty {
					t.Errorf("expected presencePenalty %f, got %f", expected.PresencePenalty, cfg.GenerationDefaults.PresencePenalty)
				}
				if cfg.GenerationDefaults.N != expected.N {
					t.Errorf("expected n %d, got %d", expected.N, cfg.GenerationDefaults.N)
				}
				if cfg.GenerationDefaults.ThinkingBudget != expected.ThinkingBudget {
					t.Errorf("expected thinkingBudget %s, got %s", expected.ThinkingBudget, cfg.GenerationDefaults.ThinkingBudget)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFS := fstest.MapFS{
				"cpe.yaml": &fstest.MapFile{
					Data: []byte(tt.configContent),
				},
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

			if err := rawCfg.Validate(); err != nil {
				t.Fatalf("Failed to validate raw config: %v", err)
			}

			cfg, err := resolveConfigFromRaw(rawCfg, tt.runtimeOpts)

			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				if tt.expectErrMessage != "" && err.Error() != tt.expectErrMessage {
					t.Errorf("expected error message %q, got %q", tt.expectErrMessage, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg == nil {
				t.Fatal("expected config but got nil")
			}

			if cfg.GenerationDefaults == nil {
				t.Fatal("expected GenerationDefaults but got nil")
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

// resolveConfigFromRaw is a helper for testing that resolves config without file I/O
func resolveConfigFromRaw(rawCfg *RawConfig, opts RuntimeOptions) (*Config, error) {
	modelRef := opts.ModelRef
	if modelRef == "" {
		if rawCfg.Defaults.Model != "" {
			modelRef = rawCfg.Defaults.Model
		} else {
			return nil, fmt.Errorf("no model specified. Set CPE_MODEL environment variable, use --model flag, or set defaults.model in configuration")
		}
	}

	selectedModel, found := rawCfg.FindModel(modelRef)
	if !found {
		return nil, fmt.Errorf("model %q not found in configuration", modelRef)
	}

	systemPromptPath := selectedModel.SystemPromptPath
	if systemPromptPath == "" {
		systemPromptPath = rawCfg.Defaults.SystemPromptPath
	}

	genParams := &gai.GenOpts{}

	if rawCfg.Defaults.GenerationParams != nil {
		globalGenOpts := rawCfg.Defaults.GenerationParams.ToGenOpts()
		if err := mergo.Merge(genParams, globalGenOpts, mergo.WithOverride); err != nil {
			return nil, err
		}
	}

	if selectedModel.GenerationDefaults != nil {
		modelGenOpts := selectedModel.GenerationDefaults.ToGenOpts()
		if err := mergo.Merge(genParams, modelGenOpts, mergo.WithOverride); err != nil {
			return nil, err
		}
	}

	if opts.GenParams != nil {
		if err := mergo.Merge(genParams, opts.GenParams, mergo.WithOverride); err != nil {
			return nil, err
		}
	}

	timeout := 5 * time.Minute
	if opts.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(opts.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout value %q: %w", opts.Timeout, err)
		}
		timeout = parsedTimeout
	} else if rawCfg.Defaults.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(rawCfg.Defaults.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid default timeout value %q: %w", rawCfg.Defaults.Timeout, err)
		}
		timeout = parsedTimeout
	}

	noStream := false
	if opts.NoStream != nil {
		noStream = *opts.NoStream
	} else {
		noStream = rawCfg.Defaults.NoStream
	}

	return &Config{
		MCPServers:         rawCfg.MCPServers,
		Model:              selectedModel.Model,
		SystemPromptPath:   systemPromptPath,
		GenerationDefaults: genParams,
		Timeout:            timeout,
		NoStream:           noStream,
	}, nil
}
