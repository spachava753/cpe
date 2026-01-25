package subagentlog

import (
	"time"
)

// Event type constants
const (
	EventTypeToolCall      = "tool_call"
	EventTypeToolResult    = "tool_result"
	EventTypeThoughtTrace  = "thought_trace"
	EventTypeSubagentStart = "subagent_start"
	EventTypeSubagentEnd   = "subagent_end"
)

// TokenUsage holds optional token accounting information
type TokenUsage struct {
	InputTokens      *int `json:"inputTokens,omitempty"`
	OutputTokens     *int `json:"outputTokens,omitempty"`
	TotalTokens      *int `json:"totalTokens,omitempty"`
	CacheReadTokens  *int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int `json:"cacheWriteTokens,omitempty"`
}

// Event represents a subagent logging event streamed to the root CPE process
type Event struct {
	SubagentName            string      `json:"subagentName"`
	SubagentRunID           string      `json:"subagentRunId"`
	Timestamp               time.Time   `json:"timestamp"`
	Type                    string      `json:"type"`
	ToolName                string      `json:"toolName,omitempty"`
	ToolCallID              string      `json:"toolCallId,omitempty"`
	Payload                 string      `json:"payload,omitempty"`
	ExecutionTimeoutSeconds int         `json:"executionTimeoutSeconds,omitempty"`
	ReasoningType           string      `json:"reasoningType,omitempty"`
	TokenUsage              *TokenUsage `json:"tokenUsage,omitempty"`
}
