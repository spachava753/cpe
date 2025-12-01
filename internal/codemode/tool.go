package codemode

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/spachava753/gai"
)

// executeGoCodeInput represents the input parameters for the execute_go_code tool
type executeGoCodeInput struct {
	Code             string `json:"code"`
	ExecutionTimeout int    `json:"executionTimeout"`
}

// ExecuteGoCodeCallback implements gai.ToolCallback for the execute_go_code tool.
// It executes LLM-generated Go code in a sandbox environment with MCP tool access.
type ExecuteGoCodeCallback struct {
	Servers []ServerToolsInfo
}

// Call executes the generated Go code and returns the result.
// Returns:
//   - Successful execution: tool result with output
//   - RecoverableError (compilation, Run() error, panic, timeout): tool result with error output
//   - FatalExecutionError (exit code 3): error that stops agent execution
//   - Infrastructure errors: error that stops agent execution
func (c *ExecuteGoCodeCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Parse input parameters
	var input executeGoCodeInput
	if err := json.Unmarshal(parametersJSON, &input); err != nil {
		return gai.ToolResultMessage(toolCallID, gai.Text, "text/plain", gai.Str("Error parsing parameters: "+err.Error())), nil
	}

	// Execute the code
	result, err := ExecuteCode(ctx, c.Servers, input.Code, input.ExecutionTimeout)
	if err != nil {
		// Check error type to determine how to handle it
		var recoverable RecoverableError
		var fatal FatalExecutionError

		switch {
		case errors.As(err, &recoverable):
			// Recoverable errors are returned as tool results so LLM can adapt
			return gai.ToolResultMessage(toolCallID, gai.Text, "text/plain", gai.Str(recoverable.Output)), nil
		case errors.As(err, &fatal):
			// Fatal errors stop agent execution
			return gai.Message{}, err
		default:
			// Infrastructure errors (temp dir, file writes, etc.) stop agent execution
			return gai.Message{}, err
		}
	}

	// Successful execution
	return gai.ToolResultMessage(toolCallID, gai.Text, "text/plain", gai.Str(result.Output)), nil
}
