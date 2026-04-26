package codemode

import (
	"bytes"
	"encoding/json"
	"fmt"
	goformat "go/format"
	"strings"
)

// FormatInput contains the parsed input from an execute_go_code tool call.
type FormatInput struct {
	Code             string
	ExecutionTimeout int
}

// ParseToolCall attempts to parse a tool call JSON as an execute_go_code input.
// Returns the parsed input and true if successful, or empty input and false otherwise.
func ParseToolCall(content string) (FormatInput, bool) {
	var toolCall struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(content), &toolCall); err != nil {
		return FormatInput{}, false
	}
	if toolCall.Name != ExecuteGoCodeToolName {
		return FormatInput{}, false
	}

	var input ExecuteGoCodeInput
	if err := json.Unmarshal(toolCall.Parameters, &input); err != nil || input.Code == "" {
		return FormatInput{}, false
	}

	return FormatInput(input), true
}

// FormatToolCallMarkdown formats an execute_go_code tool call as markdown.
// The displayed source is reflowed for readability, so compiler diagnostic line
// numbers in later tool results may not correspond exactly to the printed code.
// That is acceptable because the model still receives diagnostics for the exact
// generated source it executed; this display is primarily for users to understand
// what the model is doing.
func FormatToolCallMarkdown(input FormatInput) string {
	header := "#### [tool call]"
	if input.ExecutionTimeout > 0 {
		header = fmt.Sprintf("#### [tool call] (timeout: %ds)", input.ExecutionTimeout)
	}
	displayCode := formatGoCodeForDisplay(input.Code)
	return fmt.Sprintf("%s\n%s", header, MarkdownFencedBlock("go", displayCode))
}

func formatGoCodeForDisplay(code string) string {
	formatted, err := goformat.Source([]byte(code))
	if err != nil {
		return code
	}
	return string(formatted)
}

// FormatResultMarkdown formats an execute_go_code result as markdown.
func FormatResultMarkdown(content string, maxLines int) string {
	truncated := truncateToMaxLines(content, maxLines)
	return fmt.Sprintf("#### Code execution output:\n%s", MarkdownFencedBlock("shell", truncated))
}

// IsToolCall checks if a tool call JSON is for the execute_go_code tool.
func IsToolCall(content string) bool {
	_, ok := ParseToolCall(content)
	return ok
}

// FormatGenericToolCallMarkdown formats a generic tool call JSON as markdown.
// Returns the formatted content and true if formatting succeeded,
// or the original content and false if JSON parsing failed.
func FormatGenericToolCallMarkdown(content string) (string, bool) {
	var formattedJSON bytes.Buffer
	if err := json.Indent(&formattedJSON, []byte(content), "", "  "); err != nil {
		return content, false
	}
	return fmt.Sprintf("#### [tool call]\n%s", MarkdownFencedBlock("json", formattedJSON.String())), true
}

func truncateToMaxLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return content
	}
	if content == "" {
		return content
	}

	trailingNewline := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimSuffix(content, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= maxLines {
		return content
	}

	truncated := strings.Join(lines[:maxLines], "\n")
	if trailingNewline {
		truncated += "\n"
	}
	return truncated + "... (truncated)"
}
