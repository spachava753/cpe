package agent

import (
	"strings"
	"testing"
)

func TestParseExecuteGoCodeToolCall(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantOk      bool
		wantCode    string
		wantTimeout int
	}{
		{
			name:        "valid execute_go_code tool call",
			content:     `{"name":"execute_go_code","parameters":{"code":"package main\n\nfunc Run() {}","executionTimeout":30}}`,
			wantOk:      true,
			wantCode:    "package main\n\nfunc Run() {}",
			wantTimeout: 30,
		},
		{
			name:        "valid execute_go_code without timeout",
			content:     `{"name":"execute_go_code","parameters":{"code":"package main"}}`,
			wantOk:      true,
			wantCode:    "package main",
			wantTimeout: 0,
		},
		{
			name:    "different tool name",
			content: `{"name":"get_weather","parameters":{"city":"NYC"}}`,
			wantOk:  false,
		},
		{
			name:    "empty code",
			content: `{"name":"execute_go_code","parameters":{"code":""}}`,
			wantOk:  false,
		},
		{
			name:    "missing code field",
			content: `{"name":"execute_go_code","parameters":{"executionTimeout":30}}`,
			wantOk:  false,
		},
		{
			name:    "malformed JSON",
			content: `{invalid json`,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, ok := ParseExecuteGoCodeToolCall(tt.content)
			if ok != tt.wantOk {
				t.Errorf("ParseExecuteGoCodeToolCall() ok = %v, want %v", ok, tt.wantOk)
				return
			}
			if tt.wantOk {
				if input.Code != tt.wantCode {
					t.Errorf("ParseExecuteGoCodeToolCall() code = %q, want %q", input.Code, tt.wantCode)
				}
				if input.ExecutionTimeout != tt.wantTimeout {
					t.Errorf("ParseExecuteGoCodeToolCall() timeout = %d, want %d", input.ExecutionTimeout, tt.wantTimeout)
				}
			}
		})
	}
}

func TestFormatExecuteGoCodeToolCallMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input ExecuteGoCodeFormatInput
		want  string
	}{
		{
			name: "with timeout",
			input: ExecuteGoCodeFormatInput{
				Code:             "package main\n\nfunc Run() {}",
				ExecutionTimeout: 30,
			},
			want: "#### [tool call] (timeout: 30s)\n```go\npackage main\n\nfunc Run() {}\n```",
		},
		{
			name: "without timeout",
			input: ExecuteGoCodeFormatInput{
				Code:             "package main",
				ExecutionTimeout: 0,
			},
			want: "#### [tool call]\n```go\npackage main\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatExecuteGoCodeToolCallMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("FormatExecuteGoCodeToolCallMarkdown() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestFormatExecuteGoCodeResultMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
		want     string
	}{
		{
			name:     "short output",
			content:  "Hello, World!",
			maxLines: 20,
			want:     "#### Code execution output:\n````shell\nHello, World!\n````",
		},
		{
			name:     "output with truncation",
			content:  "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			maxLines: 3,
			want:     "#### Code execution output:\n````shell\nLine 1\nLine 2\nLine 3\n... (truncated)\n````",
		},
		{
			name:     "no truncation when maxLines is 0",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 0,
			want:     "#### Code execution output:\n````shell\nLine 1\nLine 2\nLine 3\n````",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatExecuteGoCodeResultMarkdown(tt.content, tt.maxLines)
			if got != tt.want {
				t.Errorf("FormatExecuteGoCodeResultMarkdown() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestFormatGenericToolCallMarkdown(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantOk       bool
		wantContains string
	}{
		{
			name:         "valid JSON",
			content:      `{"name":"get_weather","parameters":{"city":"NYC"}}`,
			wantOk:       true,
			wantContains: "get_weather",
		},
		{
			name:    "malformed JSON",
			content: `{invalid json`,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := FormatGenericToolCallMarkdown(tt.content)
			if ok != tt.wantOk {
				t.Errorf("FormatGenericToolCallMarkdown() ok = %v, want %v", ok, tt.wantOk)
			}
			if tt.wantOk && !strings.Contains(result, tt.wantContains) {
				t.Errorf("FormatGenericToolCallMarkdown() result doesn't contain %q, got %q", tt.wantContains, result)
			}
		})
	}
}

func TestTruncateToMaxLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
		want     string
	}{
		{
			name:     "content shorter than max",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 5,
			want:     "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "content exactly at max",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 3,
			want:     "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "content longer than max",
			content:  "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			maxLines: 3,
			want:     "Line 1\nLine 2\nLine 3\n... (truncated)",
		},
		{
			name:     "maxLines is 0 - no truncation",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 0,
			want:     "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "empty content",
			content:  "",
			maxLines: 5,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToMaxLines(tt.content, tt.maxLines)
			if got != tt.want {
				t.Errorf("truncateToMaxLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsExecuteGoCodeToolCall(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "is execute_go_code",
			content: `{"name":"execute_go_code","parameters":{"code":"package main"}}`,
			want:    true,
		},
		{
			name:    "is not execute_go_code",
			content: `{"name":"get_weather","parameters":{}}`,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsExecuteGoCodeToolCall(tt.content)
			if got != tt.want {
				t.Errorf("IsExecuteGoCodeToolCall() = %v, want %v", got, tt.want)
			}
		})
	}
}
