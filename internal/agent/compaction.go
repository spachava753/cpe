package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
)

const compactName = "compact"

type compactionKey struct{}

type compactionRequest struct {
	summary *string
}

// Compaction replaces the first AfterMessages transcript messages in model context.
// The original transcript remains intact.
type Compaction struct {
	Summary       string
	AfterMessages int
}

func compact(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var summary string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"summary",
		&summary,
	); err != nil {
		return nil, err
	}
	if strings.TrimSpace(summary) == "" {
		return nil, errors.New("summary must be a nonempty string")
	}
	ctx := dyson.EvaluationContext(thread)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request, ok := ctx.Value(compactionKey{}).(*compactionRequest)
	if !ok {
		return nil, errors.New("compact requires an active prompt")
	}
	request.summary = new(summary)
	return starlark.None, nil
}

// Resolve the boundary only after the response's calls have finished or been settled.
func (r compactionRequest) checkpoint(afterMessages int) *Compaction {
	if r.summary == nil {
		return nil
	}
	return &Compaction{
		Summary:       *r.summary,
		AfterMessages: afterMessages,
	}
}

func (h *turnHooks) checkpointCompaction(ctx context.Context) error {
	h.session.mu.Lock()
	record := h.session.record
	h.session.mu.Unlock()
	record.Dialog = slices.Clone(h.dialog)
	record.Compaction = h.compactionRequest.checkpoint(len(h.dialog))
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
	h.compaction = record.Compaction
	h.compactionRequest.summary = nil
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
