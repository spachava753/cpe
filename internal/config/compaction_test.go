package config

import "testing"

func TestResolveCompaction_DefaultsLevel(t *testing.T) {
	rawCfg := &RawConfig{
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
			Model: "test-model",
			Compaction: &CompactionConfig{
				Enabled:              true,
				AutoTriggerThreshold: 0.8,
				ToolDescription:      "Compact the conversation into a fresh branch.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"summary": map[string]any{"type": "string"},
					},
				},
				InitialMessageTemplate: "Original: {{.OriginalUserMessage}}\nSummary: {{index .ToolInput \"summary\"}}",
			},
		},
	}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{}, "")
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.Compaction == nil {
		t.Fatal("expected compaction config, got nil")
	}
	if !cfg.Compaction.Enabled {
		t.Fatal("expected compaction to be enabled")
	}
	if cfg.Compaction.AutoTriggerThreshold != 0.8 {
		t.Fatalf("unexpected threshold: got %v want %v", cfg.Compaction.AutoTriggerThreshold, 0.8)
	}
	if cfg.Compaction.MaxAutoCompactionRestarts != defaultMaxAutoCompactionRestarts {
		t.Fatalf("unexpected max auto compaction restarts: got %d want %d", cfg.Compaction.MaxAutoCompactionRestarts, defaultMaxAutoCompactionRestarts)
	}
	if cfg.Compaction.ToolDescription != "Compact the conversation into a fresh branch." {
		t.Fatalf("unexpected tool description: got %q", cfg.Compaction.ToolDescription)
	}
	if cfg.Compaction.InputSchema == nil || cfg.Compaction.InputSchema.Properties["summary"] == nil {
		t.Fatal("expected parsed input schema with summary property")
	}

	rendered, err := cfg.Compaction.RenderInitialMessage(CompactionTemplateData{
		OriginalUserMessage: "Refactor the auth module",
		ToolInput:           map[string]any{"summary": "Focus on JWT validation and session refresh."},
	})
	if err != nil {
		t.Fatalf("RenderInitialMessage returned error: %v", err)
	}
	want := "Original: Refactor the auth module\nSummary: Focus on JWT validation and session refresh."
	if rendered != want {
		t.Fatalf("unexpected rendered template: got %q want %q", rendered, want)
	}
}

func TestResolveCompaction_ModelOverride(t *testing.T) {
	rawCfg := &RawConfig{
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
			Compaction: &CompactionConfig{
				Enabled:                   true,
				AutoTriggerThreshold:      0.55,
				MaxAutoCompactionRestarts: 7,
				ToolDescription:           "Model-level compaction tool.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"modelSummary": map[string]any{"type": "string"},
					},
				},
				InitialMessageTemplate: "Model override: {{index .ToolInput \"modelSummary\"}}",
			},
		}},
		Defaults: Defaults{
			Model: "test-model",
			Compaction: &CompactionConfig{
				Enabled:              true,
				AutoTriggerThreshold: 0.8,
				ToolDescription:      "Default compaction tool.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"defaultSummary": map[string]any{"type": "string"},
					},
				},
				InitialMessageTemplate: "Default: {{index .ToolInput \"defaultSummary\"}}",
			},
		},
	}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{}, "")
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.Compaction == nil {
		t.Fatal("expected compaction config, got nil")
	}
	if cfg.Compaction.AutoTriggerThreshold != 0.55 {
		t.Fatalf("unexpected threshold: got %v want %v", cfg.Compaction.AutoTriggerThreshold, 0.55)
	}
	if cfg.Compaction.MaxAutoCompactionRestarts != 7 {
		t.Fatalf("unexpected max auto compaction restarts: got %d want %d", cfg.Compaction.MaxAutoCompactionRestarts, 7)
	}
	if cfg.Compaction.ToolDescription != "Model-level compaction tool." {
		t.Fatalf("unexpected tool description: got %q", cfg.Compaction.ToolDescription)
	}
	if cfg.Compaction.InputSchema == nil || cfg.Compaction.InputSchema.Properties["modelSummary"] == nil {
		t.Fatal("expected model-level input schema to win")
	}
	if cfg.Compaction.InputSchema.Properties["defaultSummary"] != nil {
		t.Fatal("did not expect defaults-level schema to survive model override")
	}

	rendered, err := cfg.Compaction.RenderInitialMessage(CompactionTemplateData{
		ToolInput: map[string]any{"modelSummary": "Carry forward the shortened branch context."},
	})
	if err != nil {
		t.Fatalf("RenderInitialMessage returned error: %v", err)
	}
	want := "Model override: Carry forward the shortened branch context."
	if rendered != want {
		t.Fatalf("unexpected rendered template: got %q want %q", rendered, want)
	}
}

