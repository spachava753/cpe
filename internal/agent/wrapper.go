package agent

import (
	"bytes"
	"context"
	"fmt"
	"github.com/spachava753/cpe/internal/conversation"
	"io"
	"strings"
)

// executorWrapper wraps an executor to handle conversation persistence
type executorWrapper struct {
	executor     Executor
	convoManager *conversation.Manager
	model        string
	userMessage  string
	continueID   string
}

// Execute executes the executor and saves the conversation
func (e *executorWrapper) Execute(inputs []Input) error {
	defer e.convoManager.Close()  // Close the database connection when we're done

	// Store the input as the user message
	// For now, just store the text inputs
	var textInputs []string
	for _, input := range inputs {
		if input.Type == InputTypeText {
			textInputs = append(textInputs, input.Text)
		}
	}
	e.userMessage = strings.Join(textInputs, "\n")

	// Execute the wrapped executor
	if err := e.executor.Execute(inputs); err != nil {
		return err
	}

	// Save the conversation
	var buf bytes.Buffer
	if err := e.executor.SaveMessages(&buf); err != nil {
		return fmt.Errorf("failed to save messages: %w", err)
	}

	var parentID *string
	if e.continueID != "" {
		parentID = &e.continueID
	}

	_, err := e.convoManager.CreateConversation(context.Background(), parentID, e.userMessage, buf.Bytes(), e.model)
	if err != nil {
		return fmt.Errorf("failed to create conversation: %w", err)
	}

	return nil
}

// LoadMessages loads messages into the wrapped executor
func (e *executorWrapper) LoadMessages(r io.Reader) error {
	return e.executor.LoadMessages(r)
}

// SaveMessages saves messages from the wrapped executor
func (e *executorWrapper) SaveMessages(w io.Writer) error {
	return e.executor.SaveMessages(w)
}

func (e *executorWrapper) PrintMessages() string {
	return e.executor.PrintMessages()
}
