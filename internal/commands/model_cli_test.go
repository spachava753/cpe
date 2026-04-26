package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModelListFromConfig_PrefersRuntimeModelOverrideForDefaultMarker(t *testing.T) {
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
  - ref: opus
    display_name: Opus
    id: claude-opus-4-20250514
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    context_window: 200000
    max_output: 64000
# model selection is runtime-only
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	err := ModelListFromConfig(context.Background(), ModelListFromConfigOptions{
		ConfigPath:   configPath,
		DefaultModel: "opus",
		Writer:       &out,
	})
	if err != nil {
		t.Fatalf("ModelListFromConfig() error = %v", err)
	}

	want := "sonnet\nopus (default)\n"
	if got := out.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
