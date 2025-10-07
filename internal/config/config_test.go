package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_FindModel(t *testing.T) {
	config := &Config{
		Models: []ModelConfig{
			{
				Model: Model{
					Name: "gpt4",
					ID:   "gpt-4",
					Type: "openai",
				},
			},
			{
				Model: Model{
					Name: "sonnet",
					ID:   "claude-3-5-sonnet-20241022",
					Type: "anthropic",
				},
			},
		},
	}

	// Test finding existing model
	model, found := config.FindModel("gpt4")
	if !found {
		t.Error("Expected to find model 'gpt4'")
	}
	if model.Name != "gpt4" {
		t.Errorf("Expected model name 'gpt4', got '%s'", model.Name)
	}

	// Test not finding non-existent model
	_, found = config.FindModel("nonexistent")
	if found {
		t.Error("Expected not to find model 'nonexistent'")
	}
}

func TestConfig_GetEffectiveGenerationParams(t *testing.T) {
	// Create a model with specific defaults
	temp1 := 0.5
	topP1 := 0.8
	model := ModelConfig{
		GenerationDefaults: &GenerationParams{
			Temperature: &temp1,
			TopP:        &topP1,
		},
	}

	// Create global defaults
	temp2 := 0.7
	maxTokens := 4096
	globalDefaults := &GenerationParams{
		Temperature: &temp2, // This should be overridden by model default
		MaxTokens:   &maxTokens,
	}

	// Create CLI overrides
	topP2 := 0.9
	cliOverrides := &GenerationParams{
		TopP: &topP2, // This should override model default
	}

	effective := model.GetEffectiveGenerationParams(globalDefaults, cliOverrides)

	// Temperature should come from model defaults
	if effective.Temperature == nil || *effective.Temperature != 0.5 {
		t.Errorf("Expected temperature 0.5, got %v", effective.Temperature)
	}

	// TopP should come from CLI override
	if effective.TopP == nil || *effective.TopP != 0.9 {
		t.Errorf("Expected topP 0.9, got %v", effective.TopP)
	}

	// MaxTokens should come from global defaults
	if effective.MaxTokens == nil || *effective.MaxTokens != 4096 {
		t.Errorf("Expected maxTokens 4096, got %v", effective.MaxTokens)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test.yaml")

	configContent := `
version: "1.0"
models:
  - name: "test-model"
    id: "test-id"
    type: "openai"
    context_window: 8192
    max_output: 4096
defaults:
  model: "test-model"
  timeout: "5m"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	// Test loading the config
	file, err := os.Open(configPath)
	if err != nil {
		t.Fatalf("Failed to open config file: %v", err)
	}
	defer file.Close()

	config, err := loadConfigFromFile(file)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify the config was loaded correctly
	if config.Version != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", config.Version)
	}

	if len(config.Models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(config.Models))
	}

	model := config.Models[0]
	if model.Name != "test-model" {
		t.Errorf("Expected model name 'test-model', got '%s'", model.Name)
	}
	if model.Type != "openai" {
		t.Errorf("Expected model type 'openai', got '%s'", model.Type)
	}

	if config.Defaults.Model != "test-model" {
		t.Errorf("Expected default model 'test-model', got '%s'", config.Defaults.Model)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		expectError   bool
		errorContains string
	}{
		{
			name: "valid config",
			config: Config{
				Models: []ModelConfig{
					{
						Model: Model{
							Name: "test",
							ID:   "test-id",
							Type: "openai",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "no models",
			config: Config{
				Models: []ModelConfig{},
			},
			expectError:   true,
			errorContains: "must contain at least one model",
		},
		{
			name: "duplicate model names",
			config: Config{
				Models: []ModelConfig{
					{
						Model: Model{
							Name: "test",
							ID:   "test-id",
							Type: "openai",
						},
					},
					{
						Model: Model{
							Name: "test", // duplicate
							ID:   "test-id2",
							Type: "anthropic",
						},
					},
				},
			},
			expectError:   true,
			errorContains: "duplicate model name: test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %s", err.Error())
				}
			}
		})
	}
}
