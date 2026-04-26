package subagentlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

type runtimeObserver struct {
	client       *Client
	subagentName string
	runID        string
}

func (o runtimeObserver) ToolResult(ctx context.Context, _ gai.Dialog, msg gai.Message, associatedCall gai.Block) error {
	toolName := toolNameFromBlock(associatedCall)
	if toolName == finalAnswerToolName {
		return nil
	}
	toolCallID := associatedCall.ID
	if toolCallID == "" && len(msg.Blocks) > 0 {
		toolCallID = msg.Blocks[0].ID
	}
	event := Event{
		SubagentName:  o.subagentName,
		SubagentRunID: o.runID,
		Timestamp:     time.Now(),
		Type:          EventTypeToolResult,
		ToolName:      toolName,
		ToolCallID:    toolCallID,
		Payload:       formatToolResultPayload(msg.Blocks),
	}
	if err := o.client.Emit(ctx, event); err != nil {
		return fmt.Errorf("failed to emit tool_result event: %w", err)
	}
	return nil
}

func (o runtimeObserver) ThoughtTrace(ctx context.Context, _ gai.Dialog, block gai.Block) error {
	event := Event{
		SubagentName:  o.subagentName,
		SubagentRunID: o.runID,
		Timestamp:     time.Now(),
		Type:          EventTypeThoughtTrace,
		Payload:       block.Content.String(),
	}
	if block.ExtraFields != nil {
		if reasoningType, ok := block.ExtraFields["reasoning_type"].(string); ok {
			event.ReasoningType = reasoningType
		}
	}
	if err := o.client.Emit(ctx, event); err != nil {
		return fmt.Errorf("failed to emit thought_trace event: %w", err)
	}
	return nil
}

func (o runtimeObserver) ToolCall(ctx context.Context, _ gai.Dialog, block gai.Block) error {
	event, ok := toolCallEvent(o.subagentName, o.runID, block)
	if !ok {
		return nil
	}
	if err := o.client.Emit(ctx, event); err != nil {
		return fmt.Errorf("failed to emit tool_call event: %w", err)
	}
	return nil
}

func toolCallEvent(subagentName, runID string, block gai.Block) (Event, bool) {
	var toolCall gai.ToolCallInput
	if block.Content == nil || json.Unmarshal([]byte(block.Content.String()), &toolCall) != nil {
		return Event{}, false
	}
	if toolCall.Name == finalAnswerToolName {
		return Event{}, false
	}
	event := Event{
		SubagentName:  subagentName,
		SubagentRunID: runID,
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
	} else if paramsJSON, err := json.Marshal(toolCall.Parameters); err == nil {
		event.Payload = string(paramsJSON)
	}
	return event, true
}

func toolNameFromBlock(block gai.Block) string {
	if block.BlockType != gai.ToolCall || block.Content == nil {
		return unknownToolName
	}
	var toolCall gai.ToolCallInput
	if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err == nil && toolCall.Name != "" {
		return toolCall.Name
	}
	return unknownToolName
}

func findToolNameByCallID(assistantMsg gai.Message, toolCallID string) string {
	for _, block := range assistantMsg.Blocks {
		if block.BlockType != gai.ToolCall || block.ID != toolCallID {
			continue
		}
		return toolNameFromBlock(block)
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
