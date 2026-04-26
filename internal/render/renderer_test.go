package render

import (
	"bytes"
	"testing"
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

func TestWriterWordWrapWidthFallsBackForNonTerminalWriter(t *testing.T) {
	if got := writerWordWrapWidth(&bytes.Buffer{}); got != defaultWordWrapWidth {
		t.Fatalf("writerWordWrapWidth() = %d, want %d", got, defaultWordWrapWidth)
	}
}

func TestWriterWordWrapWidthFallsBackForNilWriter(t *testing.T) {
	if got := writerWordWrapWidth(nil); got != defaultWordWrapWidth {
		t.Fatalf("writerWordWrapWidth() = %d, want %d", got, defaultWordWrapWidth)
	}
}

func TestExpandCodeBlockTabsExpandsOnlyCodeBlockContent(t *testing.T) {
	input := "before\toutside\n```shell\nok\tgithub.com/example/pkg\t0.1s\n```\nafter\toutside"
	want := "before\toutside\n```shell\nok      github.com/example/pkg  0.1s\n```\nafter\toutside"

	got := expandCodeBlockTabs(input)
	if got != want {
		t.Fatalf("expandCodeBlockTabs() mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestExpandCodeBlockTabsHandlesIndentedTildeFence(t *testing.T) {
	input := "  ~~~~\na\tb\n  ~~~~\n"
	want := "  ~~~~\na       b\n  ~~~~\n"

	got := expandCodeBlockTabs(input)
	if got != want {
		t.Fatalf("expandCodeBlockTabs() mismatch\nwant: %q\ngot:  %q", want, got)
	}
}
