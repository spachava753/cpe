package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/conversation"
)

const compactName = "compact"

type compactionKey struct{}

type compactionRequest struct {
	summary *string
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
func (r compactionRequest) checkpoint(afterMessages int) *conversation.Compaction {
	if r.summary == nil {
		return nil
	}
	return &conversation.Compaction{
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
