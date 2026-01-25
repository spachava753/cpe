package subagentlog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"
)

func ptr[T any](v T) *T {
	return &v
}

func TestEventJSONRoundTrip(t *testing.T) {
	fixedTime := time.Date(2026, 1, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event Event
	}{
		{
			name: "all fields populated with tool_call",
			event: Event{
				SubagentName:            "code_reviewer",
				SubagentRunID:           "run-123-abc",
				Timestamp:               fixedTime,
				Type:                    EventTypeToolCall,
				ToolName:                "execute_go_code",
				ToolCallID:              "call-456",
				Payload:                 `{"code":"fmt.Println(\"hello\")"}`,
				ExecutionTimeoutSeconds: 30,
				ReasoningType:           "",
				TokenUsage: &TokenUsage{
					InputTokens:      ptr(100),
					OutputTokens:     ptr(50),
					TotalTokens:      ptr(150),
					CacheReadTokens:  ptr(20),
					CacheWriteTokens: ptr(10),
				},
			},
		},
		{
			name: "all fields populated with tool_result",
			event: Event{
				SubagentName:  "code_reviewer",
				SubagentRunID: "run-123-abc",
				Timestamp:     fixedTime,
				Type:          EventTypeToolResult,
				ToolName:      "execute_go_code",
				ToolCallID:    "call-456",
				Payload:       `{"result":"success"}`,
				TokenUsage: &TokenUsage{
					InputTokens:  ptr(100),
					OutputTokens: ptr(50),
				},
			},
		},
		{
			name: "thought_trace event",
			event: Event{
				SubagentName:  "code_reviewer",
				SubagentRunID: "run-789",
				Timestamp:     fixedTime,
				Type:          EventTypeThoughtTrace,
				ReasoningType: "reasoning.encrypted",
				Payload:       "encrypted-thinking-content",
			},
		},
		{
			name: "subagent_start event",
			event: Event{
				SubagentName:  "code_reviewer",
				SubagentRunID: "run-start-001",
				Timestamp:     fixedTime,
				Type:          EventTypeSubagentStart,
				Payload:       `{"task":"review code"}`,
			},
		},
		{
			name: "subagent_end event",
			event: Event{
				SubagentName:  "code_reviewer",
				SubagentRunID: "run-end-001",
				Timestamp:     fixedTime,
				Type:          EventTypeSubagentEnd,
				Payload:       `{"status":"completed"}`,
				TokenUsage: &TokenUsage{
					TotalTokens: ptr(500),
				},
			},
		},
		{
			name: "minimal fields only",
			event: Event{
				SubagentName:  "minimal_agent",
				SubagentRunID: "run-min",
				Timestamp:     fixedTime,
				Type:          EventTypeToolCall,
			},
		},
		{
			name: "nil TokenUsage",
			event: Event{
				SubagentName:  "test_agent",
				SubagentRunID: "run-nil-tokens",
				Timestamp:     fixedTime,
				Type:          EventTypeToolResult,
				ToolName:      "some_tool",
				Payload:       "result data",
				TokenUsage:    nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			// Unmarshal back
			var got Event
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			// Use cupaloy to snapshot the JSON output and the round-tripped event
			cupaloy.SnapshotT(t, string(data), got)
		})
	}
}

func TestEventTypeConstants(t *testing.T) {
	// Snapshot all event type constants together
	constants := map[string]string{
		"EventTypeToolCall":      EventTypeToolCall,
		"EventTypeToolResult":    EventTypeToolResult,
		"EventTypeThoughtTrace":  EventTypeThoughtTrace,
		"EventTypeSubagentStart": EventTypeSubagentStart,
		"EventTypeSubagentEnd":   EventTypeSubagentEnd,
	}
	cupaloy.SnapshotT(t, constants)
}

func TestTokenUsageJSONOmitsEmpty(t *testing.T) {
	// Test that empty TokenUsage fields are omitted
	usage := TokenUsage{
		InputTokens: ptr(100),
		// Other fields nil
	}

	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	cupaloy.SnapshotT(t, string(data))
}

func TestEventJSONOmitsEmpty(t *testing.T) {
	fixedTime := time.Date(2026, 1, 24, 12, 0, 0, 0, time.UTC)

	event := Event{
		SubagentName:  "test",
		SubagentRunID: "run-1",
		Timestamp:     fixedTime,
		Type:          EventTypeToolCall,
		// Optional fields not set
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal to map to check which fields are present
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	// Snapshot the JSON output and the map of present fields
	cupaloy.SnapshotT(t, string(data), m)
}
