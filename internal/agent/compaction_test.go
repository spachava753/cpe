package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	internalmcp "github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/storage"
)

type stubToolCapableGenerator struct {
	registered  []gai.Tool
	responses   []gai.Response
	generateErr error
	calls       int
}

func (s *stubToolCapableGenerator) Register(tool gai.Tool) error {
	s.registered = append(s.registered, tool)
	return nil
}

func (s *stubToolCapableGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = dialog
	_ = options
	if s.generateErr != nil {
		return gai.Response{}, s.generateErr
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

type stubDialogGenerator struct {
	results []gai.Dialog
	inputs  []gai.Dialog
}

func (s *stubDialogGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	_ = ctx
	if optsGen != nil {
		_ = optsGen(dialog)
	}
	copied := append(gai.Dialog(nil), dialog...)
	s.inputs = append(s.inputs, copied)
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func mustResolvedConfig(t *testing.T, compaction *config.CompactionConfig) *config.Config {
	t.Helper()
	raw := &config.RawConfig{
		Version: "1.0",
		Models: []config.ModelConfig{{
			Model: config.Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				ID:            "test-id",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 200,
				MaxOutput:     64,
			},
		}},
		Defaults: config.Defaults{
			Model:      "test-model",
			Compaction: compaction,
		},
	}
	cfg, err := config.ResolveFromRaw(raw, config.RuntimeOptions{ModelRef: "test-model"})
	if err != nil {
		t.Fatalf("ResolveFromRaw returned error: %v", err)
	}
	return cfg
}

func mustToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, params)
	if err != nil {
		t.Fatalf("ToolCallBlock returned error: %v", err)
	}
	return block
}

func TestNewGenerator_RegistersCompactionToolWhenEnabled(t *testing.T) {
	base := &stubToolCapableGenerator{}
	cfg := mustResolvedConfig(t, &config.CompactionConfig{
		Enabled:                true,
		AutoTriggerThreshold:   0.8,
		ToolDescription:        "Compact the conversation.",
		InputSchema:            map[string]any{"type": "object"},
		InitialMessageTemplate: "{{.OriginalUserMessage}}",
	})

	_, err := NewGenerator(context.Background(), cfg, "system prompt", internalmcp.NewMCPState(), WithBaseGenerator(base), WithDisablePrinting())
	if err != nil {
		t.Fatalf("NewGenerator returned error: %v", err)
	}
	if len(base.registered) != 1 {
		t.Fatalf("expected 1 registered tool, got %d", len(base.registered))
	}
	if base.registered[0].Name != compactConversationToolName {
		t.Fatalf("unexpected tool name: got %q want %q", base.registered[0].Name, compactConversationToolName)
	}
}

func TestNewGenerator_DoesNotRegisterCompactionToolWhenDisabled(t *testing.T) {
	base := &stubToolCapableGenerator{}
	cfg := mustResolvedConfig(t, nil)

	_, err := NewGenerator(context.Background(), cfg, "system prompt", internalmcp.NewMCPState(), WithBaseGenerator(base), WithDisablePrinting())
	if err != nil {
		t.Fatalf("NewGenerator returned error: %v", err)
	}
	if len(base.registered) != 0 {
		t.Fatalf("expected 0 registered tools, got %d", len(base.registered))
	}
}

func TestCompactionUsageTrackingGenerator_FlipsThresholdState(t *testing.T) {
	base := &stubToolCapableGenerator{responses: []gai.Response{{
		UsageMetadata: gai.Metadata{
			gai.UsageMetricInputTokens:      70,
			gai.UsageMetricGenerationTokens: 40,
		},
	}}}
	state := &compactionRunState{contextWindow: 200, threshold: 0.5}
	wrapped := newCompactionUsageTrackingGenerator(base)

	if _, err := wrapped.Generate(withCompactionRunState(context.Background(), state), nil, nil); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if !state.thresholdExceeded {
		t.Fatal("expected thresholdExceeded to be true")
	}
	if state.latestUsedTokens != 110 {
		t.Fatalf("unexpected used tokens: got %d want %d", state.latestUsedTokens, 110)
	}
	if state.latestUtilization != 0.55 {
		t.Fatalf("unexpected utilization: got %v want %v", state.latestUtilization, 0.55)
	}
}

