package subagentlog

import (
	"encoding/json"
	"testing"
	"time"
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

			// Compare fields
			if got.SubagentName != tt.event.SubagentName {
				t.Errorf("SubagentName: got %q, want %q", got.SubagentName, tt.event.SubagentName)
			}
			if got.SubagentRunID != tt.event.SubagentRunID {
				t.Errorf("SubagentRunID: got %q, want %q", got.SubagentRunID, tt.event.SubagentRunID)
			}
			if !got.Timestamp.Equal(tt.event.Timestamp) {
				t.Errorf("Timestamp: got %v, want %v", got.Timestamp, tt.event.Timestamp)
			}
			if got.Type != tt.event.Type {
				t.Errorf("Type: got %q, want %q", got.Type, tt.event.Type)
			}
			if got.ToolName != tt.event.ToolName {
				t.Errorf("ToolName: got %q, want %q", got.ToolName, tt.event.ToolName)
			}
			if got.ToolCallID != tt.event.ToolCallID {
				t.Errorf("ToolCallID: got %q, want %q", got.ToolCallID, tt.event.ToolCallID)
			}
			if got.Payload != tt.event.Payload {
				t.Errorf("Payload: got %q, want %q", got.Payload, tt.event.Payload)
			}
			if got.ExecutionTimeoutSeconds != tt.event.ExecutionTimeoutSeconds {
				t.Errorf("ExecutionTimeoutSeconds: got %d, want %d", got.ExecutionTimeoutSeconds, tt.event.ExecutionTimeoutSeconds)
			}
			if got.ReasoningType != tt.event.ReasoningType {
				t.Errorf("ReasoningType: got %q, want %q", got.ReasoningType, tt.event.ReasoningType)
			}

			// Compare TokenUsage
			if tt.event.TokenUsage == nil {
				if got.TokenUsage != nil {
					t.Errorf("TokenUsage: got %v, want nil", got.TokenUsage)
				}
			} else {
				if got.TokenUsage == nil {
					t.Fatalf("TokenUsage: got nil, want %v", tt.event.TokenUsage)
				}
				compareIntPtr(t, "InputTokens", got.TokenUsage.InputTokens, tt.event.TokenUsage.InputTokens)
				compareIntPtr(t, "OutputTokens", got.TokenUsage.OutputTokens, tt.event.TokenUsage.OutputTokens)
				compareIntPtr(t, "TotalTokens", got.TokenUsage.TotalTokens, tt.event.TokenUsage.TotalTokens)
				compareIntPtr(t, "CacheReadTokens", got.TokenUsage.CacheReadTokens, tt.event.TokenUsage.CacheReadTokens)
				compareIntPtr(t, "CacheWriteTokens", got.TokenUsage.CacheWriteTokens, tt.event.TokenUsage.CacheWriteTokens)
			}
		})
	}
}

func compareIntPtr(t *testing.T, name string, got, want *int) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s: got %v, want nil", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s: got nil, want %d", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s: got %d, want %d", name, *got, *want)
	}
}

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{
			name:     "EventTypeToolCall",
			constant: EventTypeToolCall,
			want:     "tool_call",
		},
		{
			name:     "EventTypeToolResult",
			constant: EventTypeToolResult,
			want:     "tool_result",
		},
		{
			name:     "EventTypeThoughtTrace",
			constant: EventTypeThoughtTrace,
			want:     "thought_trace",
		},
		{
			name:     "EventTypeSubagentStart",
			constant: EventTypeSubagentStart,
			want:     "subagent_start",
		},
		{
			name:     "EventTypeSubagentEnd",
			constant: EventTypeSubagentEnd,
			want:     "subagent_end",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("got %q, want %q", tt.constant, tt.want)
			}
		})
	}
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

	// Should only contain inputTokens
	expected := `{"inputTokens":100}`
	if string(data) != expected {
		t.Errorf("got %s, want %s", string(data), expected)
	}
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

	// Required fields should be present
	requiredFields := []string{"subagentName", "subagentRunId", "timestamp", "type"}
	for _, f := range requiredFields {
		if _, ok := m[f]; !ok {
			t.Errorf("required field %q missing from JSON", f)
		}
	}

	// Optional fields should be absent
	optionalFields := []string{"toolName", "toolCallId", "payload", "executionTimeoutSeconds", "reasoningType", "tokenUsage"}
	for _, f := range optionalFields {
		if _, ok := m[f]; ok {
			t.Errorf("optional field %q should be omitted when empty, but was present", f)
		}
	}
}
