package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"testing"
	"text/template"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

type scriptedToolModel struct {
	responses []gai.Response
	calls     []gai.Dialog
	tools     []gai.Tool
}

func (m *scriptedToolModel) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = opts
	m.calls = append(m.calls, append(gai.Dialog(nil), dialog...))
	if len(m.responses) == 0 {
		return gai.Response{}, errors.New("no scripted response")
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

func (m *scriptedToolModel) Register(tool gai.Tool) error {
	m.tools = append(m.tools, tool)
	return nil
}

type callbackFunc func(context.Context, json.RawMessage, string) (gai.Message, error)

func (f callbackFunc) Call(ctx context.Context, params json.RawMessage, id string) (gai.Message, error) {
	return f(ctx, params, id)
}

type recordingDialogSaver struct {
	nextID int
}

func (s *recordingDialogSaver) SaveDialog(ctx context.Context, msgs iter.Seq[gai.Message]) iter.Seq2[gai.Message, error] {
	return func(yield func(gai.Message, error) bool) {
		_ = ctx
		previousID := ""
		for msg := range msgs {
			saved := msg
			if saved.ExtraFields == nil {
				saved.ExtraFields = make(map[string]any)
			}
			if GetMessageID(saved) == "" {
				s.nextID++
				saved.ExtraFields[storage.MessageIDKey] = fmt.Sprintf("msg_%d", s.nextID)
			}
			if previousID != "" {
				saved.ExtraFields[storage.MessageParentIDKey] = previousID
			}
			previousID = GetMessageID(saved)
			if !yield(saved, nil) {
				return
			}
		}
	}
}

func mustLifecycleToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, params)
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	return block
}

func TestRuntimeFinalAssistantResponseIsAppended(t *testing.T) {
	t.Parallel()

	model := &scriptedToolModel{responses: []gai.Response{{
		FinishReason: gai.EndTurn,
		Candidates:   []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
	}}}
	runtime := NewRuntime(model, config.Config{}, nil, nil, nil, true)

	dialog, err := runtime.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hi")}}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(dialog) != 2 {
		t.Fatalf("dialog length = %d, want 2", len(dialog))
	}
	if got := dialog[1].Blocks[0].Content.String(); got != "done" {
		t.Fatalf("assistant content = %q, want done", got)
	}
}

func TestRuntimeToolCallbackExecutesAndContinues(t *testing.T) {
	t.Parallel()

	callBlock := mustLifecycleToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"})
	model := &scriptedToolModel{responses: []gai.Response{
		{FinishReason: gai.ToolUse, Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{callBlock}}}},
		{FinishReason: gai.EndTurn, Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("final")}}}},
	}}
	runtime := NewRuntime(model, config.Config{}, nil, nil, nil, true)
	if err := runtime.Register(gai.Tool{Name: "lookup"}, callbackFunc(func(ctx context.Context, params json.RawMessage, id string) (gai.Message, error) {
		if string(params) != `{"q":"docs"}` {
			t.Fatalf("params = %s", params)
		}
		return gai.ToolResultMessage(id, gai.TextBlock("result")), nil
	})); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	dialog, err := runtime.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use tool")}}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(dialog) != 4 {
		t.Fatalf("dialog length = %d, want 4", len(dialog))
	}
	if dialog[2].Role != gai.ToolResult || dialog[2].Blocks[0].Content.String() != "result" {
		t.Fatalf("tool result message = %#v", dialog[2])
	}
	if len(model.calls) != 2 || len(model.calls[1]) != 3 {
		t.Fatalf("second model call dialog length = %d, want 3", len(model.calls[1]))
	}
}

func TestRuntimeNilCallbackTerminatesWithToolCallDialog(t *testing.T) {
	t.Parallel()

	callBlock := mustLifecycleToolCallBlock(t, "call_1", "final_answer", map[string]any{"answer": "ok"})
	model := &scriptedToolModel{responses: []gai.Response{{
		FinishReason: gai.ToolUse,
		Candidates:   []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{callBlock}}},
	}}}
	runtime := NewRuntime(model, config.Config{}, nil, nil, nil, true)
	if err := runtime.Register(gai.Tool{Name: "final_answer"}, nil); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	dialog, err := runtime.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("answer")}}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(dialog) != 2 || dialog[1].Blocks[0].ID != "call_1" {
		t.Fatalf("dialog = %#v", dialog)
	}
}

func TestRuntimeCompactionWarningAndRestart(t *testing.T) {
	t.Parallel()

	lookupCall := mustLifecycleToolCallBlock(t, "call_1", "lookup", map[string]any{})
	compactCall := mustLifecycleToolCallBlock(t, "call_2", config.CompactionToolName, map[string]any{"summary": "condensed"})
	model := &scriptedToolModel{responses: []gai.Response{
		{
			FinishReason:  gai.ToolUse,
			Candidates:    []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{lookupCall}}},
			UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 9, gai.UsageMetricGenerationTokens: 2},
		},
		{FinishReason: gai.ToolUse, Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{compactCall}}}},
		{FinishReason: gai.EndTurn, Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("after compact")}}}},
	}}
	cfg := config.Config{Compaction: &config.CompactionConfig{
		TokenThreshold: 10,
		MaxCompactions: 1,
		Tool:           gai.Tool{Name: config.CompactionToolName},
		InitialMessageTemplate: template.Must(template.New("compact").Parse(
			`summary={{ index .ToolArguments "summary" }} parent={{ .PreviousLeafID }}`,
		)),
	}}
	saver := &recordingDialogSaver{}
	runtime := NewRuntime(model, cfg, saver, nil, nil, true)
	if err := runtime.Register(gai.Tool{Name: "lookup"}, callbackFunc(func(ctx context.Context, params json.RawMessage, id string) (gai.Message, error) {
		return gai.ToolResultMessage(id, gai.TextBlock("lookup result")), nil
	})); err != nil {
		t.Fatalf("Register lookup error = %v", err)
	}
	if err := runtime.Register(cfg.Compaction.Tool, nil); err != nil {
		t.Fatalf("Register compaction error = %v", err)
	}

	dialog, err := runtime.Generate(context.Background(), gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("start")}}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(model.calls) != 3 {
		t.Fatalf("model calls = %d, want 3", len(model.calls))
	}
	warningResult := model.calls[1][2]
	if got := warningResult.Blocks[0].Content.String(); got != compactionWarningText {
		t.Fatalf("warning block = %q", got)
	}
	if got := warningResult.Blocks[0].ID; got != "call_1" {
		t.Fatalf("warning block ID = %q, want call_1", got)
	}
	compactedRoot := model.calls[2][0]
	if got := compactedRoot.Blocks[0].Content.String(); got != "summary=condensed parent=msg_4" {
		t.Fatalf("compacted root content = %q", got)
	}
	if got := compactedRoot.ExtraFields[storage.MessageCompactionParentIDKey]; got != "msg_4" {
		t.Fatalf("compaction parent = %#v, want msg_4", got)
	}
	if len(dialog) != 2 || dialog[1].Blocks[0].Content.String() != "after compact" {
		t.Fatalf("final compacted dialog = %#v", dialog)
	}
}
