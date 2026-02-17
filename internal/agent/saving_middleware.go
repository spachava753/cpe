package agent

import (
	"context"
	"fmt"
	"slices"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

// SavingMiddleware is a stateless generator middleware that incrementally saves
// messages as they flow through the generation pipeline.
//
// BEFORE Generate: Saves the entire dialog (including any unsaved messages)
// via SaveDialog, which handles the parent chain automatically.
//
// AFTER Generate: Saves the full dialog including the new assistant response.
//
// Note on atomicity: The dialog is saved before calling the inner generator,
// and the dialog plus assistant response is saved after. If the assistant save
// fails, the user messages remain saved but without a linked response. This is
// acceptable because:
// 1. Save failures are treated as unrecoverable errors that terminate execution
// 2. The database remains consistent (just an incomplete conversation)
// 3. SaveDialog uses transactions internally for each call
type SavingMiddleware struct {
	gai.GeneratorWrapper
	storage storage.DialogSaver
}

// NewSavingMiddleware creates a new SavingMiddleware.
func NewSavingMiddleware(g gai.Generator, storage storage.DialogSaver) *SavingMiddleware {
	return &SavingMiddleware{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
		storage:          storage,
	}
}

// WithSaving returns a WrapperFunc for use with gai.Wrap.
func WithSaving(storage storage.DialogSaver) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewSavingMiddleware(g, storage)
	}
}

// Generate implements the gai.Generator interface.
// It saves unsaved messages before calling the inner generator and saves
// the assistant response after.
func (m *SavingMiddleware) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	// BEFORE: Save the entire dialog. Already-persisted messages (those with
	// MessageIDKey set) are verified but not re-saved. New messages get IDs assigned.
	idx := 0
	for saved, err := range m.storage.SaveDialog(ctx, slices.Values(dialog)) {
		if err != nil {
			return gai.Response{}, fmt.Errorf("failed to save dialog: %w", err)
		}
		dialog[idx] = saved
		idx++
	}

	resp, err := m.GeneratorWrapper.Generate(ctx, dialog, opts)
	if err != nil {
		return resp, err
	}

	// AFTER: Save the full dialog including the new assistant response.
	if len(resp.Candidates) > 0 {
		fullDialog := append(dialog, resp.Candidates[0])
		idx = 0
		for saved, err := range m.storage.SaveDialog(ctx, slices.Values(fullDialog)) {
			if err != nil {
				return gai.Response{}, fmt.Errorf("failed to save assistant message: %w", err)
			}
			fullDialog[idx] = saved
			idx++
		}
		// Write the saved assistant message (with MessageIDKey populated) back
		// into resp.Candidates so outer middlewares (e.g. ResponsePrinter) can
		// read the ID.
		resp.Candidates[0] = fullDialog[len(fullDialog)-1]
	}

	return resp, nil
}

// GetMessageID retrieves the message ID from a message's ExtraFields.
// Returns an empty string if no ID is set.
func GetMessageID(msg gai.Message) string {
	if msg.ExtraFields == nil {
		return ""
	}
	id, _ := msg.ExtraFields[storage.MessageIDKey].(string)
	return id
}

// SetMessageID sets the message ID in a message's ExtraFields.
// NOTE: This mutates the message in place. This is safe because dialogs
// are built fresh for each Generate call and not shared across goroutines.
func SetMessageID(msg *gai.Message, id string) {
	if msg.ExtraFields == nil {
		msg.ExtraFields = make(map[string]any)
	}
	msg.ExtraFields[storage.MessageIDKey] = id
}
