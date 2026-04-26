package codemode

import "testing"

func TestMarkdownFencedBlockUsesLongerFenceWhenNeeded(t *testing.T) {
	t.Parallel()

	input := "before\n```\nafter"
	want := "````go\nbefore\n```\nafter\n````"

	got := MarkdownFencedBlock("go", input)
	if got != want {
		t.Fatalf("MarkdownFencedBlock() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
