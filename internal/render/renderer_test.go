package render

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
