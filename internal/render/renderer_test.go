package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlainTextRendererRender(t *testing.T) {
	t.Run("uses", func(t *testing.T) {
		t.Run("glamour formatting", func(t *testing.T) {
			renderer := NewPlainTextRenderer()
			input := "#### Tool \"starlark_repl\" result:\n````shell\nline one\nline two\n````"

			got, err := renderer.Render(input)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got == input {
				t.Fatalf("Render() returned input unchanged")
			}
			for _, unwanted := range []string{"````shell"} {
				if strings.Contains(got, unwanted) {
					t.Fatalf("rendered output still contains markdown marker %q:\n%s", unwanted, got)
				}
			}
			for _, want := range []string{"#### Tool \"starlark_repl\" result:", "line one", "line two"} {
				if !strings.Contains(got, want) {
					t.Fatalf("rendered output missing %q:\n%s", want, got)
				}
			}
		})
		t.Run("ascii style", func(t *testing.T) {
			renderer := NewPlainTextRenderer()

			got, err := renderer.Render("> quoted")
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if !strings.Contains(got, "| quoted") {
				t.Fatalf("rendered output did not use ASCII blockquote marker:\n%s", got)
			}
			if strings.Contains(got, "│") {
				t.Fatalf("rendered output used non-ASCII blockquote marker:\n%s", got)
			}
		})
	})
	t.Run("does not", func(t *testing.T) {
		t.Run("emit ansi sequences", func(t *testing.T) {
			renderer := NewPlainTextRenderer()
			input := "# Heading\n\nA [link](https://example.com) and `code`.\n\n```go\nfmt.Println(\"hello\")\n```"

			got, err := renderer.Render(input)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if strings.Contains(got, "\x1b[") {
				t.Fatalf("rendered output contains ANSI sequence:\n%q", got)
			}
		})
		t.Run("wrap", func(t *testing.T) {
			renderer := NewPlainTextRenderer()
			longLine := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau"

			got, err := renderer.Render(longLine)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if !strings.Contains(got, longLine) {
				t.Fatalf("rendered output wrapped or changed the long line\nwant line: %q\ngot:\n%s", longLine, got)
			}
		})
	})
}

func TestWriterWordWrapWidthFallsBackFor(t *testing.T) {
	t.Run("non terminal writer", func(t *testing.T) {
		if got := writerWordWrapWidth(&bytes.Buffer{}); got != defaultWordWrapWidth {
			t.Fatalf("writerWordWrapWidth() = %d, want %d", got, defaultWordWrapWidth)
		}
	})
	t.Run("nil writer", func(t *testing.T) {
		if got := writerWordWrapWidth(nil); got != defaultWordWrapWidth {
			t.Fatalf("writerWordWrapWidth() = %d, want %d", got, defaultWordWrapWidth)
		}
	})
}

func TestExpandCodeBlockTabs(t *testing.T) {
	t.Run("expands only code block content", func(t *testing.T) {
		input := "before\toutside\n```shell\nok\tgithub.com/example/pkg\t0.1s\n```\nafter\toutside"
		want := "before\toutside\n```shell\nok      github.com/example/pkg  0.1s\n```\nafter\toutside"

		got := expandCodeBlockTabs(input)
		if got != want {
			t.Fatalf("expandCodeBlockTabs() mismatch\nwant: %q\ngot:  %q", want, got)
		}
	})
	t.Run("handles indented tilde fence", func(t *testing.T) {
		input := "  ~~~~\na\tb\n  ~~~~\n"
		want := "  ~~~~\na       b\n  ~~~~\n"

		got := expandCodeBlockTabs(input)
		if got != want {
			t.Fatalf("expandCodeBlockTabs() mismatch\nwant: %q\ngot:  %q", want, got)
		}
	})
}
