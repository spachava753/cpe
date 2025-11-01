package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/gai"
)

// DialogStorage is an interface for conversation storage operations
type DialogStorage interface {
	GetMostRecentAssistantMessageId(ctx context.Context) (string, error)
	GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error)
	SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error)
	Close() error
}

// ToolCapableGenerator is an interface for AI generation with tool support
type ToolCapableGenerator interface {
	Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
}

// GenerateOptions contains all parameters for the generate command
type GenerateOptions struct {
	// User input
	UserBlocks []gai.Block
	
	// Model and configuration
	Config        *config.Config
	ModelName     string
	SystemPrompt  string
	
	// Conversation state
	ContinueID      string
	NewConversation bool
	IncognitoMode   bool
	
	// Generation parameters (CLI overrides)
	GenerationOverrides *config.GenerationParams
	
	// Dependencies
	Storage   DialogStorage
	Generator ToolCapableGenerator
	
	// Output
	Stdout io.Writer
	Stderr io.Writer
}

// Generate executes the main generation logic
func Generate(ctx context.Context, opts GenerateOptions) error {
	if len(opts.UserBlocks) == 0 {
		return errors.New("empty input")
	}
	
	if opts.ModelName == "" {
		return errors.New("no model specified")
	}
	
	// Find the model in configuration
	selectedModel, found := opts.Config.FindModel(opts.ModelName)
	if !found {
		return fmt.Errorf("model %q not found in configuration", opts.ModelName)
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
	
	// Get effective generation parameters
	effective := selectedModel.GetEffectiveGenerationParams(
		opts.Config.Defaults.GenerationParams,
		opts.GenerationOverrides,
	)
	
	// Create generation options function
	genOptsGen := func(d gai.Dialog) *gai.GenOpts {
		genOpts := &gai.GenOpts{}
		
		// Set max tokens from model max_output or effective params
		genOpts.MaxGenerationTokens = int(selectedModel.Model.MaxOutput)
		if effective.MaxTokens != nil {
			genOpts.MaxGenerationTokens = *effective.MaxTokens
		}
		
		// Apply effective parameters
		if effective.Temperature != nil {
			genOpts.Temperature = *effective.Temperature
		}
		if effective.TopP != nil {
			genOpts.TopP = *effective.TopP
		}
		if effective.TopK != nil {
			genOpts.TopK = uint(*effective.TopK)
		}
		if effective.FrequencyPenalty != nil {
			genOpts.FrequencyPenalty = *effective.FrequencyPenalty
		}
		if effective.PresencePenalty != nil {
			genOpts.PresencePenalty = *effective.PresencePenalty
		}
		if effective.NumberOfResponses != nil {
			genOpts.N = uint(*effective.NumberOfResponses)
		}
		if effective.ThinkingBudget != nil {
			genOpts.ThinkingBudget = *effective.ThinkingBudget
		}
		
		return genOpts
	}
	
	// Generate the response
	resultDialog, err := opts.Generator.Generate(ctx, dialog, genOptsGen)
	
	// Add separator
	fmt.Fprintf(opts.Stdout, "\n\n")
	
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
	
	// Determine assistant messages from result
	assistantMsgs := resultDialog[len(dialog):]
	
	shouldSave := len(assistantMsgs) > 0
	
	if !shouldSave && interrupted {
		fmt.Fprintln(opts.Stderr, "No new assistant messages to save from interrupted generation. Skipping save for this turn.")
		return nil
	}
	
	// Save user message
	userMsgID, err := opts.Storage.SaveMessage(ctx, userMessage, parentID, "")
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
		currentParentID, err = opts.Storage.SaveMessage(ctx, assistantMsg, currentParentID, "")
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
		if id, err := opts.Storage.GetMostRecentAssistantMessageId(ctx); err == nil {
			lastID = id
		}
	}
	if lastID != "" {
		fmt.Fprintf(opts.Stderr, "[cpe] last_message_id is %s\n", lastID)
	}
	
	return nil
}
