package conversation

import (
	"errors"
	"strings"

	"github.com/spachava753/gai"
)

// Compaction replaces the first AfterMessages transcript messages in model context.
// The original transcript remains intact.
type Compaction struct {
	Summary       string
	AfterMessages int
}

// WorkingDialog replaces the compacted prefix without changing the transcript.
// The compaction boundary must have passed Record.Validate. A nil receiver keeps the full dialog.
func (c *Compaction) WorkingDialog(dialog gai.Dialog) gai.Dialog {
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

func (r Record) checkCompaction() error {
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
