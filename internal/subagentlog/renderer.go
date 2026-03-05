package subagentlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/types"
)

// RenderMode controls how much event detail is printed for subagent logs.
//
// The zero value is RenderModeConcise, so Renderer{} defaults to compact output.
type RenderMode int

const (
	// RenderModeConcise prints progress-only lines suitable for frequent updates:
	// tool starts, successful tool completions, and abbreviated thought traces.
	RenderModeConcise RenderMode = iota
	// RenderModeVerbose prints full payloads (tool inputs/results and full thoughts).
	RenderModeVerbose
)

// Renderer converts Event values into markdown-like log blocks intended for
// stderr rendering in the parent CPE process.
//
// Lifecycle events (subagent_start/subagent_end) are intentionally suppressed
// here; the renderer focuses on streaming progress details.
type Renderer struct {
	markdownRenderer types.Renderer
	mode             RenderMode
}

// NewRenderer creates a renderer with the provided markdown renderer and mode.
func NewRenderer(markdownRenderer types.Renderer, mode RenderMode) *Renderer {
	return &Renderer{
		markdownRenderer: markdownRenderer,
		mode:             mode,
	}
}

// RenderEvent renders one event according to mode-specific conventions.
//
// Rendering contract:
//   - subagent_start/subagent_end are skipped (empty string).
//   - final_answer tool call/result events are skipped to avoid duplicate final output.
//   - execute_go_code calls/results use specialized headers and fenced code formatting.
//   - Unknown event types are ignored (empty string).
func (r *Renderer) RenderEvent(event Event) string {
	switch event.Type {
	case EventTypeSubagentStart, EventTypeSubagentEnd:
		return ""
	case EventTypeToolCall:
		if r.mode == RenderModeConcise {
			return r.renderToolCallConcise(event)
		}
		return r.renderToolCall(event)
	case EventTypeToolResult:
		if r.mode == RenderModeConcise {
			return r.renderToolResultConcise(event)
		}
		return r.renderToolResult(event)
	case EventTypeThoughtTrace:
		if r.mode == RenderModeConcise {
			return r.renderThoughtTraceConcise(event)
		}
		return r.renderThoughtTrace(event)
	default:
		return ""
	}
}

func (r *Renderer) renderToolCall(event Event) string {
	// Skip final_answer tool calls
	if event.ToolName == finalAnswerToolName {
		return ""
	}

	// Build header
	var header string
	if event.ToolName == codemode.ExecuteGoCodeToolName {
		// execute_go_code always shows timeout
		header = fmt.Sprintf("#### %s [%s] [tool call] (timeout: %ds)", event.SubagentName, event.SubagentRunID, event.ExecutionTimeoutSeconds)
	} else if event.ExecutionTimeoutSeconds > 0 {
		header = fmt.Sprintf("#### %s [%s] [tool call] (timeout: %ds)", event.SubagentName, event.SubagentRunID, event.ExecutionTimeoutSeconds)
	} else {
		header = fmt.Sprintf("#### %s [%s] [tool call]", event.SubagentName, event.SubagentRunID)
	}

	// Build body with appropriate code block
	var body string
	if event.ToolName == codemode.ExecuteGoCodeToolName {
		// execute_go_code payloads are rendered as Go source for readability.
		body = header + "\n" + "```go\n" + event.Payload + "\n```"
	} else {
		// Non-code tool inputs are expected to be JSON object parameters.
		formattedPayload := formatJSON(event.Payload)
		body = header + "\n" + "```json\n" + formattedPayload + "\n```"
	}

	return r.render(body)
}

func (r *Renderer) renderToolResult(event Event) string {
	// Skip final_answer tool results to avoid duplicate output
	if event.ToolName == finalAnswerToolName {
		return ""
	}

	var header string
	if event.ToolName == codemode.ExecuteGoCodeToolName {
		header = fmt.Sprintf("#### %s [%s] Code execution output:", event.SubagentName, event.SubagentRunID)
	} else {
		header = fmt.Sprintf("#### %s [%s] Tool \"%s\" result:", event.SubagentName, event.SubagentRunID, event.ToolName)
	}

	// Tool results are rendered in a shell fence to preserve raw command/tool output.
	body := header + "\n" + "```shell\n" + event.Payload + "\n```"

	return r.render(body)
}

func (r *Renderer) renderThoughtTrace(event Event) string {
	header := fmt.Sprintf("#### %s [%s] thought trace", event.SubagentName, event.SubagentRunID)
	body := header + "\n" + event.Payload
	return r.render(body)
}

func (r *Renderer) renderToolCallConcise(event Event) string {
	// Skip final_answer tool calls (consistent with verbose mode)
	if event.ToolName == finalAnswerToolName {
		return ""
	}
	// Concise format: "#### subagent [runId] → tool_name"
	header := fmt.Sprintf("#### %s [%s] → %s", event.SubagentName, event.SubagentRunID, event.ToolName)
	return r.render(header)
}

func (r *Renderer) renderToolResultConcise(event Event) string {
	// Skip final_answer tool results (consistent with verbose mode)
	if event.ToolName == finalAnswerToolName {
		return ""
	}
	// Tool results are emitted only after successful completion, so concise mode uses
	// a success checkmark: "#### subagent [runId] ✓ tool_name".
	header := fmt.Sprintf("#### %s [%s] ✓ %s", event.SubagentName, event.SubagentRunID, event.ToolName)
	return r.render(header)
}

func (r *Renderer) renderThoughtTraceConcise(event Event) string {
	header := fmt.Sprintf("#### %s [%s] thought trace", event.SubagentName, event.SubagentRunID)
	// Truncate payload to at most two lines for compact progress output.
	lines := strings.SplitN(event.Payload, "\n", 3)
	truncated := strings.Join(lines[:min(2, len(lines))], "\n")
	if len(lines) > 2 {
		truncated += "..."
	}
	body := header + "\n" + truncated
	return r.render(body)
}

func (r *Renderer) render(content string) string {
	rendered, err := r.markdownRenderer.Render(content)
	if err != nil {
		return content
	}
	return rendered
}

// formatJSON pretty-prints tool parameter payloads when they are valid JSON.
// Invalid JSON is returned unchanged so rendering remains lossless.
func formatJSON(payload string) string {
	var data interface{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return payload
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(payload), "", "  "); err != nil {
		return payload
	}
	return buf.String()
}
