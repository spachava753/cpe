package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

func TestValidateModelAuth(t *testing.T) {
	tests := []struct {
		name       string
		model      ModelConfig
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "apikey auth for openai is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-4",
					Type:        "openai",
					ApiKeyEnv:   "OPENAI_API_KEY",
					AuthMethod:  "apikey",
				},
			},
			wantErr: false,
		},
		{
			name: "no auth_method is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-4",
					Type:        "openai",
					ApiKeyEnv:   "OPENAI_API_KEY",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth for anthropic is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "claude-sonnet",
					Type:        "anthropic",
					AuthMethod:  "oauth",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth for responses is valid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-5.2-codex",
					Type:        "responses",
					AuthMethod:  "oauth",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth for openai type is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gpt-4",
					Type:        "openai",
					AuthMethod:  "oauth",
				},
			},
			wantErr:    true,
			wantErrMsg: "auth_method 'oauth' is only supported for anthropic and responses providers",
		},
		{
			name: "oauth for gemini is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "gemini-pro",
					Type:        "gemini",
					AuthMethod:  "oauth",
				},
			},
			wantErr:    true,
			wantErrMsg: "auth_method 'oauth' is only supported for anthropic and responses providers",
		},
		{
			name: "oauth for openrouter is invalid",
			model: ModelConfig{
				Model: Model{
					Ref:         "test",
					DisplayName: "Test",
					ID:          "some-model",
					Type:        "openrouter",
					AuthMethod:  "oauth",
				},
			},
			wantErr:    true,
			wantErrMsg: "auth_method 'oauth' is only supported for anthropic and responses providers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelAuth(tt.model)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrMsg != "" && err.Error() != tt.wantErrMsg {
					t.Errorf("unexpected error: got %q want %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRawConfigValidate_RequiresContextWindowAndMaxOutput(t *testing.T) {
	t.Parallel()

	base := RawConfig{
		Version: "1.0",
		Models: []ModelConfig{
			{
				Model: Model{
					Ref:           "test-model",
					DisplayName:   "Test Model",
					ID:            "test-id",
					Type:          "openai",
					ApiKeyEnv:     "OPENAI_API_KEY",
					ContextWindow: 200000,
					MaxOutput:     64000,
				},
			},
		},
		Defaults: Defaults{Model: "test-model"},
	}

	if err := base.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	missingContext := base
	missingContext.Models = append([]ModelConfig(nil), base.Models...)
	missingContext.Models[0].ContextWindow = 0
	if err := missingContext.Validate(); err == nil {
		t.Fatal("expected validation error for missing context_window")
	}

	missingMaxOutput := base
	missingMaxOutput.Models = append([]ModelConfig(nil), base.Models...)
	missingMaxOutput.Models[0].MaxOutput = 0
	if err := missingMaxOutput.Validate(); err == nil {
		t.Fatal("expected validation error for missing max_output")
	}
}

func TestRawConfigValidate_CompactionStructTags(t *testing.T) {
	t.Parallel()

	locations := []struct {
		name        string
		apply       func(*RawConfig, *RawCompactionConfig)
		fieldPrefix string
	}{
		{
			name: "defaults compaction",
			apply: func(cfg *RawConfig, compaction *RawCompactionConfig) {
				cfg.Defaults.Compaction = compaction
			},
			fieldPrefix: "RawConfig.Defaults.Compaction",
		},
		{
			name: "model compaction",
			apply: func(cfg *RawConfig, compaction *RawCompactionConfig) {
				cfg.Models[0].Compaction = compaction
			},
			fieldPrefix: "RawConfig.Models[0].Compaction",
		},
	}

	cases := []struct {
		name      string
		mutate    func(*RawCompactionConfig)
		fieldName string
		wantTag   string
	}{
		{
			name: "requires auto trigger threshold",
			mutate: func(compaction *RawCompactionConfig) {
				compaction.AutoTriggerThreshold = 0
			},
			fieldName: "AutoTriggerThreshold",
			wantTag:   "required",
		},
		{
			name: "caps auto trigger threshold at one",
			mutate: func(compaction *RawCompactionConfig) {
				compaction.AutoTriggerThreshold = 1.2
			},
			fieldName: "AutoTriggerThreshold",
			wantTag:   "max",
		},
		{
			name: "requires positive max auto compaction restarts",
			mutate: func(compaction *RawCompactionConfig) {
				compaction.MaxAutoCompactionRestarts = -1
			},
			fieldName: "MaxAutoCompactionRestarts",
			wantTag:   "min",
		},
		{
			name: "requires tool description",
			mutate: func(compaction *RawCompactionConfig) {
				compaction.ToolDescription = ""
			},
			fieldName: "ToolDescription",
			wantTag:   "required",
		},
		{
			name: "requires input schema",
			mutate: func(compaction *RawCompactionConfig) {
				compaction.InputSchema = jsonschema.Schema{}
			},
			fieldName: "InputSchema",
			wantTag:   "required",
		},
		{
			name: "requires initial message template",
			mutate: func(compaction *RawCompactionConfig) {
				compaction.InitialMessageTemplate = ""
			},
			fieldName: "InitialMessageTemplate",
			wantTag:   "required",
		},
	}

	for _, location := range locations {

		for _, tc := range cases {

			t.Run(location.name+"/"+tc.name, func(t *testing.T) {
				cfg := rawConfigWithCompaction(*validCompactionConfig())
				compaction := validCompactionConfig()
				tc.mutate(compaction)
				location.apply(&cfg, compaction)

				err := cfg.Validate()
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				assertValidationError(t, err, location.fieldPrefix+"."+tc.fieldName, tc.wantTag)
			})
		}
	}
}

func TestRawConfigValidate_CompactionStructTags_AcceptsValidConfig(t *testing.T) {
	t.Parallel()

	cfg := rawConfigWithCompaction(*validCompactionConfig())
	cfg.Models[0].Compaction = validCompactionConfig()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestRawConfigValidate_CompactionStructTags_OmittedInDefaultsAndModel(t *testing.T) {
	t.Parallel()

	cfg := rawConfigWithCodeMode(CodeModeConfig{})
	cfg.Defaults.CodeMode = nil
	cfg.Defaults.Compaction = nil
	cfg.Models[0].Compaction = nil

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected omitted compaction config to be valid, got error: %v", err)
	}
}

func TestValidateWithConfigPath_CodeModePaths(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "cpe.yaml")

	moduleDir := filepath.Join(configDir, "helpers")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("creating module dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/helpers\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("writing module go.mod: %v", err)
	}

	t.Run("valid local module paths", func(t *testing.T) {
		cfg := rawConfigWithCodeMode(CodeModeConfig{
			Enabled:          true,
			LocalModulePaths: []string{"./helpers"},
		})

		if err := cfg.ValidateWithConfigPath(configPath); err != nil {
			t.Fatalf("expected valid config, got error: %v", err)
		}
	})

	t.Run("missing go.mod in local module path", func(t *testing.T) {
		noModDir := filepath.Join(configDir, "no-mod")
		if err := os.MkdirAll(noModDir, 0o755); err != nil {
			t.Fatalf("creating no-mod dir: %v", err)
		}

		cfg := rawConfigWithCodeMode(CodeModeConfig{
			Enabled:          true,
			LocalModulePaths: []string{"./no-mod"},
		})

		err := cfg.ValidateWithConfigPath(configPath)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		want := "defaults.codeMode.localModulePaths[0]: missing go.mod in module directory: " + filepath.Join(canonicalPathForValidator(noModDir), "go.mod")
		if err.Error() != want {
			t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
		}
	})

	t.Run("rejects empty local module path entry", func(t *testing.T) {
		cfg := rawConfigWithCodeMode(CodeModeConfig{
			Enabled:          true,
			LocalModulePaths: []string{"   "},
		})

		err := cfg.ValidateWithConfigPath(configPath)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		want := "defaults.codeMode: localModulePaths[0] must not be empty"
		if err.Error() != want {
			t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
		}
	})

	t.Run("rejects duplicate local module paths after resolution", func(t *testing.T) {
		cfg := rawConfigWithCodeMode(CodeModeConfig{
			Enabled:          true,
			LocalModulePaths: []string{"./helpers", moduleDir},
		})

		err := cfg.ValidateWithConfigPath(configPath)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		want := "defaults.codeMode: localModulePaths contains duplicate path: " + canonicalPathForValidator(moduleDir)
		if err.Error() != want {
			t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
		}
	})
}

