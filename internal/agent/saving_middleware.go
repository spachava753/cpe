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
// BEFORE Generate: Walks the dialog and saves any messages without an ID,
// deriving the parent chain from the dialog structure.
//
// AFTER Generate: Saves the assistant response with the correct parent.
//
// Note on atomicity: The user message is saved before calling the inner generator,
// and the assistant response is saved after. If the assistant save fails, the user
// message remains saved but without a linked response. This is acceptable because:
// 1. Save failures are treated as unrecoverable errors that terminate execution
// 2. The database remains consistent (just an incomplete conversation)
// 3. Full transactional atomicity would require significant complexity
type SavingMiddleware struct {
	gai.GeneratorWrapper
	storage storage.MessagesSaver
}

// NewSavingMiddleware creates a new SavingMiddleware.
func NewSavingMiddleware(g gai.Generator, storage storage.MessagesSaver) *SavingMiddleware {
	return &SavingMiddleware{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
		storage:          storage,
	}
}

// WithSaving returns a WrapperFunc for use with gai.Wrap.
func WithSaving(storage storage.MessagesSaver) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewSavingMiddleware(g, storage)
	}
}

// Generate implements the gai.Generator interface.
// It saves unsaved messages before calling the inner generator and saves
// the assistant response after.
//
// The label parameter passed to SaveMessages is always empty string for normal
// interactive usage. Labels are used by subagent execution to annotate saved
// messages with subagent identity (e.g., "subagent:name:run_id").
func (m *SavingMiddleware) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	// BEFORE: Walk dialog, save messages without IDs, derive parent chain
	var lastID string
	for i := range dialog {
		if id := GetMessageID(dialog[i]); id != "" {
			lastID = id
			continue
		}
		// No ID - save it (label is empty for interactive usage)
		for id, err := range m.storage.SaveMessages(ctx, slices.Values([]storage.SaveMessageOptions{
			{Message: dialog[i], ParentID: lastID, Title: ""},
		})) {
			if err != nil {
				return gai.Response{}, fmt.Errorf("failed to save message: %w", err)
			}
			SetMessageID(&dialog[i], id)
			lastID = id
		}
	}

	resp, err := m.GeneratorWrapper.Generate(ctx, dialog, opts)
	if err != nil {
		return resp, err
	}

	// AFTER: Save assistant response (label is empty for interactive usage)
	if len(resp.Candidates) > 0 {
		for id, err := range m.storage.SaveMessages(ctx, slices.Values([]storage.SaveMessageOptions{
			{Message: resp.Candidates[0], ParentID: lastID, Title: ""},
		})) {
			if err != nil {
				return gai.Response{}, fmt.Errorf("failed to save assistant message: %w", err)
			}
			SetMessageID(&resp.Candidates[0], id)
		}
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
