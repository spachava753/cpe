package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigIntegration(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test-integration.yaml")

	configContent := `
version: "1.0"

mcpServers:
  editor:
    command: "mcp-server-editor"
    type: stdio
    timeout: 60
    toolFilter: whitelist
    enabledTools:
      - text_edit
      - shell

models:
  - name: "gpt4"
    id: "gpt-4"
    type: "openai"
    api_key_env: "OPENAI_API_KEY"
    context_window: 128000
    max_output: 4096
    input_cost_per_million: 30
    output_cost_per_million: 60
    generationDefaults:
      temperature: 0.7
      maxTokens: 2048

  - name: "sonnet"
    id: "claude-3-5-sonnet-20241022"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
    context_window: 200000
    max_output: 8192
    input_cost_per_million: 3
    output_cost_per_million: 15
    generationDefaults:
      temperature: 0.5
      topP: 0.9

defaults:
  model: "sonnet"
  systemPromptPath: "./examples/agent_instructions.prompt"
  timeout: "5m"
  noStream: false
  generationParams:
    temperature: 1.0
    maxTokens: 4096
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	// Test loading the config
	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify models
	if len(config.Models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(config.Models))
	}

	gpt4, found := config.FindModel("gpt4")
	if !found {
		t.Fatal("Expected to find gpt4 model")
	}

	if gpt4.Type != "openai" || gpt4.ID != "gpt-4" {
		t.Errorf("GPT-4 model not parsed correctly: type=%s, id=%s", gpt4.Type, gpt4.ID)
	}

	// Test generation defaults on GPT-4
	if gpt4.GenerationDefaults == nil {
		t.Fatal("Expected GPT-4 to have generation defaults")
	}
	if gpt4.GenerationDefaults.Temperature == nil || *gpt4.GenerationDefaults.Temperature != 0.7 {
		t.Errorf("Expected GPT-4 temperature 0.7, got %v", gpt4.GenerationDefaults.Temperature)
	}

	// Test sonnet model
	sonnet, found := config.FindModel("sonnet")
	if !found {
		t.Fatal("Expected to find sonnet model")
	}

	// Test effective generation parameters
	cliOverrides := &GenerationParams{
		TopK: intPtr(40),
	}

	effective := sonnet.GetEffectiveGenerationParams(config.Defaults.GenerationParams, cliOverrides)

	// Temperature should come from model defaults (0.5)
	if effective.Temperature == nil || *effective.Temperature != 0.5 {
		t.Errorf("Expected effective temperature 0.5, got %v", effective.Temperature)
	}

	// TopP should come from model defaults (0.9)
	if effective.TopP == nil || *effective.TopP != 0.9 {
		t.Errorf("Expected effective topP 0.9, got %v", effective.TopP)
	}

	// TopK should come from CLI override (40)
	if effective.TopK == nil || *effective.TopK != 40 {
		t.Errorf("Expected effective topK 40, got %v", effective.TopK)
	}

	// MaxTokens should come from global defaults (4096), not model defaults
	if effective.MaxTokens == nil || *effective.MaxTokens != 4096 {
		t.Errorf("Expected effective maxTokens 4096, got %v", effective.MaxTokens)
	}

	// Test MCP servers
	if len(config.MCPServers) != 1 {
		t.Fatalf("Expected 1 MCP server, got %d", len(config.MCPServers))
	}

	editor, exists := config.MCPServers["editor"]
	if !exists {
		t.Fatal("Expected to find 'editor' MCP server")
	}

	if editor.Command != "mcp-server-editor" || editor.Type != "stdio" {
		t.Errorf("MCP server not parsed correctly: command=%s, type=%s", editor.Command, editor.Type)
	}

	// Test defaults
	if config.GetDefaultModel() != "sonnet" {
		t.Errorf("Expected default model 'sonnet', got '%s'", config.GetDefaultModel())
	}

	if config.Defaults.Timeout != "5m" {
		t.Errorf("Expected default timeout '5m', got '%s'", config.Defaults.Timeout)
	}
}

func intPtr(i int) *int {
	return &i
}
