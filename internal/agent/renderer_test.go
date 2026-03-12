package agent

import (
	"testing"

	"github.com/spachava753/cpe/internal/codemode"
)

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

func TestFormatExecuteGoCodeToolCallMarkdownAddsLineNumbers(t *testing.T) {
	t.Parallel()

	input := ExecuteGoCodeFormatInput{
		Code:             "package main\n\nfunc Run() {}",
		ExecutionTimeout: 15,
	}

	want := "#### [tool call] (timeout: 15s)\n```go\n1  package main\n2  \n3  func Run() {}\n```"
	got := FormatExecuteGoCodeToolCallMarkdown(input)
	if got != want {
		t.Fatalf("FormatExecuteGoCodeToolCallMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatExecuteGoCodeToolCallMarkdownUsesSharedLineNumberFormatting(t *testing.T) {
	t.Parallel()

	input := ExecuteGoCodeFormatInput{Code: "package main\nfunc Run() {}"}
	want := "#### [tool call]\n" + codemode.MarkdownFencedBlock("go", codemode.FormatDisplayCodeWithLineNumbers(input.Code))

	got := FormatExecuteGoCodeToolCallMarkdown(input)
	if got != want {
		t.Fatalf("FormatExecuteGoCodeToolCallMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatExecuteGoCodeResultMarkdownUsesSafeFence(t *testing.T) {
	t.Parallel()

	input := "before\n````\nafter"
	want := "#### Code execution output:\n" + codemode.MarkdownFencedBlock("shell", input)

	got := FormatExecuteGoCodeResultMarkdown(input, 0)
	if got != want {
		t.Fatalf("FormatExecuteGoCodeResultMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
