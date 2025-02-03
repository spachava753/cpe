package conversation

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spachava753/cpe/internal/db"
)

// CLI handles conversation management command-line operations
type CLI struct {
	manager *Manager
}

// NewCLI creates a new CLI handler
func NewCLI(dbPath string) (*CLI, error) {
	manager, err := NewManager(dbPath)
	if err != nil {
		return nil, err
	}
	return &CLI{manager: manager}, nil
}

// Close closes the CLI handler
func (c *CLI) Close() error {
	return c.manager.Close()
}

// ListConversations prints all conversations in a tabulated format
func (c *CLI) ListConversations(ctx context.Context) error {
	conversations, err := c.manager.ListConversations(ctx)
	if err != nil {
		return fmt.Errorf("failed to list conversations: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
	fmt.Fprintln(w, "Id\tParent\tUser Message\tModel\tCreated At")
	fmt.Fprintln(w, strings.Repeat("-", 100))

	for _, conv := range conversations {
		parentID := "N/A"
		if conv.ParentID.Valid {
			parentID = conv.ParentID.String
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			conv.ID,
			parentID,
			conv.UserMessage,
			conv.Model,
			conv.CreatedAt.Format(time.RFC3339),
		)
	}
	return w.Flush()
}

// PrintConversation prints the contents of a specific conversation
func (c *CLI) PrintConversation(ctx context.Context, id string) error {
	conv, err := c.manager.GetConversation(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get conversation: %w", err)
	}

	fmt.Printf("Conversation ID: %s\n", conv.ID)
	if conv.ParentID.Valid {
		fmt.Printf("Parent ID: %s\n", conv.ParentID.String)
	}
	fmt.Printf("Model: %s\n", conv.Model)
	fmt.Printf("Created At: %s\n", conv.CreatedAt.Format(time.RFC3339))
	fmt.Printf("\nUser Message:\n%s\n", conv.UserMessage)

	return nil
}

// DeleteConversation deletes a conversation and optionally its children
func (c *CLI) DeleteConversation(ctx context.Context, id string, cascade bool) error {
	if err := c.manager.DeleteConversation(ctx, id, cascade); err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}
	return nil
}

// ValidateModel checks if the model matches the conversation's model
func (c *CLI) ValidateModel(ctx context.Context, id string, model string) error {
	conv, err := c.manager.GetConversation(ctx, id)
	if err != nil {
		return err
	}

	if conv.Model != model {
		return fmt.Errorf("cannot continue conversation from a different executor (conversation model: %s, requested model: %s)", conv.Model, model)
	}

	return nil
}