package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Validator interface {
	Validate() error
}

// createToolHandler creates a generic handler for tools with a common function signature pattern.
// It takes a function that accepts a context and a strongly typed input struct,
// and converts between the generic CallToolRequest/CallToolResult types and the typed function.
//
// Type parameter T is the input struct type with a Validate() method
// f is the actual tool implementation function with the signature func(ctx context.Context, t T) (string, error)
func createToolHandler[T any](f func(ctx context.Context, t T) (string, error)) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Create a new instance of T
		var input T

		// Get the type of T to check if it has a Validate method
		inputType := reflect.TypeOf(input)

		// JSON marshal the arguments map to a byte array
		argsBytes, err := json.Marshal(request.Params.Arguments)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal arguments: %v", err)), nil
		}

		// Unmarshal the byte array to the input struct
		if err := json.Unmarshal(argsBytes, &input); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to unmarshal arguments to %s: %v", inputType.Name(), err)), nil
		}

		// Check if T has a Validate method and call it
		if v, ok := any(&input).(Validator); ok {
			if err := v.Validate(); err != nil {
				// Validation failed, return the error
				return mcp.NewToolResultError(err.Error()), nil
			}
		}

		// Execute the tool function
		result, err := f(ctx, input)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("tool execution failed: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	}
}
