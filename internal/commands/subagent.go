package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/subagentlog"
	"github.com/spachava753/cpe/internal/types"
)

// FinalAnswerToolName is the name of the tool used for structured output
const FinalAnswerToolName = "final_answer"

// SubagentOptions contains parameters for subagent execution
type SubagentOptions struct {
	// UserBlocks is the input to the subagent
	UserBlocks []gai.Block

	// Generator is the tool-capable generator to use
	Generator types.Generator

	// GenOptsFunc returns generation options (optional)
	GenOptsFunc gai.GenOptsGenerator

	// OutputSchema is the JSON schema for structured output (optional).
	// When set, a final_answer tool is registered with this schema as input,
	// and execution terminates when the model calls final_answer.
	// The tool call parameters are returned as the result.
	OutputSchema *jsonschema.Schema

	// Storage is the message saver for persisting execution traces (optional).
	// When set, messages are saved with SubagentLabel annotation.
	Storage storage.MessagesSaver

	// SubagentLabel is the label used to annotate saved messages (optional).
	// Typically formatted as "subagent:<name>:<run_id>" to distinguish
	// subagent activity from parent agent entries.
	SubagentLabel string

	// EventClient is the client for emitting subagent events (optional).
	// When set, events are emitted for lifecycle, tool calls, and thinking.
	EventClient *subagentlog.Client

	// SubagentName is the name of the subagent (required when EventClient is set)
	SubagentName string

	// RunID is the correlation ID for event tracking (required when EventClient is set)
	RunID string
}

// ExecuteSubagent runs a subagent and returns the final response.
// If OutputSchema is set, the subagent must call final_answer with structured data,
// and that data is returned as JSON. Otherwise, the final text response is returned.
func ExecuteSubagent(ctx context.Context, opts SubagentOptions) (string, error) {
	if len(opts.UserBlocks) == 0 {
		return "", fmt.Errorf("empty input")
	}

	// Emit subagent_start event if event client is configured
	if opts.EventClient != nil {
		event := subagentlog.Event{
			SubagentName:  opts.SubagentName,
			SubagentRunID: opts.RunID,
			Timestamp:     time.Now(),
			Type:          subagentlog.EventTypeSubagentStart,
		}
		if err := opts.EventClient.Emit(ctx, event); err != nil {
			return "", fmt.Errorf("failed to emit subagent_start event: %w", err)
		}
	}

	// Execute the subagent and track any error for the end event
	result, execErr := executeSubagentCore(ctx, opts)

	// Emit subagent_end event if event client is configured
	if opts.EventClient != nil {
		event := subagentlog.Event{
			SubagentName:  opts.SubagentName,
			SubagentRunID: opts.RunID,
			Timestamp:     time.Now(),
			Type:          subagentlog.EventTypeSubagentEnd,
		}
		if execErr != nil {
			event.Payload = execErr.Error()
		}
		if err := opts.EventClient.Emit(ctx, event); err != nil {
			// Log but don't override the original error
			fmt.Fprintf(os.Stderr, "warning: failed to emit subagent_end event: %v\n", err)
		}
	}

	return result, execErr
}

