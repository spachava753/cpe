package subagentlog

import (
	"testing"
	"time"
)

// mockRenderer is a simple renderer that returns content as-is for testing
type mockRenderer struct{}

func (m *mockRenderer) Render(in string) (string, error) {
	return in, nil
}

func TestRenderEvent(t *testing.T) {
	renderer := NewRenderer(&mockRenderer{})
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name: "tool call without timeout",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeToolCall,
				ToolName:      "web_search",
				ToolCallID:    "call-1",
				Payload:       `{"query":"golang tutorials"}`,
			},
			want: "#### research_agent [run-123] [tool call]\n" +
				"```json\n" +
				"{\n  \"query\": \"golang tutorials\"\n}" +
				"\n```",
		},
		{
			name: "tool call with timeout",
			event: Event{
				SubagentName:            "research_agent",
				SubagentRunID:           "run-123",
				Timestamp:               baseTime,
				Type:                    EventTypeToolCall,
				ToolName:                "web_search",
				ToolCallID:              "call-1",
				Payload:                 `{"query":"test"}`,
				ExecutionTimeoutSeconds: 30,
			},
			want: "#### research_agent [run-123] [tool call] (timeout: 30s)\n" +
				"```json\n" +
				"{\n  \"query\": \"test\"\n}" +
				"\n```",
		},
		{
			name: "execute_go_code tool call",
			event: Event{
				SubagentName:            "code_agent",
				SubagentRunID:           "run-456",
				Timestamp:               baseTime,
				Type:                    EventTypeToolCall,
				ToolName:                "execute_go_code",
				ToolCallID:              "call-2",
				Payload:                 "fmt.Println(\"hello\")",
				ExecutionTimeoutSeconds: 60,
			},
			want: "#### code_agent [run-456] [tool call] (timeout: 60s)\n" +
				"```go\n" +
				"fmt.Println(\"hello\")" +
				"\n```",
		},
		{
			name: "execute_go_code tool call without explicit timeout shows 0",
			event: Event{
				SubagentName:  "code_agent",
				SubagentRunID: "run-456",
				Timestamp:     baseTime,
				Type:          EventTypeToolCall,
				ToolName:      "execute_go_code",
				ToolCallID:    "call-2",
				Payload:       "fmt.Println(\"test\")",
			},
			want: "#### code_agent [run-456] [tool call] (timeout: 0s)\n" +
				"```go\n" +
				"fmt.Println(\"test\")" +
				"\n```",
		},
		{
			name: "final_answer tool call is skipped",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeToolCall,
				ToolName:      "final_answer",
				ToolCallID:    "call-3",
				Payload:       `{"result":"done"}`,
			},
			want: "",
		},
		{
			name: "final_answer tool result is skipped",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeToolResult,
				ToolName:      "final_answer",
				ToolCallID:    "call-3",
				Payload:       "The final answer output",
			},
			want: "",
		},
		{
			name: "tool result",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeToolResult,
				ToolName:      "web_search",
				ToolCallID:    "call-1",
				Payload:       "Search completed successfully",
			},
			want: "#### research_agent [run-123] Tool \"web_search\" result:\n" +
				"```shell\n" +
				"Search completed successfully" +
				"\n```",
		},
		{
			name: "code execution output",
			event: Event{
				SubagentName:  "code_agent",
				SubagentRunID: "run-456",
				Timestamp:     baseTime,
				Type:          EventTypeToolResult,
				ToolName:      "execute_go_code",
				ToolCallID:    "call-2",
				Payload:       "hello\nworld",
			},
			want: "#### code_agent [run-456] Code execution output:\n" +
				"```shell\n" +
				"hello\nworld" +
				"\n```",
		},
		{
			name: "thought trace",
			event: Event{
				SubagentName:  "thinking_agent",
				SubagentRunID: "run-789",
				Timestamp:     baseTime,
				Type:          EventTypeThoughtTrace,
				Payload:       "Let me analyze this problem step by step...",
			},
			want: "#### thinking_agent [run-789] thought trace\n" +
				"Let me analyze this problem step by step...",
		},
		{
			name: "subagent start is skipped",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeSubagentStart,
			},
			want: "",
		},
		{
			name: "subagent end is skipped",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeSubagentEnd,
			},
			want: "",
		},
		{
			name: "unknown event type is skipped",
			event: Event{
				SubagentName:  "agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          "unknown_type",
				Payload:       "some payload",
			},
			want: "",
		},
		{
			name: "tool call with invalid JSON payload",
			event: Event{
				SubagentName:  "agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeToolCall,
				ToolName:      "some_tool",
				Payload:       "not valid json",
			},
			want: "#### agent [run-123] [tool call]\n" +
				"```json\n" +
				"not valid json" +
				"\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderer.RenderEvent(tt.event)
			if got != tt.want {
				t.Errorf("RenderEvent() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestNewRenderer(t *testing.T) {
	mock := &mockRenderer{}
	r := NewRenderer(mock)
	if r == nil {
		t.Error("NewRenderer returned nil")
	}
	if r.markdownRenderer != mock {
		t.Error("NewRenderer did not set markdownRenderer correctly")
	}
}

func TestFormatJSON(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "valid JSON object",
			payload: `{"key":"value","num":42}`,
			want:    "{\n  \"key\": \"value\",\n  \"num\": 42\n}",
		},
		{
			name:    "valid JSON array",
			payload: `[1,2,3]`,
			want:    "[\n  1,\n  2,\n  3\n]",
		},
		{
			name:    "invalid JSON returns original",
			payload: "not json",
			want:    "not json",
		},
		{
			name:    "empty string returns original",
			payload: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatJSON(tt.payload)
			if got != tt.want {
				t.Errorf("formatJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}
