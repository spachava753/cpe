package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spachava753/cpe/internal/codemode"
)

// ExecuteGoCodeFormatInput contains the parsed input from an execute_go_code tool call
type ExecuteGoCodeFormatInput struct {
	Code             string
	ExecutionTimeout int
}

// ParseExecuteGoCodeToolCall attempts to parse a tool call JSON as an execute_go_code input.
// Returns the parsed input and true if successful, or empty input and false otherwise.
func ParseExecuteGoCodeToolCall(content string) (ExecuteGoCodeFormatInput, bool) {
	// First try to parse as a ToolCallInput
	var toolCall struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(content), &toolCall); err != nil {
		return ExecuteGoCodeFormatInput{}, false
	}

	if toolCall.Name != codemode.ExecuteGoCodeToolName {
		return ExecuteGoCodeFormatInput{}, false
	}

	var input codemode.ExecuteGoCodeInput
	if err := json.Unmarshal(toolCall.Parameters, &input); err != nil || input.Code == "" {
		return ExecuteGoCodeFormatInput{}, false
	}

	return ExecuteGoCodeFormatInput{
		Code:             input.Code,
		ExecutionTimeout: input.ExecutionTimeout,
	}, true
}

// FormatExecuteGoCodeToolCallMarkdown formats an execute_go_code tool call as markdown.
// The output includes a header and a Go code block.
func FormatExecuteGoCodeToolCallMarkdown(input ExecuteGoCodeFormatInput) string {
	header := "#### [tool call]"
	if input.ExecutionTimeout > 0 {
		header = fmt.Sprintf("#### [tool call] (timeout: %ds)", input.ExecutionTimeout)
	}
	return fmt.Sprintf("%s\n```go\n%s\n```", header, input.Code)
}

// FormatExecuteGoCodeResultMarkdown formats an execute_go_code result as markdown.
// The output includes a header and a shell code block.
func FormatExecuteGoCodeResultMarkdown(content string, maxLines int) string {
	truncated := truncateToMaxLines(content, maxLines)
	return fmt.Sprintf("#### Code execution output:\n%s\n", truncated)
}

// IsExecuteGoCodeToolCall checks if a tool call JSON is for the execute_go_code tool.
func IsExecuteGoCodeToolCall(content string) bool {
	_, ok := ParseExecuteGoCodeToolCall(content)
	return ok
}

// FormatGenericToolCallMarkdown formats a generic tool call JSON as markdown.
// The output includes a header and a JSON code block with pretty-printing.
// Returns the formatted content and true if formatting succeeded,
// or the original content and false if JSON parsing failed.
func FormatGenericToolCallMarkdown(content string) (string, bool) {
	var formattedJson bytes.Buffer
	if err := json.Indent(&formattedJson, []byte(content), "", "  "); err != nil {
		return content, false
	}
	return fmt.Sprintf("#### [tool call]\n```json\n%s\n```", formattedJson.String()), true
}

// truncateToMaxLines truncates content to the specified number of lines.
// If maxLines is <= 0, no truncation is performed and content is returned as-is.
func truncateToMaxLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	return strings.Join(lines[:maxLines], "\n") + "\n... (truncated)"
}
