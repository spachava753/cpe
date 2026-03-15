package subagentlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/ports"
)

// finalAnswerToolName is filtered from streamed events because it is a terminal
// structured-output mechanism, not user-facing intermediate progress.
const (
	finalAnswerToolName = "final_answer"
	unknownToolName     = "unknown"
)

// EmittingGenerator wraps a generator to emit subagent logging events.
//
// Primary contract:
//   - Emit thought_trace, tool_call, and tool_result events during generation.
//   - Preserve chronological event ordering across tool-use iterations.
//   - Treat emission failures as fatal (return error) so runs do not continue silently.
//   - Suppress final_answer tool call/result events to avoid duplicate terminal output.
//
// The preferred path wraps an inner *gai.ToolGenerator so events are emitted at each
// Generate boundary. A fallback path exists for non-ToolGenerator implementations.
type EmittingGenerator struct {
	base               ports.Generator
	client             *Client
	subagentName       string
	runID              string
	hasWrappedInnerGen bool // true if we successfully wrapped the inner generator
}

// NewEmittingGenerator creates an emitting wrapper around base.
//
// It recursively unwraps decorator generators to find an underlying
// *gai.ToolGenerator. When found, the inner ToolCapableGenerator is wrapped so
// event emission happens at per-iteration boundaries (correct ordering). If no
// ToolGenerator is found, the returned EmittingGenerator still works using a
// fallback post-generation scan path.
func NewEmittingGenerator(base ports.Generator, client *Client, subagentName, runID string) *EmittingGenerator {
	hasWrappedInnerGen := false

	// Unwrap the generator chain to find the underlying *gai.ToolGenerator
	if toolGen := findToolGenerator(base); toolGen != nil {
		if wrappedG, ok := toolGen.G.(*emittingMiddleware); ok {
			wrappedG.client = client
			wrappedG.subagentName = subagentName
			wrappedG.runID = runID
		} else {
			toolGen.G = &emittingMiddleware{
				base:         toolGen.G,
				client:       client,
				subagentName: subagentName,
				runID:        runID,
			}
		}
		hasWrappedInnerGen = true
	}

	return &EmittingGenerator{
		base:               base,
		client:             client,
		subagentName:       subagentName,
		runID:              runID,
		hasWrappedInnerGen: hasWrappedInnerGen,
	}
}

// findToolGenerator recursively unwraps generators exposing Inner() until it
// locates a *gai.ToolGenerator, or returns nil when no such node exists.
func findToolGenerator(gen interface{}) *gai.ToolGenerator {
	if tg, ok := gen.(*gai.ToolGenerator); ok {
		return tg
	}
	if wrapper, ok := gen.(interface{ Inner() ports.Generator }); ok {
		return findToolGenerator(wrapper.Inner())
	}
	return nil
}

// emittingMiddleware emits all non-lifecycle subagent events around each
// ToolCapableGenerator.Generate call.
//
// Ordering contract per iteration:
//   - BEFORE inner Generate: emit tool_result events now present in dialog.
//   - AFTER inner Generate: emit thought_trace events from response blocks.
//   - AFTER thought traces: emit tool_call events from response blocks.
//
// Any emission failure aborts generation and is returned to the caller so subagent
// execution stops rather than silently losing observability.
type emittingMiddleware struct {
	base         gai.ToolCapableGenerator
	client       *Client
	subagentName string
	runID        string
}

// Generate wraps one model iteration with ordered event emission.
//
// Sequence:
//  1. Emit tool_result events for newly appended tool-result messages.
//  2. Call the wrapped generator.
//  3. Emit thought_trace events from returned thinking blocks.
//  4. Emit tool_call events from returned tool-call blocks.
//
// If any emit step fails, generation stops immediately and the error is returned.
func (m *emittingMiddleware) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	// BEFORE: Emit tool_result events for tool results after the last assistant message.
	// These are the "new" tool results from the previous iteration's tool execution.
	if err := m.emitToolResultEvents(ctx, dialog); err != nil {
		return gai.Response{}, err
	}

	// Call the inner generator
	resp, err := m.base.Generate(ctx, dialog, options)
	if err != nil {
		return resp, err
	}

	// AFTER: Emit events for blocks in the response
	if err := m.emitResponseEvents(ctx, resp); err != nil {
		return gai.Response{}, err
	}

	return resp, nil
}

