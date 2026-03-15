package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/ports"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/subagentlog"
)

// FinalAnswerToolName is the reserved terminal tool for structured subagent output.
// When OutputSchema is configured, the model must call this tool exactly when it is
// ready to return its final schema-shaped answer.
const FinalAnswerToolName = "final_answer"

// SubagentOptions defines execution-time dependencies for one subagent run.
//
// OutputSchema, Storage, and EventClient are optional extensions layered on top
// of core generation. When EventClient is set, SubagentName and RunID must also
// be set so emitted events can be attributed and correlated.
type SubagentOptions struct {
	// UserBlocks is the user message content for the run; must be non-empty.
	UserBlocks []gai.Block

	// Generator executes the dialog. If OutputSchema is set, it must implement
	// ports.ToolRegistrar so final_answer can be registered.
	Generator ports.Generator

	// GenOptsFunc lazily derives generation options per dialog turn (optional).
	GenOptsFunc gai.GenOptsGenerator

	// OutputSchema enables structured terminal output (optional).
	// When non-nil, final_answer is registered with this schema as input. The run
	// is considered successful only if the model calls final_answer; the call's
	// parameters are returned as JSON text.
	OutputSchema *jsonschema.Schema

	// Storage persists execution traces (optional). Saved messages are tagged with
	// storage.MessageIsSubagentKey=true for downstream filtering.
	Storage storage.DialogSaver

	// EventClient streams lifecycle/tool/thinking events to the parent process
	// (optional). See ExecuteSubagent for emission failure semantics.
	EventClient *subagentlog.Client

	// SubagentName labels emitted events (required when EventClient is set).
	SubagentName string

	// RunID correlates all emitted events for this invocation
	// (required when EventClient is set).
	RunID string
}

// ExecuteSubagent runs one subagent invocation and returns its terminal output.
//
// Output contract:
//   - With OutputSchema: returns JSON-encoded final_answer parameters.
//   - Without OutputSchema: returns final assistant text content.
//
// Lifecycle event contract when EventClient is set:
//   - subagent_start is emitted before execution; failure is fatal and aborts run.
//   - subagent_end is emitted after execution (success or failure).
//   - subagent_end emission failure is logged as warning and does not mask the
//     original execution result.
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

// executeSubagentCore performs generation, optional trace persistence, and
// terminal-output extraction without lifecycle-event boilerplate.
func executeSubagentCore(ctx context.Context, opts SubagentOptions) (string, error) {
	// Wrap generator when event streaming is enabled so tool/thinking events follow
	// subagentlog ordering and failure semantics.
	generator := opts.Generator
	if opts.EventClient != nil {
		generator = subagentlog.NewEmittingGenerator(opts.Generator, opts.EventClient, opts.SubagentName, opts.RunID)
	}

	// If structured output is configured, expose final_answer so the model can
	// terminate with schema-shaped data.
	if opts.OutputSchema != nil {
		registrar, ok := generator.(ports.ToolRegistrar)
		if !ok {
			return "", fmt.Errorf("generator does not support tool registration")
		}

		finalAnswerTool := gai.Tool{
			Name:        FinalAnswerToolName,
			Description: "Submit the final structured answer. Call this tool when you have completed the task and are ready to return the result.",
			InputSchema: opts.OutputSchema,
		}
		// Register with nil callback: tool use itself is the termination signal,
		// and parameters are later extracted from the dialog.
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

	if len(resultDialog) < len(dialog) {
		return "", fmt.Errorf("generation returned invalid dialog length: got %d, expected at least %d", len(resultDialog), len(dialog))
	}
	assistantTrace := resultDialog[len(dialog):]

	// Persist execution trace if storage is configured
	if opts.Storage != nil {
		if saveErr := saveSubagentTrace(ctx, opts.Storage, userMessage, assistantTrace); saveErr != nil {
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

// saveSubagentTrace persists one subagent run as a single dialog in message order:
// user message first, then generated assistant/tool messages.
//
// Every saved message is tagged as subagent-originated for filtering in storage
// and UI surfaces.
func saveSubagentTrace(ctx context.Context, saver storage.DialogSaver, userMsg gai.Message, assistantMsgs gai.Dialog) error {
	// Build the full dialog: user message first, then all assistant messages
	allMsgs := make([]gai.Message, 0, len(assistantMsgs)+1)

	if userMsg.ExtraFields == nil {
		userMsg.ExtraFields = make(map[string]any)
	}
	userMsg.ExtraFields[storage.MessageIsSubagentKey] = true
	allMsgs = append(allMsgs, userMsg)

	for _, msg := range assistantMsgs {
		if msg.ExtraFields == nil {
			msg.ExtraFields = make(map[string]any)
		}
		msg.ExtraFields[storage.MessageIsSubagentKey] = true
		allMsgs = append(allMsgs, msg)
	}

	// Save the entire dialog in one call
	for _, err := range saver.SaveDialog(ctx, slices.Values(allMsgs)) {
		if err != nil {
			return fmt.Errorf("failed to save subagent trace: %w", err)
		}
	}

	return nil
}

// extractFinalAnswerParams finds the most recent final_answer tool call and
// returns its parameters as compact JSON text.
//
// The search runs from the end of the dialog to honor the latest terminal tool
// invocation. If no final_answer call exists, an error is returned.
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

// extractFinalResponse returns text blocks from the last assistant message.
// Non-text modalities and non-content blocks are ignored.
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
