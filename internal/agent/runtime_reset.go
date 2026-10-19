package agent

import "github.com/spachava753/gai"

const runtimeResetNotice = "[Dyson/Starlark reset here: earlier variables, functions, and imports are unavailable. Rebuild needed state. Files and external effects may remain; inspect them before repeating actions. State created after this point persists normally.]"

// runtimeReset marks the end of history inherited by a fresh Dyson sphere.
type runtimeReset struct {
	afterMessages int
}

func (r *runtimeReset) workingDialog(dialog gai.Dialog, compaction *Compaction) gai.Dialog {
	if r == nil || compaction != nil && compaction.AfterMessages > r.afterMessages {
		return compaction.workingDialog(dialog)
	}
	// Compact only the inherited prefix so the notice follows even a restored summary.
	prefix := compaction.workingDialog(dialog[:r.afterMessages])
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