// executeSubagentCore contains the core subagent execution logic
func executeSubagentCore(ctx context.Context, opts SubagentOptions) (string, error) {
	// Determine the generator to use - wrap with EmittingGenerator if event client is set
	generator := opts.Generator
	if opts.EventClient != nil {
		generator = subagentlog.NewEmittingGenerator(opts.Generator, opts.EventClient, opts.SubagentName, opts.RunID)
	}

	// If output schema is configured, register the final_answer tool
	if opts.OutputSchema != nil {
		registrar, ok := generator.(types.ToolRegistrar)
		if !ok {
			return "", fmt.Errorf("generator does not support tool registration")
		}

		finalAnswerTool := gai.Tool{
			Name:        FinalAnswerToolName,
			Description: "Submit the final structured answer. Call this tool when you have completed the task and are ready to return the result.",
			InputSchema: opts.OutputSchema,
		}
		// Register with nil callback to terminate execution when called
		if err := registrar.Register(finalAnswerTool, nil); err != nil {
			return "", fmt.Errorf("failed to register final_answer tool: %w", err)
		}
	}

	// Build dialog with user message
	userMessage := gai.Message{
		Role:   gai.User,
		Blocks: opts.UserBlocks,
	}
	dialog := gai.Dialog{userMessage}

	// Generate response
	resultDialog, err := generator.Generate(ctx, dialog, opts.GenOptsFunc)
	if err != nil {
		return "", fmt.Errorf("generation failed: %w", err)
	}

	// Persist execution trace if storage is configured
	if opts.Storage != nil {
		if saveErr := saveSubagentTrace(ctx, opts.Storage, userMessage, resultDialog[len(dialog):], opts.SubagentLabel); saveErr != nil {
			// Log but don't fail - persistence is secondary to execution
			fmt.Fprintf(os.Stderr, "warning: failed to save subagent trace: %v\n", saveErr)
		}
	}

	// If output schema is configured, extract the final_answer parameters
	if opts.OutputSchema != nil {
		return extractFinalAnswerParams(resultDialog)
	}

	// Otherwise, extract the final assistant response text
	return extractFinalResponse(resultDialog), nil
}

// saveSubagentTrace persists the subagent execution trace to storage
func saveSubagentTrace(ctx context.Context, saver storage.MessagesSaver, userMsg gai.Message, assistantMsgs gai.Dialog, label string) error {
	// Save user message with label
	ids, err := saver.SaveMessages(ctx, []storage.SaveMessageOptions{
		{Message: userMsg, ParentID: "", Title: label},
	})
	if err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}
	var parentID string
	for id := range ids {
		parentID = id
	}

	// Save assistant messages in chain
	for _, msg := range assistantMsgs {
		ids, err = saver.SaveMessages(ctx, []storage.SaveMessageOptions{
			{Message: msg, ParentID: parentID, Title: label},
		})
		if err != nil {
			return fmt.Errorf("failed to save assistant message: %w", err)
		}
		for id := range ids {
			parentID = id
		}
	}

	return nil
}

// extractFinalAnswerParams extracts the parameters from a final_answer tool call.
// It searches the dialog for a ToolCall block with name "final_answer" and returns
// its parameters as JSON.
func extractFinalAnswerParams(dialog gai.Dialog) (string, error) {
	// Search from the end of the dialog for the final_answer tool call
	for i := len(dialog) - 1; i >= 0; i-- {
		msg := dialog[i]
		if msg.Role != gai.Assistant {
			continue
		}
		for _, block := range msg.Blocks {
			if block.BlockType != gai.ToolCall {
				continue
			}
			// Parse the tool call to get the name and parameters
			var toolCall gai.ToolCallInput
			if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err != nil {
				continue
			}
			if toolCall.Name == FinalAnswerToolName {
				// Return the parameters as JSON
				paramsJSON, err := json.Marshal(toolCall.Parameters)
				if err != nil {
					return "", fmt.Errorf("failed to marshal final_answer parameters: %w", err)
				}
				return string(paramsJSON), nil
			}
		}
	}
	return "", fmt.Errorf("subagent did not call final_answer tool")
}

// extractFinalResponse extracts the final text response from the dialog
func extractFinalResponse(dialog gai.Dialog) string {
	// Find the last assistant message
	for i := len(dialog) - 1; i >= 0; i-- {
		if dialog[i].Role == gai.Assistant {
			// Extract text content from blocks
			var textParts []string
			for _, block := range dialog[i].Blocks {
				if block.BlockType == gai.Content && block.ModalityType == gai.Text {
					textParts = append(textParts, block.Content.String())
				}
			}
			if len(textParts) > 0 {
				return strings.Join(textParts, "\n")
			}
		}
	}
	return ""
}
