package subagentlog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/types"
	"github.com/spachava753/gai"
)

// EmittingGenerator wraps a generator to emit events for thinking blocks and tool calls
type EmittingGenerator struct {
	base               types.Generator
	client             *Client
	subagentName       string
	runID              string
	hasWrappedInnerGen bool // true if we successfully wrapped the inner generator
}

// NewEmittingGenerator creates a new EmittingGenerator that wraps the base generator.
// If the base contains a *gai.ToolGenerator (possibly wrapped by other generators),
// it unwraps the chain and wraps the inner ToolCapableGenerator to emit thinking
// events immediately when they are received (before tool execution).
func NewEmittingGenerator(base types.Generator, client *Client, subagentName, runID string) *EmittingGenerator {
	hasWrappedInnerGen := false

	// Unwrap the generator chain to find the underlying *gai.ToolGenerator
	if toolGen := findToolGenerator(base); toolGen != nil {
		wrappedG := &emittingToolCapableGenerator{
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
// It checks if the generator is directly a ToolGenerator, or if it implements an Inner()
// method to access a wrapped generator.
func findToolGenerator(gen interface{}) *gai.ToolGenerator {
	// Check if it's directly a ToolGenerator
	if tg, ok := gen.(*gai.ToolGenerator); ok {
		return tg
	}
	// Check if it has an Inner() method that returns types.Generator
	if wrapper, ok := gen.(interface{ Inner() types.Generator }); ok {
		return findToolGenerator(wrapper.Inner())
	}
	return nil
}

// emittingToolCapableGenerator wraps a gai.ToolCapableGenerator to emit thinking events
// immediately after each Generate call, before the response is processed by ToolGenerator.
// This ensures thinking events appear before tool_call events in the correct chronological order.
type emittingToolCapableGenerator struct {
	base         gai.ToolCapableGenerator
	client       *Client
	subagentName string
	runID        string
}

// Generate calls the base generator and emits thinking events immediately for any thinking blocks
func (g *emittingToolCapableGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	resp, err := g.base.Generate(ctx, dialog, options)
	if err != nil {
		return resp, err
	}

	// Emit thinking events IMMEDIATELY for any thinking blocks in the response
	// This happens BEFORE ToolGenerator processes tool calls
	for _, candidate := range resp.Candidates {
		for _, block := range candidate.Blocks {
			if block.BlockType == gai.Thinking {
				event := Event{
					SubagentName:  g.subagentName,
					SubagentRunID: g.runID,
					Timestamp:     time.Now(),
					Type:          EventTypeThoughtTrace,
					Payload:       block.Content.String(),
				}
				// Extract reasoning type from extra fields if present
				if block.ExtraFields != nil {
					if reasoningType, ok := block.ExtraFields["reasoning_type"].(string); ok {
						event.ReasoningType = reasoningType
					}
				}
				if err := g.client.Emit(ctx, event); err != nil {
					return gai.Response{}, fmt.Errorf("failed to emit thought_trace event: %w", err)
				}
			}
		}
	}

	return resp, nil
}

// Register implements gai.ToolRegister by delegating to the base generator
func (g *emittingToolCapableGenerator) Register(tool gai.Tool) error {
	return g.base.Register(tool)
}

// Generate wraps the base Generate and emits events for thinking blocks.
// If the inner generator was wrapped (for *gai.ToolGenerator), thinking events are already
// emitted at the right time by emittingToolCapableGenerator.
// Otherwise, we emit thinking events here as a fallback (after full generation completes).
func (g *EmittingGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	originalLen := len(dialog)

	resultDialog, err := g.base.Generate(ctx, dialog, optsGen)
	if err != nil {
		return resultDialog, err
	}

	// If we wrapped the inner generator, thinking events are already emitted
	// at the right time (before tool callbacks). Skip to avoid duplicates.
	if g.hasWrappedInnerGen {
		return resultDialog, nil
	}

	// Fallback: emit thinking events here (after full generation, for non-ToolGenerator bases)
	// This is used when the base is not a *gai.ToolGenerator (e.g., in tests with mockGenerator)
	for _, msg := range resultDialog[originalLen:] {
		if msg.Role != gai.Assistant {
			continue
		}

		for _, block := range msg.Blocks {
			if block.BlockType == gai.Thinking {
				// Emit thought_trace event
				event := Event{
					SubagentName:  g.subagentName,
					SubagentRunID: g.runID,
					Timestamp:     time.Now(),
					Type:          EventTypeThoughtTrace,
					Payload:       block.Content.String(),
				}
				// Extract reasoning type from extra fields if present
				if block.ExtraFields != nil {
					if reasoningType, ok := block.ExtraFields["reasoning_type"].(string); ok {
						event.ReasoningType = reasoningType
					}
				}
				if err := g.client.Emit(ctx, event); err != nil {
					return nil, fmt.Errorf("failed to emit thought_trace event: %w", err)
				}
			}
		}
	}

	return resultDialog, nil
}

// Register wraps the callback with EmittingToolCallback and delegates to the base generator
func (g *EmittingGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	registrar, ok := g.base.(types.ToolRegistrar)
	if !ok {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
	}

	// If callback is nil, pass through without wrapping (nil callbacks terminate execution)
	if callback == nil {
		return registrar.Register(tool, nil)
	}

	// Wrap the callback with EmittingToolCallback
	wrappedCallback := NewEmittingToolCallback(callback, g.client, g.subagentName, g.runID, tool.Name)
	return registrar.Register(tool, wrappedCallback)
}

// EmittingToolCallback wraps a ToolCallback to emit tool_call and tool_result events
type EmittingToolCallback struct {
	base         gai.ToolCallback
	client       *Client
	subagentName string
	runID        string
	toolName     string
}

// NewEmittingToolCallback creates a new EmittingToolCallback
func NewEmittingToolCallback(base gai.ToolCallback, client *Client, subagentName, runID, toolName string) *EmittingToolCallback {
	return &EmittingToolCallback{
		base:         base,
		client:       client,
		subagentName: subagentName,
		runID:        runID,
		toolName:     toolName,
	}
}

// Call emits tool_call event, executes the wrapped callback, and emits tool_result event
func (c *EmittingToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Skip emitting events for final_answer tool to avoid duplicate output
	if c.toolName == "final_answer" {
		return c.base.Call(ctx, parametersJSON, toolCallID)
	}

	// Emit tool_call event BEFORE executing the callback
	// This ensures tool_call appears before tool_result in the event stream
	toolCallEvent := Event{
		SubagentName:  c.subagentName,
		SubagentRunID: c.runID,
		Timestamp:     time.Now(),
		Type:          EventTypeToolCall,
		ToolName:      c.toolName,
		ToolCallID:    toolCallID,
	}

	// Parse parameters to set payload appropriately
	var params map[string]interface{}
	if err := json.Unmarshal(parametersJSON, &params); err == nil {
		// Handle execute_go_code specially: extract code and timeout
		if c.toolName == codemode.ExecuteGoCodeToolName {
			if code, ok := params["code"].(string); ok {
				toolCallEvent.Payload = code
			}
			if timeout, ok := params["executionTimeout"]; ok {
				switch t := timeout.(type) {
				case float64:
					toolCallEvent.ExecutionTimeoutSeconds = int(t)
				case int:
					toolCallEvent.ExecutionTimeoutSeconds = t
				}
			}
		} else {
			// For other tools, use the raw JSON as payload
			toolCallEvent.Payload = string(parametersJSON)
		}
	}

	if err := c.client.Emit(ctx, toolCallEvent); err != nil {
		return gai.Message{}, fmt.Errorf("failed to emit tool_call event: %w", err)
	}

	// Execute the actual callback
	msg, err := c.base.Call(ctx, parametersJSON, toolCallID)
	if err != nil {
		return msg, err
	}

	// Extract result text from the message blocks
	var resultText string
	for _, block := range msg.Blocks {
		if block.ModalityType == gai.Text {
			resultText += block.Content.String()
		}
	}

	// Emit tool_result event
	toolResultEvent := Event{
		SubagentName:  c.subagentName,
		SubagentRunID: c.runID,
		Timestamp:     time.Now(),
		Type:          EventTypeToolResult,
		ToolName:      c.toolName,
		ToolCallID:    toolCallID,
		Payload:       resultText,
	}

	if err := c.client.Emit(ctx, toolResultEvent); err != nil {
		return gai.Message{}, fmt.Errorf("failed to emit tool_result event: %w", err)
	}

	return msg, nil
}
