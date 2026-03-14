package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cenkalti/backoff/v5"
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

func TestToolResultPrinterGeneratePrintsAllTrailingToolResults(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	printer := &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: staticGenerator{}},
		renderer:         &PlainTextRenderer{},
		output:           &output,
	}
	assistantMsg := gai.Message{
		Role: gai.Assistant,
		Blocks: []gai.Block{
			mustToolCallBlock(t, "call_1", "execute_go_code", map[string]any{"value": 1}),
			mustToolCallBlock(t, "call_2", "execute_go_code", map[string]any{"value": 2}),
		},
	}
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run two tools")}},
		assistantMsg,
		gai.ToolResultMessage("call_1", gai.TextBlock("first result")),
		gai.ToolResultMessage("call_2", gai.TextBlock("second result")),
	}

	_, err := printer.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	got := output.String()
	want := "\n#### Tool \"execute_go_code\" result:\n\n#### Code execution output:\n```shell\nfirst result\n```" +
		"\n#### Tool \"execute_go_code\" result:\n\n#### Code execution output:\n```shell\nsecond result\n```"
	if got != want {
		t.Fatalf("Generate() output = %q, want %q", got, want)
	}
}

func TestToolResultPrinterOutsideRetryPrintsOnce(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	flaky := &flakyGenerator{failuresRemaining: 1}
	gen := gai.Wrap(
		flaky,
		func(g gai.Generator) gai.Generator {
			return &ToolResultPrinterWrapper{
				GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
				renderer:         &PlainTextRenderer{},
				output:           &output,
			}
		},
		gai.WithRetry(backoff.NewConstantBackOff(0), backoff.WithMaxTries(2)),
	)
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "execute_go_code", map[string]any{"value": 1})}},
		gai.ToolResultMessage("call_1", gai.TextBlock("result once")),
	}

	_, err := gen.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	gotCount := strings.Count(output.String(), `#### Tool "execute_go_code" result:`)
	if gotCount != 1 {
		t.Fatalf("printed tool result header count = %d, want 1; output = %q", gotCount, output.String())
	}
}

type staticGenerator struct{}

func (staticGenerator) Generate(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
	return gai.Response{}, nil
}

type flakyGenerator struct {
	failuresRemaining int
}

func (g *flakyGenerator) Generate(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
	if g.failuresRemaining > 0 {
		g.failuresRemaining--
		return gai.Response{}, &gai.ApiErr{StatusCode: 500, Message: "temporary failure"}
	}
	return gai.Response{}, nil
}
