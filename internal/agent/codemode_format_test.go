package agent

import "testing"

func TestTruncateToMaxLinesDoesNotTruncateTrailingNewlineOnly(t *testing.T) {
	t.Parallel()

	input := "ok\n"
	want := "ok\n"

	got := truncateToMaxLines(input, 1)
	if got != want {
		t.Fatalf("truncateToMaxLines() mismatch\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestTruncateToMaxLinesPreservesTrailingNewlineWhenTruncating(t *testing.T) {
	t.Parallel()

	input := "one\ntwo\nthree\n"
	want := "one\ntwo\n... (truncated)"

	got := truncateToMaxLines(input, 2)
	if got != want {
		t.Fatalf("truncateToMaxLines() mismatch\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}
