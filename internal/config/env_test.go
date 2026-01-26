package config

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/cpe/internal/mcp"
)

func TestExpandEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *RawConfig
		envVars map[string]string
	}{
		{
			name: "expand in model base_url and api_key_env",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{
							Ref:         "test",
							DisplayName: "Test",
							ID:          "id",
							Type:        "openai",
							BaseUrl:     "$BASE_URL",
							ApiKeyEnv:   "${API_KEY_VAR}",
						},
					},
				},
			},
			envVars: map[string]string{
				"BASE_URL":    "https://api.example.com",
				"API_KEY_VAR": "ACTUAL_API_KEY",
			},
		},
		{
			name: "expand in MCP server configuration",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				MCPServers: map[string]mcp.ServerConfig{
					"server1": {
						Command: "$MCP_COMMAND",
						Args:    []string{"$ARG1", "${ARG2}"},
						URL:     "$MCP_URL",
						Env:     map[string]string{"$ENV_KEY": "$ENV_VAL"},
						Headers: map[string]string{"Authorization": "Bearer $TOKEN"},
					},
				},
			},
			envVars: map[string]string{
				"MCP_COMMAND": "/usr/bin/server",
				"ARG1":        "arg-one",
				"ARG2":        "arg-two",
				"MCP_URL":     "https://mcp.example.com",
				"ENV_KEY":     "ACTUAL_KEY",
				"ENV_VAL":     "actual_value",
				"TOKEN":       "secret-token",
			},
		},
		{
			name: "expand in subagent configuration",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Subagent: &SubagentConfig{
					Name:             "agent",
					Description:      "desc",
					OutputSchemaPath: "$SCHEMA_PATH",
				},
			},
			envVars: map[string]string{
				"SCHEMA_PATH": "/path/to/schema.json",
			},
		},
		{
			name: "expand in defaults",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					SystemPromptPath: "$PROMPT_PATH",
					CodeMode: &CodeModeConfig{
						Enabled:       true,
						ExcludedTools: []string{"$TOOL1", "${TOOL2}"},
					},
				},
			},
			envVars: map[string]string{
				"PROMPT_PATH": "/prompts/system.md",
				"TOOL1":       "expanded_tool_1",
				"TOOL2":       "expanded_tool_2",
			},
		},
		{
			name: "expand in model code mode",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"},
						CodeMode: &CodeModeConfig{
							Enabled:       true,
							ExcludedTools: []string{"$MODEL_TOOL"},
						},
					},
				},
			},
			envVars: map[string]string{
				"MODEL_TOOL": "model_expanded_tool",
			},
		},
		{
			name: "expand in patch request config",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{
						Model: Model{
							Ref:         "test",
							DisplayName: "Test",
							ID:          "id",
							Type:        "openai",
							ApiKeyEnv:   "KEY",
							PatchRequest: &PatchRequestConfig{
								IncludeHeaders: map[string]string{
									"$HEADER_KEY": "$HEADER_VAL",
								},
							},
						},
					},
				},
			},
			envVars: map[string]string{
				"HEADER_KEY": "X-Custom-Header",
				"HEADER_VAL": "custom-value",
			},
		},
		{
			name: "no expansion when no env vars match",
			cfg: &RawConfig{
				Models: []ModelConfig{
					{Model: Model{Ref: "test", DisplayName: "Test", ID: "id", Type: "openai", ApiKeyEnv: "KEY"}},
				},
				Defaults: Defaults{
					SystemPromptPath: "literal_path",
				},
			},
			envVars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			tt.cfg.expandEnvironmentVariables()

			cupaloy.SnapshotT(t, tt.cfg)
		})
	}
}

func TestExpandStringSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		envVars map[string]string
		want    []string
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			envVars: map[string]string{},
			want:    nil,
		},
		{
			name:    "empty slice returns empty slice",
			input:   []string{},
			envVars: map[string]string{},
			want:    []string{},
		},
		{
			name:    "expands environment variables",
			input:   []string{"$VAR1", "${VAR2}", "literal"},
			envVars: map[string]string{"VAR1": "value1", "VAR2": "value2"},
			want:    []string{"value1", "value2", "literal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			got := expandStringSlice(tt.input)

			if tt.want == nil {
				if got != nil {
					t.Errorf("expandStringSlice() = %v, want nil", got)
				}
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("expandStringSlice() len = %d, want %d", len(got), len(tt.want))
				return
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("expandStringSlice()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExpandStringMap(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]string
		envVars map[string]string
		want    map[string]string
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			envVars: map[string]string{},
			want:    nil,
		},
		{
			name:    "empty map returns empty map",
			input:   map[string]string{},
			envVars: map[string]string{},
			want:    map[string]string{},
		},
		{
			name:    "expands both keys and values",
			input:   map[string]string{"$KEY": "$VALUE"},
			envVars: map[string]string{"KEY": "expanded_key", "VALUE": "expanded_value"},
			want:    map[string]string{"expanded_key": "expanded_value"},
		},
		{
			name:    "key collision on expansion - last write wins",
			input:   map[string]string{"$KEY1": "val1", "$KEY2": "val2"},
			envVars: map[string]string{"KEY1": "same", "KEY2": "same"},
			// When keys collide, one value overwrites the other (map iteration order dependent)
			// We just verify the key exists with some value
			want: nil, // Special case: verify key exists
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			got := expandStringMap(tt.input)

			// Special case for key collision test
			if tt.name == "key collision on expansion - last write wins" {
				if len(got) != 1 {
					t.Errorf("expandStringMap() len = %d, want 1 (collision should result in single key)", len(got))
				}
				if _, ok := got["same"]; !ok {
					t.Error("expandStringMap() expected key 'same' after collision")
				}
				return
			}

			if tt.want == nil {
				if got != nil {
					t.Errorf("expandStringMap() = %v, want nil", got)
				}
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("expandStringMap() len = %d, want %d", len(got), len(tt.want))
				return
			}

			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok || gotV != wantV {
					t.Errorf("expandStringMap()[%q] = %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}
