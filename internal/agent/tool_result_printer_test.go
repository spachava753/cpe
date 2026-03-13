package agent

import (
	"testing"

	"github.com/spachava753/gai"
)

func TestToolResultPrinterFindToolNameSearchesEarlierAssistantMessages(t *testing.T) {
	t.Parallel()

	printer := &ToolResultPrinterWrapper{}
	assistantMsg := gai.Message{
		Role: gai.Assistant,
		Blocks: []gai.Block{
			mustToolCallBlock(t, "call_1", "first_tool", map[string]any{"value": 1}),
			mustToolCallBlock(t, "call_2", "second_tool", map[string]any{"value": 2}),
		},
	}
	firstResult := gai.ToolResultMessage("call_1", gai.TextBlock("first result"))
	secondResult := gai.ToolResultMessage("call_2", gai.TextBlock("second result"))
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run two tools")}},
		assistantMsg,
		firstResult,
		secondResult,
	}

	got := printer.findToolName(dialog, secondResult)
	want := "second_tool"
	if got != want {
		t.Fatalf("findToolName() = %q, want %q", got, want)
	}
}

func TestToolResultPrinterFindToolNameReturnsUnknownWhenNoMatchExists(t *testing.T) {
	t.Parallel()

	printer := &ToolResultPrinterWrapper{}
	toolResult := gai.ToolResultMessage("missing_call", gai.TextBlock("result"))
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "known_tool", map[string]any{})}},
		toolResult,
	}

	got := printer.findToolName(dialog, toolResult)
	want := "unknown"
	if got != want {
		t.Fatalf("findToolName() = %q, want %q", got, want)
	}
}

func TestToolResultPrinterFindToolNameDoesNotReuseStaleToolCallIDs(t *testing.T) {
	t.Parallel()

	printer := &ToolResultPrinterWrapper{}
	toolResult := gai.ToolResultMessage("call_1", gai.TextBlock("latest result"))
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("first turn")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "old_tool", map[string]any{})}},
		gai.ToolResultMessage("call_1", gai.TextBlock("old result")),
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("second turn")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_2", "new_tool", map[string]any{})}},
		toolResult,
	}

	got := printer.findToolName(dialog, toolResult)
	want := "unknown"
	if got != want {
		t.Fatalf("findToolName() = %q, want %q", got, want)
	}
}

func TestToolResultPrinterFindToolNameReturnsUnknownForEmptyDecodedName(t *testing.T) {
	t.Parallel()

	printer := &ToolResultPrinterWrapper{}
	toolResult := gai.ToolResultMessage("call_1", gai.TextBlock("result"))
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run malformed tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{{
			ID:           "call_1",
			BlockType:    gai.ToolCall,
			ModalityType: gai.Text,
			MimeType:     "application/json",
			Content:      gai.Str(`{"parameters":{"value":1}}`),
		}}},
		toolResult,
	}

	got := printer.findToolName(dialog, toolResult)
	want := "unknown"
	if got != want {
		t.Fatalf("findToolName() = %q, want %q", got, want)
	}
}
