package agent

import (
	"context"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

type captureDialogGenerator struct {
	dialog gai.Dialog
}

func (c *captureDialogGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = options
	c.dialog = dialog
	return gai.Response{}, nil
}

func TestDialogBlockFilter_PreservesEmptyToolResultMessages(t *testing.T) {
	t.Parallel()

	inner := &captureDialogGenerator{}
	filter := newBlockFilterWrapper(inner, whitelistBlockKeepFunc([]string{gai.Content}))

	_, err := filter.Generate(context.Background(), gai.Dialog{{
		Role:   gai.ToolResult,
		Blocks: nil,
	}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(inner.dialog) != 1 {
		t.Fatalf("len(dialog) = %d, want 1", len(inner.dialog))
	}
	if inner.dialog[0].Role != gai.ToolResult {
		t.Fatalf("role = %v, want %v", inner.dialog[0].Role, gai.ToolResult)
	}
	if len(inner.dialog[0].Blocks) != 0 {
		t.Fatalf("len(blocks) = %d, want 0", len(inner.dialog[0].Blocks))
	}
}

func TestApplyBlockProvenance(t *testing.T) {
	t.Parallel()

	model := config.Model{Ref: "codex", BaseUrl: "https://chatgpt.example/codex"}
	message := gai.Message{Blocks: []gai.Block{
		gai.TextBlock("answer"),
		{
			BlockType:    gai.Thinking,
			ModalityType: gai.Text,
			Content:      gai.Str("reasoning"),
			ExtraFields:  map[string]any{"existing": "value"},
		},
	}}

	ApplyBlockProvenance(&message, model)

	want := provenanceForModel(model)
	for i, block := range message.Blocks {
		got, ok := provenanceForBlock(block)
		if !ok || got != want {
			t.Fatalf("block %d provenance = %#v, %t; want %#v, true", i, got, ok, want)
		}
	}
	if got := message.Blocks[1].ExtraFields["existing"]; got != "value" {
		t.Fatalf("existing block metadata = %v, want value", got)
	}
}

func TestProviderBlockFilter_ResponsesKeepsOnlyMatchingProvenance(t *testing.T) {
	t.Parallel()

	target := config.Model{
		Ref:     "copilot",
		Type:    ModelTypeResponses,
		BaseUrl: "https://api.githubcopilot.example",
	}
	matching := thinkingBlockForModelTest("keep matching reasoning", gai.ThinkingGeneratorResponses, target)
	matching.ExtraFields[gai.ResponsesExtraFieldReasoningID] = "rs_copilot"
	matching.ExtraFields[gai.ResponsesExtraFieldEncryptedContent] = "copilot-encrypted-content"
	differentModel := thinkingBlockForModelTest(
		"drop different model",
		gai.ThinkingGeneratorResponses,
		config.Model{Ref: "codex", BaseUrl: target.BaseUrl},
	)
	differentURL := thinkingBlockForModelTest(
		"drop different URL",
		gai.ThinkingGeneratorResponses,
		config.Model{Ref: target.Ref, BaseUrl: "https://chatgpt.example/codex"},
	)
	legacy := thinkingBlockForTest("drop missing provenance", gai.ThinkingGeneratorResponses)
	foreignGenerator := thinkingBlockForModelTest("drop foreign generator", gai.ThinkingGeneratorAnthropic, target)

	inner := &captureDialogGenerator{}
	filter := WithBlockFilter(target)(inner)
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("first prompt")}},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				gai.TextBlock("answer"),
				matching,
				differentModel,
				differentURL,
				legacy,
				foreignGenerator,
				mustFilterToolCallBlock(t, "call_1", "test_tool"),
			},
		},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow-up")}},
	}

	_, err := filter.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(inner.dialog) != 3 {
		t.Fatalf("len(dialog) = %d, want 3", len(inner.dialog))
	}
	if len(inner.dialog[1].Blocks) != 3 {
		t.Fatalf("len(assistant blocks) = %d, want 3", len(inner.dialog[1].Blocks))
	}
	if got := inner.dialog[1].Blocks[0].Content.String(); got != "answer" {
		t.Fatalf("assistant content = %q, want answer", got)
	}
	if got := inner.dialog[1].Blocks[1].Content.String(); got != "keep matching reasoning" {
		t.Fatalf("kept thinking content = %q, want %q", got, "keep matching reasoning")
	}
	if got := inner.dialog[1].Blocks[2].BlockType; got != gai.ToolCall {
		t.Fatalf("last assistant block type = %q, want %q", got, gai.ToolCall)
	}
}

func TestProviderBlockFilter_OpenAIKeepsOnlyContentAndToolCalls(t *testing.T) {
	t.Parallel()

	inner := &captureDialogGenerator{}
	filter := WithBlockFilter(config.Model{Type: "openai"})(inner)

	dialog := gai.Dialog{{
		Role: gai.User,
		Blocks: []gai.Block{
			gai.TextBlock("prompt"),
			thinkingBlockForTest("drop", gai.ThinkingGeneratorResponses),
			{BlockType: gai.MetadataBlockType, ModalityType: gai.Text, Content: gai.Str("metadata")},
			mustFilterToolCallBlock(t, "call_1", "test_tool"),
		},
	}}

	_, err := filter.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(inner.dialog) != 1 {
		t.Fatalf("len(dialog) = %d, want 1", len(inner.dialog))
	}
	if len(inner.dialog[0].Blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(inner.dialog[0].Blocks))
	}
	if got := inner.dialog[0].Blocks[0].BlockType; got != gai.Content {
		t.Fatalf("first block type = %q, want %q", got, gai.Content)
	}
	if got := inner.dialog[0].Blocks[1].BlockType; got != gai.ToolCall {
		t.Fatalf("second block type = %q, want %q", got, gai.ToolCall)
	}
}

func thinkingBlockForModelTest(content, generatorType string, model config.Model) gai.Block {
	message := gai.Message{Blocks: []gai.Block{thinkingBlockForTest(content, generatorType)}}
	ApplyBlockProvenance(&message, model)
	return message.Blocks[0]
}

func thinkingBlockForTest(content, generatorType string) gai.Block {
	return gai.Block{
		BlockType:    gai.Thinking,
		ModalityType: gai.Text,
		Content:      gai.Str(content),
		ExtraFields: map[string]any{
			gai.ThinkingExtraFieldGeneratorKey: generatorType,
		},
	}
}

func mustFilterToolCallBlock(t *testing.T, id, name string) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, map[string]any{"value": 1})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	return block
}
