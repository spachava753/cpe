package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/acp/acpstar"
	"github.com/spachava753/cpe/internal/acp/xacp"
	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/mapstruct"
)

// starlarkREPLToolName is the reserved model-facing tool name for code mode.
const starlarkREPLToolName = "starlark_repl"

type starlarkREPLInput struct {
	Code             string `json:"code"`
	ExecutionTimeout int    `json:"executionTimeout"`
}

type acpConn interface {
	SessionUpdate(ctx context.Context, params *acp.SessionNotification) error
}

// starlarkREPLCallback implements the session-scoped starlark_repl tool.
type starlarkREPLCallback struct {
	MaxTimeout           int
	LargeOutputCharLimit int
	Cwd                  string
	SessionID            acp.SessionId
	Store                acpstar.SessionStore
	Conn                 acpConn

	mu   sync.Mutex
	repl *starlarkREPL
}

// Reset discards all REPL globals and creates a fresh thread on the next call.
func (c *starlarkREPLCallback) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.repl != nil {
		_ = c.repl.Close()
	}
	c.repl = nil
}

// Call validates execution limits, evaluates one chunk in the session sphere,
// reports status updates, and returns printed text or structured errors and
// artifacts while preserving REPL state between calls.
func (c *starlarkREPLCallback) Call(ctx context.Context, params map[string]any) (gai.Message, error) {
	sendToolCallUpdate := func(status acp.ToolCallStatus, blocks []gai.Block) error {
		if c.Conn == nil {
			return nil
		}
		updateCtx := ctx
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			updateCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
		}
		update := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(updateCtx))
		update.Kind = new(acp.ToolKindOther)
		update.Status = &status
		if len(blocks) > 0 {
			update.Content = xacp.BlocksToToolCallContent(blocks)
		}
		if err := c.Conn.SessionUpdate(updateCtx, &acp.SessionNotification{
			SessionID: c.SessionID,
			Update:    update,
		}); err != nil {
			return fmt.Errorf("send %s tool call update: %w", status, err)
		}
		return nil
	}

	input, err := mapstruct.Map2Struct[starlarkREPLInput](params)
	if err != nil {
		msg := toolError("Error parsing parameters: " + err.Error())
		if err := sendToolCallUpdate(acp.ToolCallStatusFailed, msg.Blocks); err != nil {
			return gai.Message{}, err
		}
		return msg, nil
	}
	if input.ExecutionTimeout < 1 {
		msg := toolError("executionTimeout must be at least 1 second")
		if err := sendToolCallUpdate(acp.ToolCallStatusFailed, msg.Blocks); err != nil {
			return gai.Message{}, err
		}
		return msg, nil
	}
	maxAllowedTimeout := c.MaxTimeout
	if maxAllowedTimeout <= 0 {
		maxAllowedTimeout = 300
	}
	if input.ExecutionTimeout > maxAllowedTimeout {
		msg := toolError(fmt.Sprintf("executionTimeout exceeds maximum allowed (%d seconds)", maxAllowedTimeout))
		if err := sendToolCallUpdate(acp.ToolCallStatusFailed, msg.Blocks); err != nil {
			return gai.Message{}, err
		}
		return msg, nil
	}
	if err := ctx.Err(); err != nil {
		return gai.Message{}, err
	}

	if err := sendToolCallUpdate(acp.ToolCallStatusInProgress, nil); err != nil {
		return gai.Message{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.repl == nil {
		c.repl = newStarlarkREPL(
			c.Cwd,
			c.LargeOutputCharLimit,
			c.SessionID,
			c.Store,
		)
	}
	result, err := c.repl.Eval(ctx, input.Code, time.Duration(input.ExecutionTimeout)*time.Second)
	if ctxErr := ctx.Err(); ctxErr != nil && !result.TimedOut {
		err = ctxErr
	}
	if err != nil {
		errorText := err.Error()
		if evalErr, ok := errors.AsType[*starlark.EvalError](err); ok {
			errorText = evalErr.Backtrace()
		}
		text := "Starlark execution error:\n" + errorText
		if result.Output != "" {
			text += "\n\nOutput:\n" + result.Output
		}
		blocks := append([]gai.Block{gai.TextBlock(text)}, result.Content...)
		msg := gai.Message{
			Role:            gai.ToolResult,
			Blocks:          blocks,
			ToolResultError: true,
		}
		if err := sendToolCallUpdate(acp.ToolCallStatusFailed, msg.Blocks); err != nil {
			return gai.Message{}, err
		}
		return msg, nil
	}

	blocks := result.Content
	if result.Output != "" {
		blocks = append([]gai.Block{gai.TextBlock(result.Output)}, blocks...)
	}
	if len(blocks) == 0 {
		blocks = []gai.Block{gai.TextBlock("")}
	}
	msg := gai.Message{Role: gai.ToolResult, Blocks: blocks}
	if err := sendToolCallUpdate(acp.ToolCallStatusCompleted, msg.Blocks); err != nil {
		return gai.Message{}, err
	}
	return msg, nil
}

func toolError(text string) gai.Message {
	msg := gai.ToolResultMessage("", gai.TextBlock(text))
	msg.ToolResultError = true
	return msg
}
