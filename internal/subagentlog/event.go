package subagentlog

import "time"

// Event type constants define the wire-level values for Event.Type.
// These strings are consumed by the parent-process renderer, so they are
// part of the logging protocol contract and should remain stable.
const (
	// EventTypeToolCall is emitted after the model returns a tool call block.
	EventTypeToolCall = "tool_call"
	// EventTypeToolResult is emitted when a tool result message is observed in dialog.
	EventTypeToolResult = "tool_result"
	// EventTypeThoughtTrace is emitted for model thinking blocks.
	EventTypeThoughtTrace = "thought_trace"
	// EventTypeSubagentStart marks the beginning of one subagent run.
	EventTypeSubagentStart = "subagent_start"
	// EventTypeSubagentEnd marks the end of one subagent run.
	EventTypeSubagentEnd = "subagent_end"
)

// TokenUsage carries optional token accounting metadata for events that include it.
// All fields are pointers so senders can omit unknown provider-specific counters.
type TokenUsage struct {
	InputTokens      *int `json:"inputTokens,omitempty"`
	OutputTokens     *int `json:"outputTokens,omitempty"`
	TotalTokens      *int `json:"totalTokens,omitempty"`
	CacheReadTokens  *int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int `json:"cacheWriteTokens,omitempty"`
}

// Event is the canonical payload posted from subagent processes to the local
// logging server. Required fields (subagentName, subagentRunId, timestamp, type)
// identify one lifecycle/tool/thinking event. Optional fields are interpreted by
// event type:
//   - tool_call: toolName, toolCallId, payload (tool input), executionTimeoutSeconds
//   - tool_result: toolName, toolCallId, payload (tool output text)
//   - thought_trace: payload (thinking text), reasoningType (if provided by model)
//   - subagent_end: payload may contain terminal error text
//
// The JSON field names are part of the over-the-wire contract used by the root
// process and should be treated as stable.
type Event struct {
	// SubagentName is the display name for the emitting subagent tool.
	SubagentName string `json:"subagentName"`
	// SubagentRunID correlates all events for a single MCP tool invocation.
	SubagentRunID string `json:"subagentRunId"`
	// Timestamp is set by the emitter when the event is created.
	Timestamp time.Time `json:"timestamp"`
	// Type identifies the event kind and selects payload interpretation.
	Type string `json:"type"`
	// ToolName is set for tool_call/tool_result events.
	ToolName string `json:"toolName,omitempty"`
	// ToolCallID links tool_result back to the originating tool_call block ID.
	ToolCallID string `json:"toolCallId,omitempty"`
	// Payload carries event-specific text content (code, JSON params, output, thinking).
	Payload string `json:"payload,omitempty"`
	// ExecutionTimeoutSeconds is populated for tool calls that declare a timeout.
	ExecutionTimeoutSeconds int `json:"executionTimeoutSeconds,omitempty"`
	// ReasoningType captures provider-specific reasoning metadata when available.
	ReasoningType string `json:"reasoningType,omitempty"`
	// TokenUsage optionally reports model token accounting for the event.
	TokenUsage *TokenUsage `json:"tokenUsage,omitempty"`
}
