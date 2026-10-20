package agent

import (
	"context"
	"slices"
	"time"

	"github.com/spachava753/cpe/internal/conversation"
)

const compactName = "compact"

type compactionRequest struct {
	summary *string
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
