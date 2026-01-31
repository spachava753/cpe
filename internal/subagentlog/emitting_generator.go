package subagentlog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/types"
)

const finalAnswerToolName = "final_answer"

// EmittingGenerator wraps a generator to emit subagent events via a generator middleware.
// All events (thought_trace, tool_call, tool_result) are emitted by wrapping the inner
// ToolCapableGenerator's Generate method, eliminating the need for callback wrapping.
type EmittingGenerator struct {
	base               types.Generator
	client             *Client
	subagentName       string
	runID              string
	hasWrappedInnerGen bool // true if we successfully wrapped the inner generator
}

// NewEmittingGenerator creates a new EmittingGenerator that wraps the base generator.
// If the base contains a *gai.ToolGenerator (possibly wrapped by other generators),
// it unwraps the chain and wraps the inner ToolCapableGenerator with a middleware
// that emits all subagent events (thought_trace, tool_call, tool_result).
func NewEmittingGenerator(base types.Generator, client *Client, subagentName, runID string) *EmittingGenerator {
	hasWrappedInnerGen := false

	// Unwrap the generator chain to find the underlying *gai.ToolGenerator
	if toolGen := findToolGenerator(base); toolGen != nil {
		wrappedG := &emittingMiddleware{
			base:         toolGen.G,
			client:       client,
			subagentName: subagentName,
			runID:        runID,
		}
		toolGen.G = wrappedG
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

// findToolGenerator recursively unwraps generator wrappers to find a *gai.ToolGenerator.
func findToolGenerator(gen interface{}) *gai.ToolGenerator {
	if tg, ok := gen.(*gai.ToolGenerator); ok {
		return tg
	}
	if wrapper, ok := gen.(interface{ Inner() types.Generator }); ok {
		return findToolGenerator(wrapper.Inner())
	}
	return nil
}

// emittingMiddleware wraps a gai.ToolCapableGenerator to emit all subagent events.
// It emits events by inspecting the dialog and response at the Generate boundary:
//   - tool_result events: emitted BEFORE calling inner Generate, for tool results in the dialog
//   - thought_trace events: emitted AFTER inner Generate returns, for thinking blocks in response
//   - tool_call events: emitted AFTER inner Generate returns, for tool call blocks in response
//
// This approach consolidates all event emission into a single middleware, eliminating
// the need for callback wrapping.
type emittingMiddleware struct {
	base         gai.ToolCapableGenerator
	client       *Client
	subagentName string
	runID        string
}

// Generate implements the middleware pattern for event emission.
// Event emission order ensures correct chronological sequence:
// 1. Emit tool_result for any new tool results in the dialog (from previous iteration)
// 2. Call inner generator
// 3. Emit thought_trace for thinking blocks in response
// 4. Emit tool_call for tool call blocks in response
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

// emitToolResultEvents emits tool_result events for tool results after the last assistant message.
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

		var resultText string
		if block.ModalityType == gai.Text {
			resultText = block.Content.String()
		}

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

// emitResponseEvents emits thought_trace and tool_call events for blocks in the response.
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

// emitThinkingEvent emits a thought_trace event for a thinking block.
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

// emitToolCallEvent emits a tool_call event for a tool call block.
func (m *emittingMiddleware) emitToolCallEvent(ctx context.Context, block gai.Block) error {
	var toolCall gai.ToolCallInput
	if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err != nil {
		// Skip malformed tool calls - this is a recoverable error
		return nil
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

// findToolNameByCallID finds the tool name from an assistant message's tool calls by call ID.
func (m *emittingMiddleware) findToolNameByCallID(assistantMsg gai.Message, toolCallID string) string {
	for _, block := range assistantMsg.Blocks {
		if block.BlockType != gai.ToolCall || block.ID != toolCallID {
			continue
		}

		var toolCall gai.ToolCallInput
		if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err == nil {
			return toolCall.Name
		}
	}
	return "unknown"
}

// Register implements gai.ToolRegister by delegating to the base generator.
func (m *emittingMiddleware) Register(tool gai.Tool) error {
	return m.base.Register(tool)
}

// Generate delegates to the base generator.
// If the inner generator was wrapped (for *gai.ToolGenerator), events are already
// emitted at the right time by the emittingMiddleware.
// Otherwise, this provides a fallback for non-ToolGenerator bases (e.g., in tests).
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
	for _, msg := range resultDialog[originalLen:] {
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
			// Find tool name from previous assistant message
			toolName := "unknown"
			if len(msg.Blocks) > 0 {
				toolCallID := msg.Blocks[0].ID
				for i := len(resultDialog) - 1; i >= 0; i-- {
					if resultDialog[i].Role == gai.Assistant {
						for _, b := range resultDialog[i].Blocks {
							if b.BlockType == gai.ToolCall && b.ID == toolCallID {
								var tc gai.ToolCallInput
								if err := json.Unmarshal([]byte(b.Content.String()), &tc); err == nil {
									toolName = tc.Name
								}
								break
							}
						}
						break
					}
				}
			}
			if toolName == finalAnswerToolName {
				continue
			}
			var resultText string
			if len(msg.Blocks) > 0 && msg.Blocks[0].ModalityType == gai.Text {
				resultText = msg.Blocks[0].Content.String()
			}
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

// Register delegates tool registration to the base generator without wrapping callbacks.
// Event emission is handled entirely by the middleware wrapping Generate().
func (g *EmittingGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	registrar, ok := g.base.(types.ToolRegistrar)
	if !ok {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
	}

	// Pass through the callback without wrapping - events are emitted via the middleware
	return registrar.Register(tool, callback)
}