// emitToolResultEvents emits tool_result events for tool-result messages that
// appear after the most recent assistant message.
//
// This maps to "results from the previous iteration" in tool-use loops. Results
// for final_answer are intentionally skipped.
func (m *emittingMiddleware) emitToolResultEvents(ctx context.Context, dialog gai.Dialog) error {
	lastAssistantIdx := -1
	for i := len(dialog) - 1; i >= 0; i-- {
		if dialog[i].Role == gai.Assistant {
			lastAssistantIdx = i
			break
		}
	}

	if lastAssistantIdx < 0 {
		return nil
	}

	assistantMsg := dialog[lastAssistantIdx]

	for i := lastAssistantIdx + 1; i < len(dialog); i++ {
		msg := dialog[i]
		if msg.Role != gai.ToolResult {
			continue
		}

		// gai ensures each tool result message has exactly one block
		if len(msg.Blocks) == 0 {
			continue
		}

		block := msg.Blocks[0]
		toolCallID := block.ID
		toolName := m.findToolNameByCallID(assistantMsg, toolCallID)

		if toolName == finalAnswerToolName {
			continue
		}

		resultText := formatToolResultPayload(msg.Blocks)

		event := Event{
			SubagentName:  m.subagentName,
			SubagentRunID: m.runID,
			Timestamp:     time.Now(),
			Type:          EventTypeToolResult,
			ToolName:      toolName,
			ToolCallID:    toolCallID,
			Payload:       resultText,
		}

		if err := m.client.Emit(ctx, event); err != nil {
			return fmt.Errorf("failed to emit tool_result event: %w", err)
		}
	}

	return nil
}

