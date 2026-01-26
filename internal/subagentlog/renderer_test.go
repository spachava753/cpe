package subagentlog

import (
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"
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
		},
		{
			name: "subagent start is skipped",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeSubagentStart,
			},
		},
		{
			name: "subagent end is skipped",
			event: Event{
				SubagentName:  "research_agent",
				SubagentRunID: "run-123",
				Timestamp:     baseTime,
				Type:          EventTypeSubagentEnd,
			},
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderer.RenderEvent(tt.event)
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestNewRenderer(t *testing.T) {
	mock := &mockRenderer{}
	r := NewRenderer(mock)
	if r == nil {
		t.Fatal("NewRenderer returned nil")
	}
	if r.markdownRenderer != mock {
		t.Error("NewRenderer did not set markdownRenderer correctly")
	}
}

func TestFormatJSON(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "valid JSON object",
			payload: `{"key":"value","num":42}`,
		},
		{
			name:    "valid JSON array",
			payload: `[1,2,3]`,
		},
		{
			name:    "invalid JSON returns original",
			payload: "not json",
		},
		{
			name:    "empty string returns original",
			payload: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatJSON(tt.payload)
			cupaloy.SnapshotT(t, got)
		})
	}
}
