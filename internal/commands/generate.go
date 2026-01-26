package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spachava753/cpe/internal/types"
	"github.com/spachava753/gai"
)

// DialogStorage is an interface for conversation storage operations
type DialogStorage interface {
	GetMostRecentAssistantMessageId(ctx context.Context) (string, error)
	GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error)
	SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error)
	Close() error
}

// GenerateOptions contains all parameters for the generate command
type GenerateOptions struct {
	// User input
	UserBlocks []gai.Block

	// Conversation state
	ContinueID      string
	NewConversation bool
	IncognitoMode   bool

	GenOptsFunc gai.GenOptsGenerator

	// Dependencies
	Storage   DialogStorage
	Generator types.Generator

	// Output
	Stderr io.Writer
}

// Generate executes the main generation logic
func Generate(ctx context.Context, opts GenerateOptions) error {
	if len(opts.UserBlocks) == 0 {
		return errors.New("empty input")
	}

	// Determine continue ID if not explicitly set
	continueID := opts.ContinueID
	newConversation := opts.NewConversation

	if continueID == "" && !newConversation && opts.Storage != nil {
		var err error
		continueID, err = opts.Storage.GetMostRecentAssistantMessageId(ctx)
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
	var msgIdList []string

	if !newConversation && opts.Storage != nil {
		var err error
		dialog, msgIdList, err = opts.Storage.GetDialogForMessage(ctx, continueID)
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
	resultDialog, err := opts.Generator.Generate(ctx, dialog, opts.GenOptsFunc)

	interrupted := errors.Is(err, context.Canceled)
	if err != nil && !interrupted {
		fmt.Fprintf(opts.Stderr, "Error generating response: %v\n", err)
	}

	// Don't save in incognito mode
	if opts.IncognitoMode || opts.Storage == nil {
		return nil
	}

	// Determine parent ID
	var parentID string
	if len(msgIdList) != 0 {
		parentID = msgIdList[len(msgIdList)-1]
	}

	// Warn about interrupted generation
	if interrupted {
		fmt.Fprintln(opts.Stderr, "WARNING: Generation was interrupted. Attempting to save partial dialog.")
	}
	fmt.Fprintln(opts.Stderr, "You can cancel this save operation by interrupting (Ctrl+C).")

	// Create a new context for save operation that can be cancelled independently
	// This allows the user to interrupt the save with a second Ctrl+C
	//nolint:contextcheck // Intentional: save operations should complete even if parent context is cancelled
	saveCtx, saveCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer saveCancel()

	// Determine assistant messages from result
	assistantMsgs := resultDialog[len(dialog):]

	shouldSave := len(assistantMsgs) > 0

	if !shouldSave && interrupted {
		fmt.Fprintln(opts.Stderr, "No new assistant messages to save from interrupted generation. Skipping save for this turn.")
		return nil
	}

	// Save user message
	userMsgID, err := opts.Storage.SaveMessage(saveCtx, userMessage, parentID, "") //nolint:contextcheck // Intentional: save operations should complete even if parent context is cancelled
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(opts.Stderr, "Save operation cancelled by user.")
			return nil
		}
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Save assistant messages
	currentParentID := userMsgID
	for _, assistantMsg := range assistantMsgs {
		currentParentID, err = opts.Storage.SaveMessage(saveCtx, assistantMsg, currentParentID, "") //nolint:contextcheck // Intentional: save operations should complete even if parent context is cancelled
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(opts.Stderr, "Save operation cancelled by user during assistant message saving.")
				return nil
			}
			return fmt.Errorf("failed to save assistant message: %w", err)
		}
	}

	if interrupted && len(assistantMsgs) > 0 {
		fmt.Fprintln(opts.Stderr, "Partial dialog saved successfully.")
	}

	// Print the last message's ID
	lastID := currentParentID
	if lastID == "" {
		if id, err := opts.Storage.GetMostRecentAssistantMessageId(saveCtx); err == nil { //nolint:contextcheck // Intentional: save operations should complete even if parent context is cancelled
			lastID = id
		}
	}
	if lastID != "" {
		fmt.Fprintf(opts.Stderr, "[cpe] last_message_id is %s\n", lastID)
	}

	return nil
}
