package config

import (
	"testing"
	"testing/fstest"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

func TestLoadRawConfigFromFileFormats(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filename string
		wantErr  bool
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
			filename: "config.yaml",
			wantErr:  false,
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
			filename: "config.json",
			wantErr:  false,
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
			filename: "config.yml",
			wantErr:  false,
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
			filename: "config",
			wantErr:  false,
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

			cupaloy.SnapshotT(t, config.Models[0])
		})
	}
}

func TestParseConfigData(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		filename string
		wantErr  bool
	}{
		{
			name: "parse YAML data",
			data: `
version: "1.0"
models:
  - ref: "model"
    display_name: "Model"
    id: "id"
    type: "openai"
`,
			filename: "test.yaml",
			wantErr:  false,
		},
		{
			name:     "parse JSON data",
			data:     `{"version": "1.0", "models": [{"ref": "m", "display_name": "M", "id": "i", "type": "anthropic"}]}`,
			filename: "test.json",
			wantErr:  false,
		},
		{
			name: "unknown extension tries YAML first",
			data: `
version: "1.0"
models:
  - ref: "model"
    display_name: "Model"
    id: "id"
    type: "gemini"
`,
			filename: "test.conf",
			wantErr:  false,
		},
		{
			name:     "unknown extension falls back to JSON",
			data:     `{"version": "1.0", "models": [{"ref": "m", "display_name": "M", "id": "i", "type": "openai"}]}`,
			filename: "test.conf",
			wantErr:  false,
		},
		{
			name:     "invalid data with unknown extension",
			data:     "not valid yaml or json {{{",
			filename: "test.conf",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseConfigData([]byte(tt.data), tt.filename)

			if tt.wantErr {
				if err == nil {
					t.Error("parseConfigData() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parseConfigData() unexpected error: %v", err)
				return
			}

			if config == nil {
				t.Fatal("parseConfigData() returned nil config")
			}

			cupaloy.SnapshotT(t, config)
		})
	}
}

func TestLoadSubagentConfig(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		filename    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid subagent config",
			content: `
version: "1.0"
models:
  - ref: "opus"
    display_name: "Claude Opus"
    id: "claude-opus-4-20250514"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
subagent:
  name: "review_changes"
  description: "Review a diff and return prioritized feedback"
defaults:
  model: opus
`,
			filename: "subagent.yaml",
			wantErr:  false,
		},
		{
			name: "subagent with code mode enabled",
			content: `
version: "1.0"
models:
  - ref: "opus"
    display_name: "Claude Opus"
    id: "claude-opus-4-20250514"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
subagent:
  name: "implement_change"
  description: "Make code changes and run tests"
defaults:
  model: opus
  codeMode:
    enabled: true
`,
			filename: "coder_subagent.yaml",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if config.Subagent == nil {
				t.Fatal("expected subagent to be present")
			}

			cupaloy.SnapshotT(t, config.Subagent)
		})
	}
}

// Tests that were previously in loader_test.go but now use exported ResolveFromRaw
func TestResolveConfig_GenerationDefaultsMerging(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		runtimeOpts   RuntimeOptions
		expectErr     bool
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
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
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
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
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
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
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
				ModelRef:  "test-model",
				GenParams: &gai.GenOpts{Temperature: 0.9},
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
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
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
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
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

			if err := rawCfg.Validate(); err != nil {
				t.Fatalf("Failed to validate raw config: %v", err)
			}

			cfg, err := ResolveFromRaw(rawCfg, tt.runtimeOpts)

			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error but got none")
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

			cupaloy.SnapshotT(t, cfg.GenerationDefaults)
		})
	}
}

func TestResolveConfig_CodeMode(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		runtimeOpts   RuntimeOptions
		expectErr     bool
	}{
		{
			name: "global code mode enabled",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - tool1
      - tool2
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "global code mode disabled",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
defaults:
  codeMode:
    enabled: false
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "model-specific code mode overrides global (enabled)",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    codeMode:
      enabled: true
      excludedTools:
        - model_tool
defaults:
  codeMode:
    enabled: false
    excludedTools:
      - default_tool
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "model-specific code mode overrides global (disabled)",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    codeMode:
      enabled: false
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - default_tool1
      - default_tool2
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "model override completely replaces global (no merging)",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    codeMode:
      enabled: true
      excludedTools:
        - tool_c
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - tool_a
      - tool_b
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "no code mode specified anywhere",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "empty excluded tools list",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    codeMode:
      enabled: true
      excludedTools: []
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
		},
		{
			name: "model with no code mode inherits from defaults",
			configContent: `
version: "1.0"
models:
  - ref: "model1"
    display_name: "Model 1"
    id: "model1-id"
    type: "openai"
    api_key_env: "API_KEY"
  - ref: "model2"
    display_name: "Model 2"
    id: "model2-id"
    type: "anthropic"
    api_key_env: "API_KEY"
    codeMode:
      enabled: false
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - global_tool
`,
			runtimeOpts: RuntimeOptions{ModelRef: "model1"},
		},
		{
			name: "multiple excluded tools",
			configContent: `
version: "1.0"
models:
  - ref: "test-model"
    display_name: "Test Model"
    id: "test-id"
    type: "openai"
    api_key_env: "API_KEY"
    codeMode:
      enabled: true
      excludedTools:
        - tool1
        - tool2
        - tool3
        - tool4
`,
			runtimeOpts: RuntimeOptions{ModelRef: "test-model"},
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

			if err := rawCfg.Validate(); err != nil {
				t.Fatalf("Failed to validate raw config: %v", err)
			}

			cfg, err := ResolveFromRaw(rawCfg, tt.runtimeOpts)

			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg == nil {
				t.Fatal("expected config but got nil")
			}

			cupaloy.SnapshotT(t, cfg.CodeMode)
		})
	}
}
