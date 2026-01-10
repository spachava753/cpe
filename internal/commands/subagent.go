package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
)

// FinalAnswerToolName is the name of the tool used for structured output
const FinalAnswerToolName = "final_answer"

// ToolRegistrar is an interface for registering tools
type ToolRegistrar interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

// SubagentOptions contains parameters for subagent execution
type SubagentOptions struct {
	// UserBlocks is the input to the subagent
	UserBlocks []gai.Block

	// Generator is the tool-capable generator to use
	Generator ToolCapableGenerator

	// GenOptsFunc returns generation options (optional)
	GenOptsFunc gai.GenOptsGenerator

	// OutputSchema is the JSON schema for structured output (optional).
	// When set, a final_answer tool is registered with this schema as input,
	// and execution terminates when the model calls final_answer.
	// The tool call parameters are returned as the result.
	OutputSchema *jsonschema.Schema

	// Storage is the dialog storage for persisting execution traces (optional).
	// When set, messages are saved with SubagentLabel annotation.
	Storage DialogStorage

	// SubagentLabel is the label used to annotate saved messages (optional).
	// Typically formatted as "subagent:<name>:<run_id>" to distinguish
	// subagent activity from parent agent entries.
	SubagentLabel string
}

// ExecuteSubagent runs a subagent and returns the final response.
// If OutputSchema is set, the subagent must call final_answer with structured data,
// and that data is returned as JSON. Otherwise, the final text response is returned.
func ExecuteSubagent(ctx context.Context, opts SubagentOptions) (string, error) {
	if len(opts.UserBlocks) == 0 {
		return "", fmt.Errorf("empty input")
	}

	// If output schema is configured, register the final_answer tool
	if opts.OutputSchema != nil {
		registrar, ok := opts.Generator.(ToolRegistrar)
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
	resultDialog, err := opts.Generator.Generate(ctx, dialog, opts.GenOptsFunc)
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
func saveSubagentTrace(ctx context.Context, storage DialogStorage, userMsg gai.Message, assistantMsgs gai.Dialog, label string) error {
	// Save user message with label
	userMsgID, err := storage.SaveMessage(ctx, userMsg, "", label)
	if err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Save assistant messages in chain
	parentID := userMsgID
	for _, msg := range assistantMsgs {
		parentID, err = storage.SaveMessage(ctx, msg, parentID, label)
		if err != nil {
			return fmt.Errorf("failed to save assistant message: %w", err)
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
