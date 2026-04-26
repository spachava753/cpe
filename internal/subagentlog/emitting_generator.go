package subagentlog

import (
	"context"
	"fmt"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/ports"
)

// finalAnswerToolName is filtered from streamed events because it is a terminal
// structured-output mechanism, not user-facing intermediate progress.
const (
	finalAnswerToolName = "final_answer"
	unknownToolName     = "unknown"
)

// EmittingGenerator wraps a generator to emit subagent logging events.
type EmittingGenerator struct {
	base         ports.Generator
	client       *Client
	subagentName string
	runID        string
	observed     bool
}

// NewEmittingGenerator creates an emitting wrapper around base. CPE runtimes
// receive an observer for chronological events; other generators use a fallback
// post-generation scan.
func NewEmittingGenerator(base ports.Generator, client *Client, subagentName, runID string) *EmittingGenerator {
	observed := false
	if registrar, ok := base.(ports.RuntimeObserverRegistrar); ok {
		registrar.SetRuntimeObserver(runtimeObserver{client: client, subagentName: subagentName, runID: runID})
		observed = true
	}
	return &EmittingGenerator{base: base, client: client, subagentName: subagentName, runID: runID, observed: observed}
}

// Generate delegates to the wrapped generator. When the base cannot accept a
// runtime observer, it falls back to scanning newly appended messages after the run.
func (g *EmittingGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	originalLen := len(dialog)
	resultDialog, err := g.base.Generate(ctx, dialog, optsGen)
	if err != nil {
		return resultDialog, err
	}
	if g.observed {
		return resultDialog, nil
	}
	observer := runtimeObserver{client: g.client, subagentName: g.subagentName, runID: g.runID}
	for offset, msg := range resultDialog[originalLen:] {
		switch msg.Role {
		case gai.Assistant:
			if err := emitFallbackAssistant(ctx, observer, resultDialog[:originalLen+offset+1], msg); err != nil {
				return nil, err
			}
		case gai.ToolResult:
			if len(msg.Blocks) == 0 {
				continue
			}
			call := gai.Block{ID: msg.Blocks[0].ID, BlockType: gai.ToolCall}
			if assistantMsg, ok := findNearestPrecedingAssistant(resultDialog, originalLen+offset); ok {
				for _, block := range assistantMsg.Blocks {
					if block.BlockType == gai.ToolCall && block.ID == msg.Blocks[0].ID {
						call = block
						break
					}
				}
			}
			if err := observer.ToolResult(ctx, resultDialog[:originalLen+offset+1], msg, call); err != nil {
				return nil, err
			}
		}
	}
	return resultDialog, nil
}

func emitFallbackAssistant(ctx context.Context, observer runtimeObserver, dialog gai.Dialog, msg gai.Message) error {
	for _, block := range msg.Blocks {
		if block.BlockType == gai.Thinking {
			if err := observer.ThoughtTrace(ctx, dialog, block); err != nil {
				return err
			}
		}
	}
	for _, block := range msg.Blocks {
		if block.BlockType == gai.ToolCall {
			if err := observer.ToolCall(ctx, dialog, block); err != nil {
				return err
			}
		}
	}
	return nil
}

// Register forwards tool registration without callback interception.
func (g *EmittingGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	registrar, ok := g.base.(ports.ToolRegistrar)
	if !ok {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
	}
	return registrar.Register(tool, callback)
}
