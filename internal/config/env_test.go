package config

import (
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
)

func TestEnvVarExpansion(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		envVars map[string]string
	}{
		{
			name: "expand in model base_url and api_key_env",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    base_url: $BASE_URL
    api_key_env: ${API_KEY_VAR}
`,
			envVars: map[string]string{
				"BASE_URL":    "https://api.example.com",
				"API_KEY_VAR": "ACTUAL_API_KEY",
			},
		},
		{
			name: "expand in MCP server configuration",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
mcpServers:
  server1:
    type: stdio
    command: $MCP_COMMAND
    args:
      - $ARG1
      - ${ARG2}
    env:
      $ENV_KEY: $ENV_VAL
`,
			envVars: map[string]string{
				"MCP_COMMAND": "/usr/bin/server",
				"ARG1":        "arg-one",
				"ARG2":        "arg-two",
				"ENV_KEY":     "ACTUAL_KEY",
				"ENV_VAL":     "actual_value",
			},
		},
		{
			name: "expand in MCP server URL and headers",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
mcpServers:
  server1:
    type: sse
    url: $MCP_URL
    headers:
      Authorization: Bearer $TOKEN
`,
			envVars: map[string]string{
				"MCP_URL": "https://mcp.example.com",
				"TOKEN":   "secret-token",
			},
		},
		{
			name: "expand in subagent configuration",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
subagent:
  name: agent
  description: desc
  outputSchemaPath: $SCHEMA_PATH
`,
			envVars: map[string]string{
				"SCHEMA_PATH": "/path/to/schema.json",
			},
		},
		{
			name: "expand in defaults",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
defaults:
  systemPromptPath: $PROMPT_PATH
  codeMode:
    enabled: true
    excludedTools:
      - $TOOL1
      - ${TOOL2}
`,
			envVars: map[string]string{
				"PROMPT_PATH": "/prompts/system.md",
				"TOOL1":       "expanded_tool_1",
				"TOOL2":       "expanded_tool_2",
			},
		},
		{
			name: "expand in model code mode",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
    codeMode:
      enabled: true
      excludedTools:
        - $MODEL_TOOL
`,
			envVars: map[string]string{
				"MODEL_TOOL": "model_expanded_tool",
			},
		},
		{
			name: "expand in patch request config",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
    patchRequest:
      includeHeaders:
        $HEADER_KEY: $HEADER_VAL
`,
			envVars: map[string]string{
				"HEADER_KEY": "X-Custom-Header",
				"HEADER_VAL": "custom-value",
			},
		},
		{
			name: "no expansion when no env vars match",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
defaults:
  systemPromptPath: literal_path
`,
			envVars: map[string]string{},
		},
		{
			name: "expand in generation params",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
    generationDefaults:
      toolChoice: $TOOL_CHOICE
      stopSequences:
        - $STOP1
        - ${STOP2}
`,
			envVars: map[string]string{
				"TOOL_CHOICE": "auto",
				"STOP1":       "END",
				"STOP2":       "STOP",
			},
		},
		{
			name: "mixed expansion - some vars defined, some not",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    base_url: $DEFINED_URL
    api_key_env: $UNDEFINED_VAR
`,
			envVars: map[string]string{
				"DEFINED_URL": "https://defined.example.com",
				// UNDEFINED_VAR is not set, should expand to empty string
			},
		},
		{
			name: "expand with prefix and suffix",
			yaml: `
models:
  - ref: test
    display_name: Test
    id: test-id
    type: openai
    api_key_env: KEY
    base_url: https://${DOMAIN}/v1
`,
			envVars: map[string]string{
				"DOMAIN": "api.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := parseConfigData([]byte(tt.yaml), "test.yaml")
			if err != nil {
				t.Fatalf("parseConfigData() error = %v", err)
			}

			cupaloy.SnapshotT(t, cfg)
		})
	}
}

func TestEnvVarExpansionJSON(t *testing.T) {
	t.Run("expand in JSON config", func(t *testing.T) {
		t.Setenv("JSON_URL", "https://json.example.com")
		t.Setenv("JSON_KEY", "JSON_API_KEY")

		jsonConfig := `{
  "models": [
    {
      "ref": "test",
      "display_name": "Test",
      "id": "test-id",
      "type": "openai",
      "base_url": "$JSON_URL",
      "api_key_env": "${JSON_KEY}"
    }
  ]
}`

		cfg, err := parseConfigData([]byte(jsonConfig), "test.json")
		if err != nil {
			t.Fatalf("parseConfigData() error = %v", err)
		}

		cupaloy.SnapshotT(t, cfg)
	})
}

func TestEnvVarExpansionEdgeCases(t *testing.T) {
	t.Run("special chars in env var with quotes are safe", func(t *testing.T) {
		// When the YAML value is quoted, special characters in env vars are safe
		t.Setenv("SPECIAL_VALUE", "value:with:colons")

		yaml := `
models:
  - ref: test
    display_name: "$SPECIAL_VALUE"
    id: test-id
    type: openai
    api_key_env: KEY
`
		cfg, err := parseConfigData([]byte(yaml), "test.yaml")
		if err != nil {
			t.Fatalf("parseConfigData() error = %v", err)
		}

		if cfg.Models[0].DisplayName != "value:with:colons" {
			t.Errorf("expected display_name to be 'value:with:colons', got %q", cfg.Models[0].DisplayName)
		}
	})

	t.Run("special chars in unquoted env var may cause parse error", func(t *testing.T) {
		// Unquoted values with YAML special characters can break parsing
		t.Setenv("SPECIAL_VALUE", "value: with colon")

		yaml := `
models:
  - ref: test
    display_name: $SPECIAL_VALUE
    id: test-id
    type: openai
    api_key_env: KEY
`
		_, err := parseConfigData([]byte(yaml), "test.yaml")
		// This may or may not error depending on the YAML parser's handling,
		// but we document that unquoted values with special chars are risky
		if err != nil {
			// Error is expected - verify it contains the hint
			if !strings.Contains(err.Error(), "hint") {
				t.Errorf("expected error to contain expansion hint, got: %v", err)
			}
		}
	})

	t.Run("error message contains expansion hint when parsing fails", func(t *testing.T) {
		t.Setenv("BAD_JSON", `"broken`)

		jsonConfig := `{"models": [{"ref": $BAD_JSON}]}`

		_, err := parseConfigData([]byte(jsonConfig), "test.json")
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		if !strings.Contains(err.Error(), "hint") || !strings.Contains(err.Error(), "environment variable") {
			t.Errorf("expected error to contain expansion hint, got: %v", err)
		}
	})
}
