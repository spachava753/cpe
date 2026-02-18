package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/types"
)

// GenerateOptions contains all parameters for the generate command
type GenerateOptions struct {
	// User input
	UserBlocks []gai.Block

	// InitialDialog is the pre-loaded conversation history to continue from.
	// When non-empty, the new user message is appended to this dialog.
	// The caller is responsible for loading the dialog (e.g. via
	// storage.GetDialogForMessage or auto-continue detection).
	// When empty, a new conversation is started with just the user message.
	InitialDialog gai.Dialog

	GenOptsFunc gai.GenOptsGenerator

	// Dependencies
	Generator types.Generator

	// Output
	Stderr io.Writer
}

// Generate executes the main generation logic.
// Saving is handled by the SavingMiddleware in the generator pipeline.
func Generate(ctx context.Context, opts GenerateOptions) error {
	if len(opts.UserBlocks) == 0 {
		return errors.New("empty input")
	}

	// Build user message
	userMessage := gai.Message{
		Role:   gai.User,
		Blocks: opts.UserBlocks,
	}

	// Build dialog: append user message to initial dialog (if any)
	var dialog gai.Dialog
	if len(opts.InitialDialog) > 0 {
		dialog = append(opts.InitialDialog, userMessage)
	} else {
		dialog = gai.Dialog{userMessage}
	}

	// Validate that Generator is provided
	if opts.Generator == nil {
		return errors.New("no model specified")
	}

	// Generate the response
	// Saving is handled incrementally by the SavingMiddleware in the pipeline.
	// Message IDs are printed by the response/tool printers as messages are saved.
	_, err := opts.Generator.Generate(ctx, dialog, opts.GenOptsFunc)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(opts.Stderr, "Error generating response: %v\n", err)
	}

	return nil
}

// DialogResolver provides the read operations needed to resolve conversation
// history for continuation. It is a subset of storage.MessageDB containing
// only the interfaces required by ResolveInitialDialog.
type DialogResolver interface {
	storage.MessagesLister
	storage.MessagesGetter
}

// ResolveInitialDialog determines the conversation history to continue from.
// If continueID is set, it loads that specific conversation. If neither
// continueID nor newConversation is set, it auto-detects the most recent
// assistant/tool_result message and loads its conversation history.
// Returns nil when a new conversation should be started.
func ResolveInitialDialog(ctx context.Context, resolver DialogResolver, continueID string, newConversation bool) (gai.Dialog, error) {
	if newConversation {
		return nil, nil
	}

	// If no explicit continue ID, auto-detect from most recent message
	if continueID == "" {
		msgs, err := resolver.ListMessages(ctx, storage.ListMessagesOptions{})
		if err != nil {
			// No previous conversation found - start new
			return nil, nil //nolint:nilerr // intentional: treat list failure as empty history
		}
		for msg := range msgs {
			if msg.Role == gai.Assistant || msg.Role == gai.ToolResult {
				if id, ok := msg.ExtraFields[storage.MessageIDKey].(string); ok && id != "" {
					continueID = id
					break
				}
			}
		}
		if continueID == "" {
			// No assistant/tool_result message found - start new
			return nil, nil
		}
	}

	return storage.GetDialogForMessage(ctx, resolver, continueID)
}
