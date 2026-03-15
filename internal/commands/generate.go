package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/ports"
	"github.com/spachava753/cpe/internal/storage"
)

// GenerateOptions contains all parameters for Generate.
type GenerateOptions struct {
	// UserBlocks is the new user turn content. It must be non-empty.
	UserBlocks []gai.Block

	// InitialDialog is the existing root-to-leaf history to continue from.
	// When non-empty, the new user message is appended to this dialog.
	// Callers typically populate this via ResolveInitialDialog.
	InitialDialog gai.Dialog

	// GenOptsFunc provides per-request generator options.
	GenOptsFunc gai.GenOptsGenerator

	// Generator performs model inference and middleware execution.
	Generator ports.Generator

	// Stderr receives non-fatal generation errors.
	Stderr io.Writer
}

// Generate appends the new user message to opts.InitialDialog and runs the
// generation pipeline.
//
// Persistence is delegated to middleware (SavingMiddleware) attached to
// opts.Generator. This function does not call storage directly.
//
// Contract note: input validation errors are returned, but model-generation
// failures are reported to opts.Stderr and the function still returns nil
// (except context cancellation, which is treated as silent termination).
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
		fmt.Fprintf(opts.Stderr, "Error generating response: %s\n", formatGenerationError(err))
	}

	return nil
}

func formatGenerationError(err error) string {
	if err == nil {
		return ""
	}

	var message string

	var apiErr *gai.ApiErr
	if errors.As(err, &apiErr) {
		message = appendErrorHint(apiErr.Error(), apiErrorHint(apiErr))
		return appendErrorHint(message, generationHint(err))
	}

	var contentPolicyErr gai.ContentPolicyErr
	if errors.As(err, &contentPolicyErr) {
		message = appendErrorHint(contentPolicyErr.Error(), "Adjust the prompt or inputs to comply with the provider policy")
		return appendErrorHint(message, generationHint(err))
	}

	switch {
	case errors.Is(err, gai.ContextLengthExceededErr):
		message = appendErrorHint(err.Error(), "Shorten the prompt, reduce attached input, or compact the conversation")
	case errors.Is(err, gai.MaxGenerationLimitErr):
		message = appendErrorHint(err.Error(), "Increase the token limit or refine the prompt")
	default:
		message = err.Error()
	}
	return appendErrorHint(message, generationHint(err))
}

type generationHintProvider interface {
	GenerationHint() string
}

func generationHint(err error) string {
	var hintProvider generationHintProvider
	if errors.As(err, &hintProvider) {
		return hintProvider.GenerationHint()
	}
	return ""
}

func apiErrorHint(apiErr *gai.ApiErr) string {
	if apiErr == nil {
		return ""
	}

	switch apiErr.Kind {
	case gai.APIErrorKindAuthentication, gai.APIErrorKindPermission:
		return "Check the configured credentials and provider access"
	case gai.APIErrorKindRateLimit, gai.APIErrorKindTimeout, gai.APIErrorKindServer, gai.APIErrorKindServiceUnavailable, gai.APIErrorKindOverloaded:
		return "This looks transient; retry later or try another model"
	case gai.APIErrorKindInvalidRequest, gai.APIErrorKindRequestTooLarge:
		return "Check the request parameters, tool inputs, and attached content size"
	case gai.APIErrorKindNotFound:
		return "Check the configured model ID and provider base URL"
	case gai.APIErrorKindContentPolicy:
		return "Adjust the prompt or inputs to comply with the provider policy"
	default:
		return ""
	}
}

func appendErrorHint(message, hint string) string {
	if hint == "" {
		return message
	}
	if strings.HasSuffix(message, ".") || strings.HasSuffix(message, "!") || strings.HasSuffix(message, "?") {
		return message + " " + hint
	}
	return message + ". " + hint
}

// DialogResolver provides the read operations ResolveInitialDialog needs.
//
// ResolveInitialDialog depends on ListMessages default ordering semantics
// (newest-first) for auto-continue selection.
type DialogResolver interface {
	storage.MessagesLister
	storage.MessagesGetter
}

// ResolveInitialDialog selects the history prefix for a generate request.
//
// Selection precedence:
//   - If newConversation is true, returns nil (force fresh conversation).
//   - Else if continueID is provided, loads the chain ending at that ID.
//   - Else auto-continue: scans ListMessages (default newest-first) and picks
//     the first non-subagent assistant/tool_result message.
//
// Auto-continue requires MessageIDKey and MessageIsSubagentKey metadata on
// listed messages. If no eligible message exists, returns nil to start fresh.
func ResolveInitialDialog(ctx context.Context, resolver DialogResolver, continueID string, newConversation bool) (gai.Dialog, error) {
	if newConversation {
		return nil, nil
	}

	// If no explicit continue ID, auto-detect from most recent message
	if continueID == "" {
		msgs, err := resolver.ListMessages(ctx, storage.ListMessagesOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list messages for auto-continue: %w", err)
		}
		for msg := range msgs {
			if isSubagent, ok := msg.ExtraFields[storage.MessageIsSubagentKey].(bool); ok && isSubagent {
				continue
			}
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
