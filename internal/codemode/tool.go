package codemode

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/acp-go-sdk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/mapstruct"
)

// ExecuteGoCodeToolName is the reserved model-facing tool name for code mode.
const ExecuteGoCodeToolName = "execute_go_code"

// executeGoCodeInput is the execute_go_code payload expected from the model.
// ExecutionTimeout is validated against callback-level limits before execution starts.
type executeGoCodeInput struct {
	Code             string `json:"code"`
	ExecutionTimeout int    `json:"executionTimeout"`
}

type acpConn interface {
	SessionUpdate(ctx context.Context, params acp.SessionNotification) error
	CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error)
	KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error)
	TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error)
	ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error)
	WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error)
}

// ExecuteGoCodeCallback implements gai.ToolCallback for execute_go_code.
// It enforces timeout/output policy and delegates execution to the sandbox pipeline.
type ExecuteGoCodeCallback struct {
	MaxTimeout           int
	LargeOutputCharLimit int
	Cwd                  string
	SessionId            acp.SessionId
	Conn                 acpConn
	TerminalSupport      bool
}

// contentToBlocks adapts MCP multimodal content into gai blocks.
// Unsupported content variants are ignored so tool output remains renderable.
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

// Call validates input, runs generated code, and maps failures into agent control flow.
// Recoverable execution failures are returned as ToolResult text so the model can iterate;
// infrastructure failures are returned as Go errors to stop the run.
func (c *ExecuteGoCodeCallback) Call(ctx context.Context, params map[string]any) (gai.Message, error) {
	sendToolCallUpdate := func(status acp.ToolCallStatus) {
		if c.Conn == nil {
			return
		}
		c.Conn.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: c.SessionId,
			Update: acp.UpdateToolCall(
				xctx.ToolCallIdFrom(ctx),
				acp.WithUpdateKind(acp.ToolKindExecute),
				acp.WithUpdateStatus(status),
			),
		})
	}

	input, err := mapstruct.Map2Struct[executeGoCodeInput](params)
	if err != nil {
		sendToolCallUpdate(acp.ToolCallStatusFailed)
		// Return error as tool result so LLM can adapt, not as Go error that stops execution
		return gai.ToolResultMessage("", gai.TextBlock("Error parsing parameters: "+err.Error())), nil //nolint:nilerr // user/tool errors return results with nil error to allow agent recovery
	}

	if input.ExecutionTimeout < 1 {
		sendToolCallUpdate(acp.ToolCallStatusFailed)
		return gai.ToolResultMessage("", gai.TextBlock("executionTimeout must be at least 1 second")), nil
	}
	maxAllowedTimeout := c.MaxTimeout
	if maxAllowedTimeout <= 0 {
		maxAllowedTimeout = 300
	}
	if input.ExecutionTimeout > maxAllowedTimeout {
		sendToolCallUpdate(acp.ToolCallStatusFailed)
		return gai.ToolResultMessage("", gai.TextBlock(fmt.Sprintf("executionTimeout exceeds maximum allowed (%d seconds)", maxAllowedTimeout))), nil
	}

	sendToolCallUpdate(acp.ToolCallStatusInProgress)

	// Execute the code
	result, err := c.executeCode(
		ctx,
		input.Code,
		input.ExecutionTimeout,
	)
	if err != nil {
		if recoverable, ok := errors.AsType[RecoverableError](err); ok {
			// Recoverable errors are returned as tool results so LLM can adapt.
			sendToolCallUpdate(acp.ToolCallStatusFailed)
			text := recoverable.Error()
			if result.Output != "" {
				text += "\n\n" + result.Output
			}
			return gai.ToolResultMessage("", gai.TextBlock(text)), nil
		}
		// Infrastructure errors (temp dir, file writes, etc.) stop agent execution.
		return gai.Message{}, err
	}

	// Successful execution.
	sendToolCallUpdate(acp.ToolCallStatusCompleted)

	// build response with content blocks
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

	return gai.Message{
		Role:   gai.ToolResult,
		Blocks: blocks,
	}, nil
}