// emitResponseEvents emits post-generation events from response blocks in order:
// thought_trace first, then tool_call, preserving the per-candidate block sequence.
func (m *emittingMiddleware) emitResponseEvents(ctx context.Context, resp gai.Response) error {
	for _, candidate := range resp.Candidates {
		for _, block := range candidate.Blocks {
			switch block.BlockType {
			case gai.Thinking:
				if err := m.emitThinkingEvent(ctx, block); err != nil {
					return err
				}
			case gai.ToolCall:
				if err := m.emitToolCallEvent(ctx, block); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// emitThinkingEvent emits a thought_trace event for one thinking block,
// copying provider-specific reasoning_type metadata when present.
func (m *emittingMiddleware) emitThinkingEvent(ctx context.Context, block gai.Block) error {
	event := Event{
		SubagentName:  m.subagentName,
		SubagentRunID: m.runID,
		Timestamp:     time.Now(),
		Type:          EventTypeThoughtTrace,
		Payload:       block.Content.String(),
	}

	if block.ExtraFields != nil {
		if reasoningType, ok := block.ExtraFields["reasoning_type"].(string); ok {
			event.ReasoningType = reasoningType
		}
	}

	if err := m.client.Emit(ctx, event); err != nil {
		return fmt.Errorf("failed to emit thought_trace event: %w", err)
	}

	return nil
}

// emitToolCallEvent emits a tool_call event for one tool call block.
//
// Malformed tool-call JSON is treated as recoverable and skipped. final_answer is
// intentionally filtered out. execute_go_code emits raw Go source + timeout,
// while other tools emit JSON-serialized parameters.
func (m *emittingMiddleware) emitToolCallEvent(ctx context.Context, block gai.Block) error {
	var toolCall gai.ToolCallInput
	// Skip malformed tool calls - this is a recoverable error
	if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err != nil {
		return nil //nolint:nilerr // intentionally ignoring unmarshal errors for malformed tool calls
	}

	if toolCall.Name == finalAnswerToolName {
		return nil
	}

	event := Event{
		SubagentName:  m.subagentName,
		SubagentRunID: m.runID,
		Timestamp:     time.Now(),
		Type:          EventTypeToolCall,
		ToolName:      toolCall.Name,
		ToolCallID:    block.ID,
	}

	if toolCall.Name == codemode.ExecuteGoCodeToolName {
		if code, ok := toolCall.Parameters["code"].(string); ok {
			event.Payload = code
		}
		if timeout, ok := toolCall.Parameters["executionTimeout"]; ok {
			switch t := timeout.(type) {
			case float64:
				event.ExecutionTimeoutSeconds = int(t)
			case int:
				event.ExecutionTimeoutSeconds = t
			}
		}
	} else {
		if paramsJSON, err := json.Marshal(toolCall.Parameters); err == nil {
			event.Payload = string(paramsJSON)
		}
	}

	if err := m.client.Emit(ctx, event); err != nil {
		return fmt.Errorf("failed to emit tool_call event: %w", err)
	}

	return nil
}

// findToolNameByCallID resolves a tool call ID to a tool name using the
// assistant tool-call blocks from the preceding assistant message.
// Returns "unknown" when parsing or lookup fails.
func (m *emittingMiddleware) findToolNameByCallID(assistantMsg gai.Message, toolCallID string) string {
	return findToolNameByCallID(assistantMsg, toolCallID)
}

func findToolNameByCallID(assistantMsg gai.Message, toolCallID string) string {
	for _, block := range assistantMsg.Blocks {
		if block.BlockType != gai.ToolCall || block.ID != toolCallID {
			continue
		}

		var toolCall gai.ToolCallInput
		if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err == nil && toolCall.Name != "" {
			return toolCall.Name
		}
		return unknownToolName
	}
	return unknownToolName
}

func findNearestPrecedingAssistant(dialog gai.Dialog, before int) (gai.Message, bool) {
	for i := before - 1; i >= 0; i-- {
		if dialog[i].Role == gai.Assistant {
			return dialog[i], true
		}
	}
	return gai.Message{}, false
}

func formatToolResultPayload(blocks []gai.Block) string {
	if len(blocks) == 0 {
		return ""
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.ModalityType == gai.Text {
			parts = append(parts, block.Content.String())
			continue
		}

		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = block.ModalityType.String()
		}
		parts = append(parts, fmt.Sprintf("[%s content]", mimeType))
	}
	return strings.Join(parts, "\n\n")
}

// Register passes tool registration through unchanged; emission is handled at
// Generate boundaries, not by callback wrapping.
func (m *emittingMiddleware) Register(tool gai.Tool) error {
	return m.base.Register(tool)
}

// Generate delegates to the wrapped generator while preventing duplicate events.
//
// If NewEmittingGenerator successfully wrapped an inner *gai.ToolGenerator,
// middleware emission has already occurred in correct chronological order and this
// method simply returns the generated dialog.
//
// Otherwise, this fallback path scans newly appended messages and emits events
// after generation (used by non-ToolGenerator implementations, including tests).
func (g *EmittingGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	originalLen := len(dialog)

	resultDialog, err := g.base.Generate(ctx, dialog, optsGen)
	if err != nil {
		return resultDialog, err
	}

	// If we wrapped the inner generator, events are already emitted
	// at the right time. Skip to avoid duplicates.
	if g.hasWrappedInnerGen {
		return resultDialog, nil
	}

	// Fallback: emit events here (after full generation, for non-ToolGenerator bases)
	// This is used when the base is not a *gai.ToolGenerator (e.g., in tests with mockGenerator)
	for offset, msg := range resultDialog[originalLen:] {
		if msg.Role == gai.Assistant {
			for _, block := range msg.Blocks {
				switch block.BlockType {
				case gai.Thinking:
					event := Event{
						SubagentName:  g.subagentName,
						SubagentRunID: g.runID,
						Timestamp:     time.Now(),
						Type:          EventTypeThoughtTrace,
						Payload:       block.Content.String(),
					}
					if block.ExtraFields != nil {
						if reasoningType, ok := block.ExtraFields["reasoning_type"].(string); ok {
							event.ReasoningType = reasoningType
						}
					}
					if err := g.client.Emit(ctx, event); err != nil {
						return nil, fmt.Errorf("failed to emit thought_trace event: %w", err)
					}
				case gai.ToolCall:
					var toolCall gai.ToolCallInput
					if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err != nil {
						continue
					}
					if toolCall.Name == finalAnswerToolName {
						continue
					}
					event := Event{
						SubagentName:  g.subagentName,
						SubagentRunID: g.runID,
						Timestamp:     time.Now(),
						Type:          EventTypeToolCall,
						ToolName:      toolCall.Name,
						ToolCallID:    block.ID,
					}
					if toolCall.Name == codemode.ExecuteGoCodeToolName {
						if code, ok := toolCall.Parameters["code"].(string); ok {
							event.Payload = code
						}
						if timeout, ok := toolCall.Parameters["executionTimeout"]; ok {
							switch t := timeout.(type) {
							case float64:
								event.ExecutionTimeoutSeconds = int(t)
							case int:
								event.ExecutionTimeoutSeconds = t
							}
						}
					} else {
						if paramsJSON, err := json.Marshal(toolCall.Parameters); err == nil {
							event.Payload = string(paramsJSON)
						}
					}
					if err := g.client.Emit(ctx, event); err != nil {
						return nil, fmt.Errorf("failed to emit tool_call event: %w", err)
					}
				}
			}
		} else if msg.Role == gai.ToolResult {
			if len(msg.Blocks) == 0 {
				continue
			}

			toolName := "unknown"
			if assistantMsg, ok := findNearestPrecedingAssistant(resultDialog, originalLen+offset); ok {
				toolName = findToolNameByCallID(assistantMsg, msg.Blocks[0].ID)
			}
			if toolName == finalAnswerToolName {
				continue
			}
			resultText := formatToolResultPayload(msg.Blocks)
			event := Event{
				SubagentName:  g.subagentName,
				SubagentRunID: g.runID,
				Timestamp:     time.Now(),
				Type:          EventTypeToolResult,
				ToolName:      toolName,
				ToolCallID:    msg.Blocks[0].ID,
				Payload:       resultText,
			}
			if err := g.client.Emit(ctx, event); err != nil {
				return nil, fmt.Errorf("failed to emit tool_result event: %w", err)
			}
		}
	}

	return resultDialog, nil
}

// Register forwards tool registration without callback interception.
//
// Contract: callbacks are passed through exactly as provided; event logging stays
// centralized in Generate/middleware to preserve ordering and avoid double emission.
func (g *EmittingGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	registrar, ok := g.base.(ports.ToolRegistrar)
	if !ok {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
	}

	// Pass through the callback without wrapping - events are emitted via the middleware
	return registrar.Register(tool, callback)
}
