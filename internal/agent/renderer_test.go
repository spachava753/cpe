package agent

import "testing"

func TestPlainTextRendererRender(t *testing.T) {
	renderer := &PlainTextRenderer{}
	input := "#### Tool \"execute_go_code\" result:\n````shell\nline one\nline two\n````"

	got, err := renderer.Render(input)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != input {
		t.Fatalf("Render() output mismatch\nwant: %q\ngot:  %q", input, got)
	}
}

func TestNewRendererForTTYFalseReturnsPlainTextRenderer(t *testing.T) {
	renderer := newRendererForTTY(false)
	if _, ok := renderer.(*PlainTextRenderer); !ok {
		t.Fatalf("newRendererForTTY(false) should return *PlainTextRenderer, got %T", renderer)
	}
}

func TestNewResponsePrinterRenderersForTTYFalseReturnsPlainTextRenderers(t *testing.T) {
	renderers := newResponsePrinterRenderersForTTY(false)

	if _, ok := renderers.Content.(*PlainTextRenderer); !ok {
		t.Fatalf("Content renderer should be *PlainTextRenderer, got %T", renderers.Content)
	}
	if _, ok := renderers.Thinking.(*PlainTextRenderer); !ok {
		t.Fatalf("Thinking renderer should be *PlainTextRenderer, got %T", renderers.Thinking)
	}
	if _, ok := renderers.ToolCall.(*PlainTextRenderer); !ok {
		t.Fatalf("ToolCall renderer should be *PlainTextRenderer, got %T", renderers.ToolCall)
	}
}
