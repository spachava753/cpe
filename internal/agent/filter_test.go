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

func TestBlockWhitelistFilter_PreservesEmptyToolResultMessages(t *testing.T) {
	t.Parallel()

	inner := &captureDialogGenerator{}
	filter := NewBlockWhitelistFilter(inner, []string{gai.Content})

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

func TestThinkingBlockFilter_PreservesEmptyToolResultMessages(t *testing.T) {
	t.Parallel()

	inner := &captureDialogGenerator{}
	filter := NewThinkingBlockFilter(inner, []string{gai.ThinkingGeneratorResponses})

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
