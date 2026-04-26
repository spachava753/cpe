package agent

import (
	"context"
	"testing"

	"github.com/spachava753/gai"
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
	filter := NewBlockFilterWrapper(inner, whitelistBlockKeepFunc([]string{gai.Content}))

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

func TestProviderBlockFilter_ResponsesKeepsOnlyCompatibleThinkingBlocks(t *testing.T) {
	t.Parallel()

	inner := &captureDialogGenerator{}
	filter := WithBlockFilter("responses")(inner)

	dialog := gai.Dialog{{
		Role: gai.User,
		Blocks: []gai.Block{
			gai.TextBlock("prompt"),
			thinkingBlockForTest("keep", gai.ThinkingGeneratorResponses),
			thinkingBlockForTest("drop", gai.ThinkingGeneratorAnthropic),
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
	if len(inner.dialog[0].Blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3", len(inner.dialog[0].Blocks))
	}
	if got := inner.dialog[0].Blocks[1].Content.String(); got != "keep" {
		t.Fatalf("kept thinking block content = %q, want %q", got, "keep")
	}
	if got := inner.dialog[0].Blocks[2].BlockType; got != gai.ToolCall {
		t.Fatalf("last block type = %q, want %q", got, gai.ToolCall)
	}
}

func TestProviderBlockFilter_OpenAIKeepsOnlyContentAndToolCalls(t *testing.T) {
	t.Parallel()

	inner := &captureDialogGenerator{}
	filter := WithBlockFilter("openai")(inner)

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
