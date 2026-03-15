package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMCPListServersFromConfig_DoesNotRequireDefaultModel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "cpe.yaml")
	configYAML := `version: "1.0"
models:
  - ref: sonnet
    display_name: Sonnet
    id: claude-sonnet-4-20250514
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    context_window: 200000
    max_output: 64000
mcpServers:
  local:
    command: echo
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	err := MCPListServersFromConfig(context.Background(), MCPListServersFromConfigOptions{
		ConfigPath: configPath,
		Writer:     &out,
	})
	if err != nil {
		t.Fatalf("MCPListServersFromConfig() error = %v", err)
	}

	want := "Configured MCP Servers:\n- local (Type: stdio, Timeout: 60s)\n  Command: echo \n"
	if got := out.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
