package codemode

import "testing"

func TestFormatDisplayCodeWithLineNumbers(t *testing.T) {
	t.Parallel()

	input := "package main\n\nfunc Run() {}\n"
	want := "1  package main\n2  \n3  func Run() {}"

	got := FormatDisplayCodeWithLineNumbers(input)
	if got != want {
		t.Fatalf("FormatDisplayCodeWithLineNumbers() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatDisplayCodeWithLineNumbersPadsMultiDigitLines(t *testing.T) {
	t.Parallel()

	input := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl"
	want := " 1  a\n 2  b\n 3  c\n 4  d\n 5  e\n 6  f\n 7  g\n 8  h\n 9  i\n10  j\n11  k\n12  l"

	got := FormatDisplayCodeWithLineNumbers(input)
	if got != want {
		t.Fatalf("FormatDisplayCodeWithLineNumbers() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatDisplayCodeWithLineNumbersPreservesSingleBlankLine(t *testing.T) {
	t.Parallel()

	input := "\n"
	want := "1  "

	got := FormatDisplayCodeWithLineNumbers(input)
	if got != want {
		t.Fatalf("FormatDisplayCodeWithLineNumbers() mismatch\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestMarkdownFencedBlockUsesLongerFenceWhenNeeded(t *testing.T) {
	t.Parallel()

	input := "before\n```\nafter"
	want := "````go\nbefore\n```\nafter\n````"

	got := MarkdownFencedBlock("go", input)
	if got != want {
		t.Fatalf("MarkdownFencedBlock() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatDisplayCodeWithLineNumbersNormalizesCRLF(t *testing.T) {
	t.Parallel()

	input := "package main\r\n\r\nfunc Run() {}\r\n"
	want := "1  package main\n2  \n3  func Run() {}"

	got := FormatDisplayCodeWithLineNumbers(input)
	if got != want {
		t.Fatalf("FormatDisplayCodeWithLineNumbers() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
