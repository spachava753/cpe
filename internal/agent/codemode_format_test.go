package agent

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
)

func TestParseExecuteGoCodeToolCall(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantOk  bool
	}{
		{
			name:    "valid execute_go_code tool call",
			content: `{"name":"execute_go_code","parameters":{"code":"package main\n\nfunc Run() {}","executionTimeout":30}}`,
			wantOk:  true,
		},
		{
			name:    "valid execute_go_code without timeout",
			content: `{"name":"execute_go_code","parameters":{"code":"package main"}}`,
			wantOk:  true,
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
				cupaloy.SnapshotT(t, input)
			}
		})
	}
}

func TestFormatExecuteGoCodeToolCallMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input ExecuteGoCodeFormatInput
	}{
		{
			name: "with timeout",
			input: ExecuteGoCodeFormatInput{
				Code:             "package main\n\nfunc Run() {}",
				ExecutionTimeout: 30,
			},
		},
		{
			name: "without timeout",
			input: ExecuteGoCodeFormatInput{
				Code:             "package main",
				ExecutionTimeout: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatExecuteGoCodeToolCallMarkdown(tt.input)
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestFormatExecuteGoCodeResultMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
	}{
		{
			name:     "short output",
			content:  "Hello, World!",
			maxLines: 20,
		},
		{
			name:     "output with truncation",
			content:  "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			maxLines: 3,
		},
		{
			name:     "no truncation when maxLines is 0",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatExecuteGoCodeResultMarkdown(tt.content, tt.maxLines)
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestFormatGenericToolCallMarkdown(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantOk  bool
	}{
		{
			name:    "valid JSON",
			content: `{"name":"get_weather","parameters":{"city":"NYC"}}`,
			wantOk:  true,
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
			if tt.wantOk {
				cupaloy.SnapshotT(t, result)
			}
		})
	}
}

func TestTruncateToMaxLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
	}{
		{
			name:     "content shorter than max",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 5,
		},
		{
			name:     "content exactly at max",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 3,
		},
		{
			name:     "content longer than max",
			content:  "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			maxLines: 3,
		},
		{
			name:     "maxLines is 0 - no truncation",
			content:  "Line 1\nLine 2\nLine 3",
			maxLines: 0,
		},
		{
			name:     "empty content",
			content:  "",
			maxLines: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToMaxLines(tt.content, tt.maxLines)
			cupaloy.SnapshotT(t, got)
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
