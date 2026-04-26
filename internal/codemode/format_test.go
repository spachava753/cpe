package codemode

import "testing"

func TestFormatToolCallMarkdownIncludesTimeout(t *testing.T) {
	t.Parallel()

	input := FormatInput{
		Code:             "package main\n\nfunc Run() {}",
		ExecutionTimeout: 15,
	}

	want := "#### [tool call] (timeout: 15s)\n```go\npackage main\n\nfunc Run() {}\n\n```"
	got := FormatToolCallMarkdown(input)
	if got != want {
		t.Fatalf("FormatToolCallMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatToolCallMarkdownFormatsGoCodeForDisplay(t *testing.T) {
	t.Parallel()

	input := FormatInput{Code: "package main\nimport \"fmt\"\nfunc Run(){fmt.Println(\"hi\")}"}
	want := "#### [tool call]\n```go\npackage main\n\nimport \"fmt\"\n\nfunc Run() { fmt.Println(\"hi\") }\n\n```"

	got := FormatToolCallMarkdown(input)
	if got != want {
		t.Fatalf("FormatToolCallMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatToolCallMarkdownFallsBackForInvalidGo(t *testing.T) {
	t.Parallel()

	input := FormatInput{Code: "package main\nfunc Run() {"}
	want := "#### [tool call]\n" + MarkdownFencedBlock("go", input.Code)

	got := FormatToolCallMarkdown(input)
	if got != want {
		t.Fatalf("FormatToolCallMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatResultMarkdownUsesSafeFence(t *testing.T) {
	t.Parallel()

	input := "before\n````\nafter"
	want := "#### Code execution output:\n" + MarkdownFencedBlock("shell", input)

	got := FormatResultMarkdown(input, 0)
	if got != want {
		t.Fatalf("FormatResultMarkdown() mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
