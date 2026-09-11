package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
	"gopkg.in/yaml.v3"
)

func TestResolve(t *testing.T) {
	t.Run("in memory config keeps paths without a source location", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{"", "prompts/agent.md", filepath.Join(t.TempDir(), "agent.md")} {
			model := testModelProfile()
			model.SystemPromptPath = path
			cfg, err := ResolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: model.Ref})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SystemPromptPath != path {
				t.Fatalf("path = %q, want unchanged %q", cfg.SystemPromptPath, path)
			}
		}
	})
	t.Run("loaded config keeps prompt paths anchored after changing directories", func(t *testing.T) {
		for _, source := range []string{"absolute config path", "relative config path", "current directory discovery", "user config discovery"} {
			t.Run(source, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config home"))
				t.Setenv("APPDATA", filepath.Join(home, "AppData"))
				userConfigDir, err := os.UserConfigDir()
				if err != nil {
					t.Fatal(err)
				}
				configDir := filepath.Join(userConfigDir, "cpe")
				if err := os.MkdirAll(filepath.Join(configDir, "prompts"), 0o700); err != nil {
					t.Fatal(err)
				}
				promptPath := filepath.Join(configDir, "prompts", "agent.md")
				if err := os.WriteFile(promptPath, []byte("the configured prompt"), 0o600); err != nil {
					t.Fatal(err)
				}
				models := []ModelConfig{testModelProfile(), testModelProfile(), testModelProfile()}
				models[0].Ref = "relative"
				models[0].SystemPromptPath = "./prompts/../prompts/agent.md"
				models[1].Ref = "absolute"
				models[1].SystemPromptPath = promptPath
				models[2].Ref = "no-prompt"
				models[2].SystemPromptPath = ""
				encoded, err := yaml.Marshal(&RawConfig{Models: models})
				if err != nil {
					t.Fatal(err)
				}
				configPath := filepath.Join(configDir, "cpe.yaml")
				if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				t.Chdir(configDir)
				loadPath := configPath
				switch source {
				case "relative config path":
					loadPath = "cpe.yaml"
				case "current directory discovery":
					loadPath = ""
				case "user config discovery":
					t.Chdir(home)
					loadPath = ""
				}
				raw, err := LoadRawConfig(loadPath)
				if err != nil {
					t.Fatal(err)
				}

				// Runtime creation happens later, potentially in another directory.
				// Plant a same-named prompt there to catch accidental CWD resolution.
				runtimeDir := t.TempDir()
				if err := os.MkdirAll(filepath.Join(runtimeDir, "prompts"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(runtimeDir, "prompts", "agent.md"), []byte("wrong prompt"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Chdir(runtimeDir)
				for _, model := range models {
					cfg, err := ResolveFromRaw(raw, RuntimeOptions{ModelRef: model.Ref})
					if err != nil {
						t.Fatal(err)
					}
					wantPath, wantPrompt := promptPath, "the configured prompt"
					if model.SystemPromptPath == "" {
						wantPath, wantPrompt = "", ""
					}
					if cfg.SystemPromptPath != wantPath {
						t.Fatalf("%s runtime path = %q, want %q", model.Ref, cfg.SystemPromptPath, wantPath)
					}
					prompt, err := LoadSystemPrompt(t.Context(), LoadSystemPromptOptions{SystemPromptPath: cfg.SystemPromptPath, Config: cfg})
					if err != nil || prompt != wantPrompt {
						t.Fatalf("%s runtime prompt = %q, %v; want %q", model.Ref, prompt, err, wantPrompt)
					}
					fromFile, err := ResolveConfig(configPath, RuntimeOptions{ModelRef: model.Ref})
					if err != nil || !reflect.DeepEqual(fromFile, cfg) {
						t.Fatalf("%s ResolveConfig and ResolveFromRaw disagree: %v", model.Ref, err)
					}
					if model.SystemPromptPath != "" {
						var out bytes.Buffer
						err := ModelSystemPromptFromConfig(t.Context(), ModelSystemPromptFromConfigOptions{ConfigPath: configPath, ModelName: model.Ref, Output: &out})
						if err != nil || !strings.HasSuffix(out.String(), "\n\n"+wantPrompt+"\n") {
							t.Fatalf("%s inspection output = %q, %v", model.Ref, out.String(), err)
						}
					}
				}
				for i, model := range models {
					if raw.Models[i].SystemPromptPath != model.SystemPromptPath {
						t.Fatalf("resolution mutated the raw prompt path for %s", model.Ref)
					}
				}
			})
		}
	})
	t.Run("config requires model", func(t *testing.T) {
		_, err := ResolveFromRaw(&RawConfig{Models: []ModelConfig{testModelProfile()}}, RuntimeOptions{})
		if err == nil {
			t.Fatal("expected missing model error")
		}
		want := "no model specified. Set CPE_MODEL or pass --model"
		if err.Error() != want {
			t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
		}
	})
	t.Run("generation params uses model profile and runtime only", func(t *testing.T) {
		model := testModelProfile()
		model.GenerationParams = &generationParams{
			Temperature:         new(0.7),
			MaxGenerationTokens: new(1024),
			StopSequences:       []string{"model-stop"},
		}
		result := resolveGenerationParams(model, RuntimeOptions{GenParams: &gai.GenOpts{
			Temperature:   new(0.2),
			StopSequences: []string{"runtime-stop"},
		}})

		checkPtr(t, "Temperature", result.Temperature, new(0.2))
		checkPtr(t, "MaxGenerationTokens", result.MaxGenerationTokens, new(1024))
		if got, want := result.StopSequences, []string{"runtime-stop"}; len(got) != len(want) || got[0] != want[0] {
			t.Fatalf("StopSequences = %v, want %v", got, want)
		}
	})
	t.Run("timeout uses model profile and runtime override", func(t *testing.T) {
		model := testModelProfile()
		model.Timeout = "30s"
		got, err := resolveTimeout(model, RuntimeOptions{})
		if err != nil {
			t.Fatalf("resolveTimeout returned error: %v", err)
		}
		if got != 30*time.Second {
			t.Fatalf("timeout = %s, want 30s", got)
		}

		got, err = resolveTimeout(model, RuntimeOptions{Timeout: "2m"})
		if err != nil {
			t.Fatalf("resolveTimeout override returned error: %v", err)
		}
		if got != 2*time.Minute {
			t.Fatalf("timeout = %s, want 2m", got)
		}
	})
	t.Run("disable edit tool", func(t *testing.T) {
		t.Parallel()

		defaultModel := testModelProfile()
		disabledModel := testModelProfile()
		disabledModel.Ref = "without-edit"
		disabledModel.DisableEditTool = true

		cfg, err := ResolveFromRaw(&RawConfig{Models: []ModelConfig{defaultModel, disabledModel}}, RuntimeOptions{ModelRef: "test-model"})
		if err != nil {
			t.Fatalf("ResolveFromRaw default returned error: %v", err)
		}
		if cfg.DisableEditTool {
			t.Fatal("DisableEditTool default = true, want false")
		}

		cfg, err = ResolveFromRaw(&RawConfig{Models: []ModelConfig{defaultModel, disabledModel}}, RuntimeOptions{ModelRef: "without-edit"})
		if err != nil {
			t.Fatalf("ResolveFromRaw disabled returned error: %v", err)
		}
		if !cfg.DisableEditTool {
			t.Fatal("DisableEditTool = false, want true")
		}
	})
	t.Run("compaction from model profile", func(t *testing.T) {
		t.Parallel()

		model := testModelProfile()
		model.ContextWindow = 1000
		model.Compaction = &RawCompactionConfig{
			AutoTriggerThreshold:      0.25,
			MaxAutoCompactionRestarts: 2,
			ToolDescription:           "model compact",
			InputSchema:               jsonschema.Schema{Type: "object"},
			InitialMessageTemplate:    "model {{ .ToolArgumentsJSON }}",
		}

		cfg, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: "test-model"}, "")
		if err != nil {
			t.Fatalf("resolveFromRaw returned error: %v", err)
		}
		if cfg.Compaction == nil {
			t.Fatal("expected compaction config")
		}
		if cfg.Compaction.TokenThreshold != 250 {
			t.Fatalf("TokenThreshold = %d, want 250", cfg.Compaction.TokenThreshold)
		}
		if cfg.Compaction.MaxCompactions != 2 {
			t.Fatalf("MaxCompactions = %d, want 2", cfg.Compaction.MaxCompactions)
		}
		if cfg.Compaction.Tool.Description != "model compact" {
			t.Fatalf("tool description = %q", cfg.Compaction.Tool.Description)
		}
		if cfg.Compaction.InputSchema == nil {
			t.Fatal("resolved input schema is nil")
		}
	})
	t.Run("compaction invalid schema fails", func(t *testing.T) {
		t.Parallel()

		model := testModelProfile()
		model.Compaction = &RawCompactionConfig{
			AutoTriggerThreshold:      0.5,
			MaxAutoCompactionRestarts: 1,
			ToolDescription:           "compact",
			InputSchema:               jsonschema.Schema{Ref: "#/$defs/missing"},
			InitialMessageTemplate:    "compacted",
		}

		_, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: "test-model"}, "")
		if err == nil {
			t.Fatal("expected invalid input schema error")
		}
		if !strings.Contains(err.Error(), "inputSchema") {
			t.Fatalf("error = %q, want inputSchema context", err)
		}
	})
	t.Run("compaction invalid template fails", func(t *testing.T) {
		t.Parallel()

		model := testModelProfile()
		model.Compaction = &RawCompactionConfig{
			AutoTriggerThreshold: 0.5, MaxAutoCompactionRestarts: 1, ToolDescription: "compact", InputSchema: jsonschema.Schema{Type: "object"}, InitialMessageTemplate: "{{",
		}

		_, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: "test-model"}, "")
		if err == nil {
			t.Fatal("expected invalid template error")
		}
	})
}

func checkPtr[T comparable](t *testing.T, name string, got, want *T) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s: expected nil, got %v", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s: expected %v, got nil", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s: expected %v, got %v", name, *want, *got)
	}
}

func testModelProfile() ModelConfig {
	return ModelConfig{Model: Model{
		Ref:           "test-model",
		DisplayName:   "Test Model",
		ID:            "test-id",
		Type:          "openai",
		ApiKeyEnv:     "OPENAI_API_KEY",
		ContextWindow: 200000,
		MaxOutput:     64000,
	}}
}
