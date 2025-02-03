package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"github.com/spachava753/cpe/internal/conversation"
)

// executorWrapper wraps an executor to handle conversation persistence
type executorWrapper struct {
	executor     Executor
	convoManager *conversation.Manager
	model       string
	userMessage string
	continueID  string
}

// Execute executes the executor and saves the conversation
func (w *executorWrapper) Execute(input string) error {
	// Execute the wrapped executor
	if err := w.executor.Execute(input); err != nil {
		return err
	}

	// Save the conversation
	var buf bytes.Buffer
	if err := w.executor.SaveMessages(&buf); err != nil {
		return fmt.Errorf("failed to save messages: %w", err)
	}

	var parentID *string
	if w.continueID != "" {
		parentID = &w.continueID
	}

	_, err := w.convoManager.CreateConversation(context.Background(), parentID, w.userMessage, buf.Bytes(), w.model)
	if err != nil {
		return fmt.Errorf("failed to create conversation: %w", err)
	}

	return nil
}

// LoadMessages loads messages into the wrapped executor
func (w *executorWrapper) LoadMessages(r io.Reader) error {
	return w.executor.LoadMessages(r)
}

// SaveMessages saves messages from the wrapped executor
func (w *executorWrapper) SaveMessages(w io.Writer) error {
	return w.executor.SaveMessages(w)
}