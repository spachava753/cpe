package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigIntegration(t *testing.T) {
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
  - ref: "gpt4"
    display_name: "GPT-4"
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

  - ref: "sonnet"
    display_name: "Claude Sonnet"
    id: "claude-3-5-sonnet-20241022"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
    context_window: 200000
    max_output: 8192
    input_cost_per_million: 3
    output_cost_per_million: 15
    systemPromptPath: "./examples/model-specific.prompt"
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

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	gpt4, found := cfg.FindModel("gpt4")
	if !found {
		t.Fatalf("Expected to find gpt4 model")
	}
	if gpt4.Type != "openai" {
		t.Fatalf("unexpected type %s", gpt4.Type)
	}

	sonnet, found := cfg.FindModel("sonnet")
	if !found {
		t.Fatalf("Expected to find sonnet model")
	}

	effectivePath := sonnet.GetEffectiveSystemPromptPath(cfg.Defaults.SystemPromptPath, "")
	if effectivePath != "./examples/model-specific.prompt" {
		t.Fatalf("expected model-specific prompt, got %s", effectivePath)
	}

	effective := sonnet.GetEffectiveGenerationParams(cfg.Defaults.GenerationParams, nil)
	if effective.Temperature == nil || *effective.Temperature != 0.5 {
		t.Fatalf("expected sonnet temperature 0.5, got %v", effective.Temperature)
	}
	if effective.TopP == nil || *effective.TopP != 0.9 {
		t.Fatalf("expected sonnet topP 0.9, got %v", effective.TopP)
	}
	if effective.MaxTokens == nil || *effective.MaxTokens != 4096 {
		t.Fatalf("expected sonnet maxTokens 4096, got %v", effective.MaxTokens)
	}

	if len(cfg.MCPServers) != 1 {
		t.Fatalf("Expected 1 MCP server, got %d", len(cfg.MCPServers))
	}

	editor, exists := cfg.MCPServers["editor"]
	if !exists {
		t.Fatalf("Expected to find 'editor' MCP server")
	}
	if editor.Command != "mcp-server-editor" {
		t.Fatalf("unexpected command %s", editor.Command)
	}

	if cfg.GetDefaultModel() != "sonnet" {
		t.Fatalf("expected default model sonnet, got %s", cfg.GetDefaultModel())
	}
	if cfg.Defaults.Timeout != "5m" {
		t.Fatalf("expected timeout 5m, got %s", cfg.Defaults.Timeout)
	}
}
