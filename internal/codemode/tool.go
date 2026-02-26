package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcp"
)

// executeGoCodeInput represents the input parameters for the execute_go_code tool
type ExecuteGoCodeInput struct {
	Code             string `json:"code"`
	ExecutionTimeout int    `json:"executionTimeout"`
}

// ExecuteGoCodeCallback implements gai.ToolCallback for the execute_go_code tool.
// It executes LLM-generated Go code in a sandbox environment with MCP tool access.
type ExecuteGoCodeCallback struct {
	Servers              []*mcp.MCPConn
	MaxTimeout           int
	LargeOutputCharLimit int
	LocalModulePaths     []string
}

// contentToBlocks converts MCP content types to gai blocks.
// It handles TextContent, ImageContent (including PDFs), and AudioContent.
func contentToBlocks(content []mcpsdk.Content) []gai.Block {
	blocks := make([]gai.Block, 0, len(content))
	for _, c := range content {
		switch v := c.(type) {
		case *mcpsdk.TextContent:
			blocks = append(blocks, gai.TextBlock(v.Text))
		case *mcpsdk.ImageContent:
			if v.MIMEType == "application/pdf" || v.MIMEType == "application/x-pdf" {
				blocks = append(blocks, gai.PDFBlock(v.Data, "document.pdf"))
			} else {
				blocks = append(blocks, gai.ImageBlock(v.Data, v.MIMEType))
			}
		case *mcpsdk.AudioContent:
			blocks = append(blocks, gai.AudioBlock(v.Data, v.MIMEType))
		}
	}
	return blocks
}

// Call executes the generated Go code and returns the result.
// Returns:
//   - Successful execution: tool result with output
//   - RecoverableError (compilation, Run() error, panic, timeout): tool result with error output
//   - FatalExecutionError (exit code 3): error that stops agent execution
//   - Infrastructure errors: error that stops agent execution
func (c *ExecuteGoCodeCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Parse input parameters
	var input ExecuteGoCodeInput
	if err := json.Unmarshal(parametersJSON, &input); err != nil {
		// Return error as tool result so LLM can adapt, not as Go error that stops execution
		//nolint:nilerr // Intentional: user/tool errors return results with nil error to allow agent recovery
		return gai.ToolResultMessage(toolCallID, gai.TextBlock("Error parsing parameters: "+err.Error())), nil
	}

	if input.ExecutionTimeout < 1 {
		return gai.ToolResultMessage(toolCallID, gai.TextBlock("executionTimeout must be at least 1 second")), nil
	}
	maxAllowedTimeout := c.MaxTimeout
	if maxAllowedTimeout <= 0 {
		maxAllowedTimeout = 300
	}
	if input.ExecutionTimeout > maxAllowedTimeout {
		return gai.ToolResultMessage(toolCallID, gai.TextBlock(fmt.Sprintf("executionTimeout exceeds maximum allowed (%d seconds)", maxAllowedTimeout))), nil
	}

	// Execute the code
	result, err := ExecuteCode(
		ctx,
		c.Servers,
		input.Code,
		ExecuteCodeOptions{
			TimeoutSeconds:       input.ExecutionTimeout,
			LargeOutputCharLimit: c.LargeOutputCharLimit,
			LocalModulePaths:     c.LocalModulePaths,
		},
	)
	if err != nil {
		// Check error type to determine how to handle it
		var recoverable RecoverableError
		var fatal FatalExecutionError

		switch {
		case errors.As(err, &recoverable):
			// Recoverable errors are returned as tool results so LLM can adapt
			return gai.ToolResultMessage(toolCallID, gai.TextBlock(recoverable.Output)), nil
		case errors.As(err, &fatal):
			// Fatal errors stop agent execution
			return gai.Message{}, err
		default:
			// Infrastructure errors (temp dir, file writes, etc.) stop agent execution
			return gai.Message{}, err
		}
	}

	// Successful execution - build response with content blocks
	var blocks []gai.Block

	// Prepend stdout/stderr output as text block if present
	if result.Output != "" {
		blocks = append(blocks, gai.TextBlock(result.Output))
	}

	// Add multimedia content blocks
	blocks = append(blocks, contentToBlocks(result.Content)...)

	// If no blocks at all, add an empty text block to satisfy message requirements
	if len(blocks) == 0 {
		blocks = append(blocks, gai.TextBlock(""))
	}

	// Set the tool call ID on all blocks to associate with the tool call
	for i := range blocks {
		blocks[i].ID = toolCallID
	}

	return gai.Message{
		Role:   gai.ToolResult,
		Blocks: blocks,
	}, nil
}
