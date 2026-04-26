package commands

import (
	"context"
	"io"
	"os"

	"github.com/spachava753/cpe/internal/render"
)

// ConversationListFromConfigOptions contains CLI-facing options for listing
// stored conversations.
type ConversationListFromConfigOptions struct {
	ConversationStoragePath string
	Writer                  io.Writer
	TreePrinter             TreePrinter
}

// ConversationListFromConfig opens configured storage and prints the message
// tree using the provided or default formatter.
func ConversationListFromConfig(ctx context.Context, opts ConversationListFromConfigOptions) error {
	db, dialogStorage, err := OpenConversationStorage(ctx, opts.ConversationStoragePath)
	if err != nil {
		return err
	}
	defer db.Close()

	treePrinter := opts.TreePrinter
	if treePrinter == nil {
		treePrinter = &DefaultTreePrinter{}
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return ConversationList(ctx, ConversationListOptions{
		Storage:     dialogStorage,
		Writer:      writer,
		TreePrinter: treePrinter,
	})
}

// ConversationDeleteFromConfigOptions contains CLI-facing options for deleting
// messages from configured storage.
type ConversationDeleteFromConfigOptions struct {
	ConversationStoragePath string
	MessageIDs              []string
	Cascade                 bool
	Stdout                  io.Writer
	Stderr                  io.Writer
}

// ConversationDeleteFromConfig opens configured storage and deletes messages.
func ConversationDeleteFromConfig(ctx context.Context, opts ConversationDeleteFromConfigOptions) error {
	db, dialogStorage, err := OpenConversationStorage(ctx, opts.ConversationStoragePath)
	if err != nil {
		return err
	}
	defer db.Close()

	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	return ConversationDelete(ctx, ConversationDeleteOptions{
		Storage:    dialogStorage,
		MessageIDs: opts.MessageIDs,
		Cascade:    opts.Cascade,
		Stdout:     stdout,
		Stderr:     stderr,
	})
}

// ConversationPrintFromConfigOptions contains CLI-facing options for printing a
// conversation thread from configured storage.
type ConversationPrintFromConfigOptions struct {
	ConversationStoragePath string
	MessageID               string
	Writer                  io.Writer
	DialogFormatter         DialogFormatter
}

// ConversationPrintFromConfig opens configured storage and prints a thread.
func ConversationPrintFromConfig(ctx context.Context, opts ConversationPrintFromConfigOptions) error {
	db, dialogStorage, err := OpenConversationStorage(ctx, opts.ConversationStoragePath)
	if err != nil {
		return err
	}
	defer db.Close()

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	formatter := opts.DialogFormatter
	if formatter == nil {
		formatter = &MarkdownDialogFormatter{Renderer: &render.PlainTextRenderer{}}
		if render.IsTTYWriter(writer) {
			formatter = &MarkdownDialogFormatter{Renderer: render.NewGlamourRendererForWriter(writer)}
		}

	}

	return ConversationPrint(ctx, ConversationPrintOptions{
		Storage:         dialogStorage,
		MessageID:       opts.MessageID,
		Writer:          writer,
		DialogFormatter: formatter,
	})
}