func TestWrapToolCallbackWithCompactionWarning_PrependsWarning(t *testing.T) {
	state := &compactionRunState{contextWindow: 200, threshold: 0.75}
	state.latestUsedTokens = 160
	state.latestUtilization = 0.8
	state.thresholdExceeded = true

	callback := toolCallbackFunc(func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
		_ = ctx
		_ = parametersJSON
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{{
				ID:           toolCallID,
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("original result"),
			}},
		}, nil
	})

	wrapped := wrapToolCallbackWithCompactionWarning(callback)
	msg, err := wrapped.Call(withCompactionRunState(context.Background(), state), json.RawMessage(`{}`), "tool_1")
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if len(msg.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(msg.Blocks))
	}
	if got := msg.Blocks[0].Content.String(); got != state.warningText() {
		t.Fatalf("unexpected warning block: got %q want %q", got, state.warningText())
	}
	if got := msg.Blocks[1].Content.String(); got != "original result" {
		t.Fatalf("unexpected original block content: got %q want %q", got, "original result")
	}
}

func TestCompactionRuntime_WrapToolCallbackWrapper_ComposesBaseWrapperAndWarning(t *testing.T) {
	cfg := mustResolvedConfig(t, &config.CompactionConfig{
		Enabled:                true,
		AutoTriggerThreshold:   0.75,
		ToolDescription:        "Compact the conversation.",
		InputSchema:            map[string]any{"type": "object"},
		InitialMessageTemplate: "{{.OriginalUserMessage}}",
	})
	runtime := newCompactionRuntime(cfg)
	if runtime == nil {
		t.Fatal("expected compaction runtime, got nil")
	}

	state := &compactionRunState{contextWindow: 200, threshold: 0.75}
	state.latestUsedTokens = 160
	state.latestUtilization = 0.8
	state.thresholdExceeded = true

	baseWrapperCalled := false
	baseWrapper := func(toolName string, callback gai.ToolCallback) gai.ToolCallback {
		if toolName != "test_tool" {
			t.Fatalf("toolName = %q, want %q", toolName, "test_tool")
		}
		return toolCallbackFunc(func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			baseWrapperCalled = true
			msg, err := callback.Call(ctx, parametersJSON, toolCallID)
			if err != nil {
				return msg, err
			}
			msg.Blocks = append(msg.Blocks, gai.TextBlock("wrapped result"))
			return msg, nil
		})
	}

	callback := toolCallbackFunc(func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
		_ = ctx
		_ = parametersJSON
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{{
				ID:           toolCallID,
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("original result"),
			}},
		}, nil
	})

	wrappedCallback := runtime.wrapToolCallbackWrapper(baseWrapper)("test_tool", callback)
	msg, err := wrappedCallback.Call(withCompactionRunState(context.Background(), state), json.RawMessage(`{}`), "tool_1")
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if !baseWrapperCalled {
		t.Fatal("expected base wrapper to be called")
	}
	if len(msg.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(msg.Blocks))
	}
	if got := msg.Blocks[0].Content.String(); got != state.warningText() {
		t.Fatalf("unexpected warning block: got %q want %q", got, state.warningText())
	}
	if got := msg.Blocks[1].Content.String(); got != "original result" {
		t.Fatalf("unexpected original block content: got %q want %q", got, "original result")
	}
	if got := msg.Blocks[2].Content.String(); got != "wrapped result" {
		t.Fatalf("unexpected wrapped block content: got %q want %q", got, "wrapped result")
	}
}

func TestExtractCompactionToolInput(t *testing.T) {
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original task")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", compactConversationToolName, map[string]any{"summary": "carry this forward"})}},
	}

	got, ok, err := extractCompactionToolInput(dialog, 1)
	if err != nil {
		t.Fatalf("extractCompactionToolInput returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected compaction tool call to be found")
	}
	if got["summary"] != "carry this forward" {
		t.Fatalf("unexpected tool input: got %v", got)
	}
}

func TestExtractCompactionToolInput_IgnoresHistoricalCompactionCalls(t *testing.T) {
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("old root")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_old", compactConversationToolName, map[string]any{"summary": "stale"})}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("new user turn")}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("normal assistant reply")}},
	}

	got, ok, err := extractCompactionToolInput(dialog, 2)
	if err != nil {
		t.Fatalf("extractCompactionToolInput returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected no fresh compaction tool call, got %v", got)
	}
}