func canonicalPathForValidator(path string) string {
	cleaned := filepath.Clean(path)
	if realPath, err := filepath.EvalSymlinks(cleaned); err == nil {
		return filepath.Clean(realPath)
	}
	return cleaned
}

func TestValidateWithConfigPath_SubagentOutputSchemaPathRelativeToConfig(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "cpe.yaml")
	schemaPath := filepath.Join(configDir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("writing schema: %v", err)
	}

	cfg := RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
		Subagent: &SubagentConfig{
			Name:             "test-subagent",
			Description:      "test",
			OutputSchemaPath: "./schema.json",
		},
	}

	if err := cfg.ValidateWithConfigPath(configPath); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateWithConfigPath_SubagentOutputSchemaPathAcceptsBooleanSchema(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "cpe.yaml")
	schemaPath := filepath.Join(configDir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`true`), 0o644); err != nil {
		t.Fatalf("writing schema: %v", err)
	}

	cfg := RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
		Subagent: &SubagentConfig{
			Name:             "test-subagent",
			Description:      "test",
			OutputSchemaPath: "./schema.json",
		},
	}

	if err := cfg.ValidateWithConfigPath(configPath); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateWithConfigPath_MCPServerURLRequiresExplicitType(t *testing.T) {
	t.Parallel()

	cfg := RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
		MCPServers: map[string]mcpconfig.ServerConfig{
			"remote": {URL: "http://example.com/mcp"},
		},
	}

	err := cfg.ValidateWithConfigPath("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "mcpServers.remote.type: required when url is set; use \"http\" or \"sse\""
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestValidateWithConfigPath_MCPServerHeadersRequireRemoteTransportType(t *testing.T) {
	t.Parallel()

	cfg := RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
		MCPServers: map[string]mcpconfig.ServerConfig{
			"local": {Command: "echo", Headers: map[string]string{"X-Test": "1"}},
		},
	}

	err := cfg.ValidateWithConfigPath("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "mcpServers.local.headers: only supported for type \"http\" or \"sse\""
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestValidateWithConfigPath_RemoteMCPServerRejectsCommandAndArgs(t *testing.T) {
	t.Parallel()

	cfg := RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{Model: "test-model"},
		MCPServers: map[string]mcpconfig.ServerConfig{
			"remote": {Type: "http", URL: "http://example.com/mcp", Command: "echo", Args: []string{"hello"}},
		},
	}

	err := cfg.ValidateWithConfigPath("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "mcpServers.remote.command: only supported for type \"stdio\""
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func assertValidationError(t *testing.T, err error, wantField, wantTag string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "invalid configuration file:") {
		t.Fatalf("expected validator error wrapper, got %q", msg)
	}
	if !strings.Contains(msg, wantField) {
		t.Fatalf("expected error to mention %q, got %q", wantField, msg)
	}
	if !strings.Contains(msg, "failed on the '"+wantTag+"' tag") {
		t.Fatalf("expected error to mention tag %q, got %q", wantTag, msg)
	}
}

func validCompactionConfig() *RawCompactionConfig {
	return &RawCompactionConfig{
		AutoTriggerThreshold:      0.8,
		MaxAutoCompactionRestarts: 3,
		ToolDescription:           "Compact the conversation into a fresh branch.",
		InputSchema: jsonschema.Schema{
			Type: "object",
		},
		InitialMessageTemplate: "Original: {{.OriginalUserMessage}}",
	}
}

func rawConfigWithCompaction(compaction RawCompactionConfig) RawConfig {
	cfg := rawConfigWithCodeMode(CodeModeConfig{})
	cfg.Defaults.CodeMode = nil
	cfg.Defaults.Compaction = &compaction
	return cfg
}

func rawConfigWithCodeMode(codeMode CodeModeConfig) RawConfig {
	return RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200000,
				MaxOutput:     64000,
			},
		}},
		Defaults: Defaults{
			Model:    "test-model",
			CodeMode: &codeMode,
		},
	}
}
