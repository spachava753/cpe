package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
)

const compactToolName = "compact"

// Compaction replaces the first AfterMessages transcript messages in model context.
// The original transcript remains intact, including the compact call and its result.
type Compaction struct {
	Summary       string
	AfterMessages int
}

func (h *turnHooks) compactionTool() gaiagent.Tool {
	return gaiagent.Tool{
		Definition: gai.Tool{
			Name:        compactToolName,
			Description: "Compact earlier conversation context into a summary.",
			InputSchema: stringSchema("summary"),
		},
		Handler: gaiagent.ToolHandlerFunc(h.compact),
	}
}

func (h *turnHooks) compact(ctx context.Context, request gaiagent.ToolRequest) (gai.Message, error) {
	if err := ctx.Err(); err != nil {
		return gai.Message{}, err
	}
	summary, ok := request.Call.Parameters["summary"].(string)
	if !ok || strings.TrimSpace(summary) == "" {
		result := gai.ToolResultMessage(
			request.Block.ID,
			gai.TextBlock("summary must be a nonempty string"),
		)
		result.ToolResultError = true
		return result, nil
	}
	// Keep the current response's whole tool batch, including results not yet available.
	h.pendingCompaction = &Compaction{
		Summary:       summary,
		AfterMessages: len(h.dialog),
	}
	return gai.ToolResultMessage(
		request.Block.ID,
		gai.TextBlock("Summary accepted for compaction."),
	), nil
}

func (h *turnHooks) checkpointCompaction(ctx context.Context) error {
	h.session.mu.Lock()
	record := h.session.record
	h.session.mu.Unlock()
	record.Dialog = slices.Clone(h.dialog)
	record.Compaction = h.pendingCompaction
	record.Info.UpdatedAt = time.Now().UTC()
	if err := h.session.deps.History.Save(
		ctx,
		record,
	); err != nil {
		return err
	}
	h.session.mu.Lock()
	h.session.record = record
	h.session.mu.Unlock()
	h.compaction = h.pendingCompaction
	h.pendingCompaction = nil
	return nil
}

func (c *Compaction) workingDialog(dialog gai.Dialog) gai.Dialog {
	if c == nil {
		return dialog
	}
	return append(
		gai.Dialog{
			{
				Role: gai.User,
				Blocks: []gai.Block{
					gai.TextBlock("Summary of earlier conversation:\n\n" + c.Summary),
				},
			},
		},
		dialog[c.AfterMessages:]...,
	)
}

func (r SessionRecord) checkCompaction() error {
	if r.Compaction == nil {
		return nil
	}
	c := r.Compaction
	if strings.TrimSpace(c.Summary) == "" || c.AfterMessages < 0 || c.AfterMessages > len(r.Dialog) {
		return errors.New("invalid compaction summary or transcript boundary")
	}
	pending := make(map[string]bool)
	for _, message := range r.Dialog[:c.AfterMessages] {
		for _, block := range message.Blocks {
			if block.BlockType == gai.ToolCall {
				pending[block.ID] = true
			} else if message.Role == gai.ToolResult {
				delete(
					pending,
					block.ID,
				)
			}
		}
	}
	if len(pending) != 0 {
		return errors.New("compaction boundary splits a tool call from its result")
	}
	return nil
}
