package agent

import (
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/conversation"
)

const runtimeResetNotice = "[Starlark state reset here: the session state store is empty. Rebuild needed data. Files and external effects may remain; inspect them before repeating actions. Newly stored data persists for this live session, but globals and imports last one call.]"

// runtimeReset marks the end of history inherited by a fresh session state store.
type runtimeReset struct {
	afterMessages int
}

func (r *runtimeReset) workingDialog(dialog gai.Dialog, compaction *conversation.Compaction) gai.Dialog {
	if r == nil || compaction != nil && compaction.AfterMessages > r.afterMessages {
		return compaction.WorkingDialog(dialog)
	}
	// Compact only the inherited prefix so the notice follows even a restored summary.
	prefix := compaction.WorkingDialog(dialog[:r.afterMessages])
	working := make(
		gai.Dialog,
		0,
		len(prefix)+1+len(dialog)-r.afterMessages,
	)
	working = append(
		working,
		prefix...,
	)
	working = append(
		working,
		gai.Message{
			Role: gai.User,
			Blocks: []gai.Block{
				gai.TextBlock(runtimeResetNotice),
			},
		},
	)
	return append(
		working,
		dialog[r.afterMessages:]...,
	)
}