func TestParseCompactionInputSchema_BooleanSchemas(t *testing.T) {
	trueSchema, err := parseCompactionInputSchema(true, true)
	if err != nil {
		t.Fatalf("parseCompactionInputSchema(true) returned error: %v", err)
	}
	if trueSchema == nil {
		t.Fatal("expected true schema to be non-nil")
	}

	falseSchema, err := parseCompactionInputSchema(false, false)
	if err != nil {
		t.Fatalf("parseCompactionInputSchema(false, disabled) returned error: %v", err)
	}
	if falseSchema == nil || falseSchema.Not == nil {
		t.Fatal("expected false schema to compile to a rejecting schema when disabled")
	}

	_, err = parseCompactionInputSchema(false, true)
	wantErr := "enabled compaction cannot use a false boolean schema"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("unexpected error for enabled false schema: got %v want %q", err, wantErr)
	}
}

func TestValidateCompactionConfig(t *testing.T) {
	tests := []struct {
		name       string
		compaction *CompactionConfig
		wantErr    string
	}{
		{
			name: "invalid threshold",
			compaction: &CompactionConfig{
				Enabled:                true,
				AutoTriggerThreshold:   1.2,
				ToolDescription:        "Compact conversation",
				InitialMessageTemplate: "{{.OriginalUserMessage}}",
			},
			wantErr: "defaults.compaction: autoTriggerThreshold must be > 0 and <= 1",
		},
		{
			name: "negative max auto compaction restarts",
			compaction: &CompactionConfig{
				Enabled:                   true,
				AutoTriggerThreshold:      0.8,
				MaxAutoCompactionRestarts: -1,
				ToolDescription:           "Compact conversation",
				InitialMessageTemplate:    "{{.OriginalUserMessage}}",
			},
			wantErr: "defaults.compaction: maxAutoCompactionRestarts must be >= 1 when set",
		},
		{
			name: "missing description when enabled",
			compaction: &CompactionConfig{
				Enabled:                true,
				AutoTriggerThreshold:   0.8,
				InitialMessageTemplate: "{{.OriginalUserMessage}}",
			},
			wantErr: "defaults.compaction: toolDescription must not be empty when compaction is enabled",
		},
		{
			name: "missing template when enabled",
			compaction: &CompactionConfig{
				Enabled:              true,
				AutoTriggerThreshold: 0.8,
				ToolDescription:      "Compact conversation",
			},
			wantErr: "defaults.compaction: initialMessageTemplate must not be empty when compaction is enabled",
		},
		{
			name: "invalid input schema",
			compaction: &CompactionConfig{
				Enabled:              true,
				AutoTriggerThreshold: 0.8,
				ToolDescription:      "Compact conversation",
				InputSchema: map[string]any{
					"type": make(chan int),
				},
				InitialMessageTemplate: "{{.OriginalUserMessage}}",
			},
			wantErr: "defaults.compaction: inputSchema: marshaling input schema: json: unsupported type: chan int",
		},
		{
			name: "false boolean schema rejected when enabled",
			compaction: &CompactionConfig{
				Enabled:                true,
				AutoTriggerThreshold:   0.8,
				ToolDescription:        "Compact conversation",
				InputSchema:            false,
				InitialMessageTemplate: "{{.OriginalUserMessage}}",
			},
			wantErr: "defaults.compaction: inputSchema: enabled compaction cannot use a false boolean schema",
		},
		{
			name: "invalid template parse",
			compaction: &CompactionConfig{
				Enabled:                true,
				AutoTriggerThreshold:   0.8,
				ToolDescription:        "Compact conversation",
				InitialMessageTemplate: "{{",
			},
			wantErr: "defaults.compaction: initialMessageTemplate: parsing template: template: compaction initial message template:1: unclosed action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := rawConfigWithCompaction(tt.compaction)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("unexpected error: got %q want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func rawConfigWithCompaction(compaction *CompactionConfig) RawConfig {
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
			Model:      "test-model",
			Compaction: compaction,
		},
	}
}