func TestCompactionAwareGenerator_RestartsIntoCompactedBranch(t *testing.T) {
	cfg := mustResolvedConfig(t, &config.CompactionConfig{
		Enabled:                true,
		AutoTriggerThreshold:   0.8,
		ToolDescription:        "Compact the conversation.",
		InputSchema:            map[string]any{"type": "object"},
		InitialMessageTemplate: "Original: {{.OriginalUserMessage}}\nSummary: {{index .ToolInput \"summary\"}}",
	})

	firstAssistant := gai.Message{
		Role:        gai.Assistant,
		Blocks:      []gai.Block{mustToolCallBlock(t, "call_1", compactConversationToolName, map[string]any{"summary": "shortened context"})},
		ExtraFields: map[string]any{storage.MessageIDKey: "msg_prev"},
	}
	finalAssistant := gai.Message{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}

	base := &stubDialogGenerator{results: []gai.Dialog{
		{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original task")}}, firstAssistant},
		{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Original: original task\nSummary: shortened context")}}, finalAssistant},
	}}
	wrapped := newCompactionAwareGenerator(base, nil, cfg.Compaction, cfg.Model.ContextWindow)

	result, err := wrapped.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original task")}}}, func(dialog gai.Dialog) *gai.GenOpts {
		return &gai.GenOpts{StopSequences: []string{"halt"}}
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(base.inputs) != 2 {
		t.Fatalf("expected 2 generator invocations, got %d", len(base.inputs))
	}
	if len(base.inputs[1]) != 1 {
		t.Fatalf("expected compacted dialog with 1 message, got %d", len(base.inputs[1]))
	}
	if got := base.inputs[1][0].Blocks[0].Content.String(); got != "Original: original task\nSummary: shortened context" {
		t.Fatalf("unexpected compacted root content: got %q", got)
	}
	if got := base.inputs[1][0].ExtraFields[storage.MessageCompactionParentIDKey]; got != "msg_prev" {
		t.Fatalf("unexpected compaction parent metadata: got %v want %q", got, "msg_prev")
	}
	if len(result) != 2 || result[1].Blocks[0].Content.String() != "done" {
		t.Fatalf("unexpected final dialog: %#v", result)
	}
}

func TestCompactionAwareGenerator_StopsAtLoopCap(t *testing.T) {
	cfg := mustResolvedConfig(t, &config.CompactionConfig{
		Enabled:                   true,
		AutoTriggerThreshold:      0.8,
		MaxAutoCompactionRestarts: 1,
		ToolDescription:           "Compact the conversation.",
		InputSchema:               &jsonschema.Schema{Type: "object"},
		InitialMessageTemplate:    "{{.OriginalUserMessage}}",
	})

	base := &stubDialogGenerator{results: []gai.Dialog{
		{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("task")}}, {Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", compactConversationToolName, map[string]any{})}, ExtraFields: map[string]any{storage.MessageIDKey: "msg_1"}}},
		{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("task")}}, {Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_2", compactConversationToolName, map[string]any{})}, ExtraFields: map[string]any{storage.MessageIDKey: "msg_2"}}},
	}}
	wrapped := newCompactionAwareGenerator(base, nil, cfg.Compaction, cfg.Model.ContextWindow)

	_, err := wrapped.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("task")}}}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "compaction restart limit exceeded after 1 compactions"
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
	if len(base.inputs) != 2 {
		t.Fatalf("expected 2 generator invocations before failure, got %d", len(base.inputs))
	}
}

func TestCompactionAwareGenerator_RegisterDelegates(t *testing.T) {
	registrarTarget := &stubToolCapableGenerator{}
	base := &stubDialogGenerator{}
	wrapped := newCompactionAwareGenerator(base, toolRegistrarAdapter{registrarTarget}, mustResolvedConfig(t, &config.CompactionConfig{
		Enabled:                true,
		AutoTriggerThreshold:   0.8,
		ToolDescription:        "Compact the conversation.",
		InputSchema:            map[string]any{"type": "object"},
		InitialMessageTemplate: "{{.OriginalUserMessage}}",
	}).Compaction, 200)

	tool := gai.Tool{Name: "custom_tool"}
	if err := wrapped.Register(tool, nil); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if len(registrarTarget.registered) != 1 || registrarTarget.registered[0].Name != "custom_tool" {
		t.Fatalf("unexpected delegated registrations: %#v", registrarTarget.registered)
	}
}

type toolRegistrarAdapter struct{ inner *stubToolCapableGenerator }

func (a toolRegistrarAdapter) Register(tool gai.Tool, callback gai.ToolCallback) error {
	_ = callback
	return a.inner.Register(tool)
}
