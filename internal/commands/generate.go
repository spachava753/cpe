package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/types"
)

// DialogLoader is an interface for loading conversation history
type DialogLoader interface {
	GetMostRecentAssistantMessageId(ctx context.Context) (string, error)
	GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error)
}

// DialogStorage is a type alias for types.DialogSaver.
// Used by subagent execution to persist execution traces.
type DialogStorage = types.DialogSaver

// GenerateOptions contains all parameters for the generate command
type GenerateOptions struct {
	// User input
	UserBlocks []gai.Block

	// Conversation state
	ContinueID      string
	NewConversation bool

	GenOptsFunc gai.GenOptsGenerator

	// Dependencies
	DialogLoader DialogLoader
	Generator    types.Generator

	// Output
	Stderr io.Writer
}

// Generate executes the main generation logic.
// Saving is handled by the SavingMiddleware in the generator pipeline.
func Generate(ctx context.Context, opts GenerateOptions) error {
	if len(opts.UserBlocks) == 0 {
		return errors.New("empty input")
	}

	// Determine continue ID if not explicitly set
	continueID := opts.ContinueID
	newConversation := opts.NewConversation

	if continueID == "" && !newConversation && opts.DialogLoader != nil {
		var err error
		continueID, err = opts.DialogLoader.GetMostRecentAssistantMessageId(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			// No previous conversation found - start new
			newConversation = true
		}
	}

	// Build user message
	userMessage := gai.Message{
		Role:   gai.User,
		Blocks: opts.UserBlocks,
	}

	// Create dialog
	dialog := gai.Dialog{userMessage}

	if !newConversation && opts.DialogLoader != nil {
		var err error
		dialog, _, err = opts.DialogLoader.GetDialogForMessage(ctx, continueID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("failed to get previous dialog: %w", err)
		}
		dialog = append(dialog, userMessage)
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
