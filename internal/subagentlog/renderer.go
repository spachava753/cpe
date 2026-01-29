package subagentlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/types"
)

// RenderMode controls the verbosity of rendered subagent events.
// Note: The zero value (RenderModeConcise) is the default, meaning a Renderer{}
// created without explicit mode will use concise rendering.
type RenderMode int

const (
	RenderModeConcise RenderMode = iota // Default: truncated thinking, tool names only, no results
	RenderModeVerbose                   // Full thinking, full tool calls, full results
)

// Renderer formats subagent events for printing to stderr
type Renderer struct {
	markdownRenderer types.Renderer
	mode             RenderMode
}

// NewRenderer creates a new Renderer using the provided markdown renderer
func NewRenderer(markdownRenderer types.Renderer, mode RenderMode) *Renderer {
	return &Renderer{
		markdownRenderer: markdownRenderer,
		mode:             mode,
	}
}

// RenderEvent formats an event with the subagent name prefix and returns
// the rendered string. Returns empty string for events that should be skipped.
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
	if event.ToolName == "final_answer" {
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
		body = header + "\n" + "```go\n" + event.Payload + "\n```"
	} else {
		// Format JSON payload
		formattedPayload := formatJSON(event.Payload)
		body = header + "\n" + "```json\n" + formattedPayload + "\n```"
	}

	return r.render(body)
}

func (r *Renderer) renderToolResult(event Event) string {
	// Skip final_answer tool results to avoid duplicate output
	if event.ToolName == "final_answer" {
		return ""
	}

	var header string
	if event.ToolName == codemode.ExecuteGoCodeToolName {
		header = fmt.Sprintf("#### %s [%s] Code execution output:", event.SubagentName, event.SubagentRunID)
	} else {
		header = fmt.Sprintf("#### %s [%s] Tool \"%s\" result:", event.SubagentName, event.SubagentRunID, event.ToolName)
	}

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
	if event.ToolName == "final_answer" {
		return ""
	}
	// Just show: "#### subagent [runId] → tool_name"
	header := fmt.Sprintf("#### %s [%s] → %s", event.SubagentName, event.SubagentRunID, event.ToolName)
	return r.render(header)
}

func (r *Renderer) renderToolResultConcise(event Event) string {
	// Skip final_answer tool results (consistent with verbose mode)
	if event.ToolName == "final_answer" {
		return ""
	}
	// Tool results are only emitted on success, so show checkmark
	// Format: "#### subagent [runId] ✓ tool_name"
	header := fmt.Sprintf("#### %s [%s] ✓ %s", event.SubagentName, event.SubagentRunID, event.ToolName)
	return r.render(header)
}

func (r *Renderer) renderThoughtTraceConcise(event Event) string {
	header := fmt.Sprintf("#### %s [%s] thought trace", event.SubagentName, event.SubagentRunID)
	// Truncate payload to 2 lines
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

// formatJSON attempts to format JSON payload for readability.
// Returns the original string if parsing fails.
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
