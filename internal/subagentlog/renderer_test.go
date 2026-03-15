package subagentlog

import (
	"testing"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/render"
)

func TestRendererRenderEventExecuteGoCodeToolCallAddsLineNumbers(t *testing.T) {
	t.Parallel()

	renderer := NewRenderer(&render.PlainTextRenderer{}, RenderModeVerbose)
	event := Event{
		Type:                    EventTypeToolCall,
		SubagentName:            "worker",
		SubagentRunID:           "run-123",
		ToolName:                codemode.ExecuteGoCodeToolName,
		ExecutionTimeoutSeconds: 30,
		Payload:                 "package main\nfunc Run() {}",
	}

	want := "#### worker [run-123] [tool call] (timeout: 30s)\n" + codemode.MarkdownFencedBlock("go", codemode.FormatDisplayCodeWithLineNumbers(event.Payload))
	got := renderer.RenderEvent(event)
	if got != want {
		t.Fatalf("RenderEvent() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestRendererRenderEventToolResultUsesSafeFence(t *testing.T) {
	t.Parallel()

	renderer := NewRenderer(&render.PlainTextRenderer{}, RenderModeVerbose)
	event := Event{
		Type:          EventTypeToolResult,
		SubagentName:  "worker",
		SubagentRunID: "run-123",
		ToolName:      codemode.ExecuteGoCodeToolName,
		Payload:       "before\n```\nafter",
	}

	want := "#### worker [run-123] Code execution output:\n" + codemode.MarkdownFencedBlock("shell", event.Payload)
	got := renderer.RenderEvent(event)
	if got != want {
		t.Fatalf("RenderEvent() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestRendererRenderEventToolResultConciseUsesNeutralMarker(t *testing.T) {
	t.Parallel()

	renderer := NewRenderer(&render.PlainTextRenderer{}, RenderModeConcise)
	event := Event{
		Type:          EventTypeToolResult,
		SubagentName:  "worker",
		SubagentRunID: "run-123",
		ToolName:      codemode.ExecuteGoCodeToolName,
	}

	want := "#### worker [run-123] ← execute_go_code"
	got := renderer.RenderEvent(event)
	if got != want {
		t.Fatalf("RenderEvent() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestRendererZeroValueFallsBackToPlainText(t *testing.T) {
	t.Parallel()

	var renderer Renderer
	event := Event{
		Type:          EventTypeToolResult,
		SubagentName:  "worker",
		SubagentRunID: "run-123",
		ToolName:      codemode.ExecuteGoCodeToolName,
		Payload:       "output",
	}

	want := "#### worker [run-123] ← execute_go_code"
	got := renderer.RenderEvent(event)
	if got != want {
		t.Fatalf("RenderEvent() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
