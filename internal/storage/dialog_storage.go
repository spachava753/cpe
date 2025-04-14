package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"github.com/matoous/go-nanoid/v2"
	"github.com/spachava753/gai"
	"slices"
)

//go:embed schema.sql
var schemaSQL string

// DialogStorage provides operations for storing and retrieving gai.Dialog objects.
// It serves as an abstraction over the implementation details of how messages
// are actually stored. Do not access its internal database directly.
type DialogStorage struct {
	db          *sql.DB
	q           *Queries
	idGenerator func() string
}

// InitDialogStorage initializes and returns a new DialogStorage instance
// This function opens or creates the database and initializes the schema
func InitDialogStorage(dbPath string) (*DialogStorage, error) {
	// Open or create the database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize schema from embedded SQL file
	_, err = db.Exec(schemaSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Create and return the dialog storage
	return &DialogStorage{
		db:          db,
		q:           New(db),
		idGenerator: func() string { return gonanoid.Must(6) },
	}, nil
}

// NewDialogStorage creates a new DialogStorage instance with an existing database connection
func NewDialogStorage(db *sql.DB) (*DialogStorage, error) {
	return &DialogStorage{
		db:          db,
		q:           New(db),
		idGenerator: func() string { return gonanoid.Must(6) },
	}, nil
}

// roleToString converts a gai.Role to its string representation
func roleToString(role gai.Role) string {
	switch role {
	case gai.User:
		return "user"
	case gai.Assistant:
		return "assistant"
	case gai.ToolResult:
		return "tool_result"
	default:
		return "unknown"
	}
}

// stringToRole converts a string to its gai.Role representation
func stringToRole(s string) (gai.Role, error) {
	switch s {
	case "user":
		return gai.User, nil
	case "assistant":
		return gai.Assistant, nil
	case "tool_result":
		return gai.ToolResult, nil
	default:
		return 0, fmt.Errorf("invalid role: %s", s)
	}
}

// GetBlocksByMessage retrieves all blocks associated with a message ID
func (s *DialogStorage) GetBlocksByMessage(ctx context.Context, messageID string) ([]Block, error) {
	return s.q.GetBlocksByMessage(ctx, messageID)
}

// HasChildrenByID checks if a message has any children
func (s *DialogStorage) HasChildrenByID(ctx context.Context, messageID string) (bool, error) {
	hasChildren, err := s.q.HasChildren(ctx, sql.NullString{String: messageID, Valid: true})
	if err != nil {
		return false, err
	}
	return hasChildren > 0, nil
}

// Close closes the underlying db connection
func (s *DialogStorage) Close() error {
	return s.db.Close()
}

// SaveMessage saves a single message and its blocks to the database
func (s *DialogStorage) SaveMessage(ctx context.Context, message gai.Message, parentID string, title string) (string, error) {
	// Generate a unique message ID
	messageID, err := s.generateUniqueID(ctx, s.checkMessageIDExists)
	if err != nil {
		return "", fmt.Errorf("failed to generate message ID: %w", err)
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	// Prepare parent ID parameter
	var parentIDParam sql.NullString
	if parentID != "" {
		parentIDParam = sql.NullString{String: parentID, Valid: true}
	}

	// Prepare title parameter
	var titleParam sql.NullString
	if title != "" {
		titleParam = sql.NullString{String: title, Valid: true}
	}

	// Create the message
	err = qtx.CreateMessage(ctx, CreateMessageParams{
		ID:              messageID,
		ParentID:        parentIDParam,
		Title:           titleParam,
		Role:            roleToString(message.Role),
		ToolResultError: message.ToolResultError,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	// Create all blocks for this message
	for j, block := range message.Blocks {
		// If the block already has an ID, use it; otherwise leave it as NULL
		var blockID sql.NullString
		if block.ID != "" {
			// Use the existing block ID
			blockID = sql.NullString{
				String: block.ID,
				Valid:  true,
			}
		}

		err = qtx.CreateBlock(ctx, CreateBlockParams{
			ID:            blockID,
			MessageID:     messageID,
			BlockType:     block.BlockType,
			ModalityType:  int64(block.ModalityType),
			MimeType:      block.MimeType,
			Content:       block.Content.String(),
			SequenceOrder: int64(j),
		})
		if err != nil {
			return "", fmt.Errorf("failed to create block: %w", err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return messageID, nil
}

// GetMessage retrieves a message by its ID
// Returns the message, the parent ID (empty string if no parent), and an error
func (s *DialogStorage) GetMessage(ctx context.Context, messageID string) (gai.Message, string, error) {
	msg, err := s.q.GetMessage(ctx, messageID)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("failed to get message: %w", err)
	}

	role, err := stringToRole(msg.Role)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("invalid role in database: %w", err)
	}

	blocks, err := s.q.GetBlocksByMessage(ctx, messageID)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("failed to get blocks: %w", err)
	}

	var gaiBlocks []gai.Block
	for _, block := range blocks {
		var blockID string
		if block.ID.Valid {
			blockID = block.ID.String
		}

		gaiBlocks = append(gaiBlocks, gai.Block{
			ID:           blockID,
			BlockType:    block.BlockType,
			ModalityType: gai.Modality(block.ModalityType),
			MimeType:     block.MimeType,
			Content:      gai.Str(block.Content),
		})
	}

	// Extract parent ID
	parentID := ""
	if msg.ParentID.Valid {
		parentID = msg.ParentID.String
	}

	return gai.Message{
		Role:            role,
		Blocks:          gaiBlocks,
		ToolResultError: msg.ToolResultError,
	}, parentID, nil
}

// GetMostRecentUserMessageId retrieves the most recently created user message
func (s *DialogStorage) GetMostRecentUserMessageId(ctx context.Context) (string, error) {
	msg, err := s.q.GetMostRecentUserMessage(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get most recent user message: %w", err)
	}

	return msg.ID, nil
}

// GetDialogForMessage reconstructs a dialog starting given a message and traversing up to the root.
// Based on the message, we traverse up and down a conversation list of messages to fetch a complete dialog.
func (s *DialogStorage) GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error) {

	var dialog gai.Dialog
	var msgIds []string

	// get user message first
	msg, parentId, err := s.GetMessage(ctx, messageID)
	if err != nil {
		return gai.Dialog{}, msgIds, fmt.Errorf("failed to get dialog: %w", err)
	}
	dialog = append(dialog, msg)
	msgIds = append(msgIds, messageID)

	// Then keep querying parent messages until we reach root message
	for parentId != "" {
		var newParentID string
		msg, newParentID, err = s.GetMessage(ctx, parentId)
		if err != nil {
			return gai.Dialog{}, msgIds, fmt.Errorf("failed to get dialog: %w", err)
		}
		dialog = append(dialog, msg)
		msgIds = append(msgIds, parentId)
		parentId = newParentID
	}

	// Reverse the order so now the message is last
	slices.Reverse(dialog)
	slices.Reverse(msgIds)

	// Then get any leftover children messages
	children, err := s.q.GetMessageChildrenId(ctx, sql.NullString{
		String: messageID,
		Valid:  true,
	})
	if err != nil {
		return gai.Dialog{}, msgIds, err
	}

	// If there are no children, this was the last message, so we can return the dialog as is.
	// If there are more than one child, then this was the last message in an assistant turn,
	// so we can return the dialog as is
	if len(children) == 0 || len(children) > 1 {
		return dialog, msgIds, nil
	}

	childMsg, err := s.q.GetMessage(ctx, children[0])
	if err != nil {
		return gai.Dialog{}, msgIds, err
	}

	// If the single child is a user message, we should stop and return the dialog up until the current point
	if childMsg.Role == "user" {
		return dialog, msgIds, nil
	}

	msg, _, err = s.GetMessage(ctx, childMsg.ID)
	if err != nil {
		return gai.Dialog{}, msgIds, err
	}

	dialog = append(dialog, msg)
	msgIds = append(msgIds, childMsg.ID)

	// Now, we keep getting the children of each message to get the chain of assistant messages
	// that belong to this user-ai turn of conversation. We know we have reached the end of the turn when
	// 1. There are no more children for an assistant message
	// 2. The children are user messages (branching occurs after the end of the last assistant message)
	//
	// Example:
	// ┌ User Message: Hi! How many files do I have?
	// ├ Assistant Message: Sure let me help with that
	// ├ Assistant Message: Tool call -> file overview
	// ├ Assistant Message: You have 3 files
	// ├──────────(BRANCHING)─────────┐
	// ├ User Message: Delete a file  ├ User Message: add a file

	assistantMsgId := childMsg.ID
	for {
		children, err = s.q.GetMessageChildrenId(ctx, sql.NullString{
			String: assistantMsgId,
			Valid:  true,
		})
		if err != nil {
			return gai.Dialog{}, msgIds, err
		}

		if len(children) == 0 || len(children) > 1 {
			break
		}

		assistantMsgId = children[0]

		msg, _, err = s.GetMessage(ctx, assistantMsgId)
		if err != nil {
			return gai.Dialog{}, msgIds, err
		}

		dialog = append(dialog, msg)
		msgIds = append(msgIds, assistantMsgId)
	}

	return dialog, msgIds, nil
}

// ListMessages returns all message IDs, sorted by created_at timestamp (newest first)
func (s *DialogStorage) ListMessages(ctx context.Context) ([]string, error) {
	return s.q.ListMessages(ctx)
}

// DeleteMessage deletes a message if it has no children
func (s *DialogStorage) DeleteMessage(ctx context.Context, messageID string) error {
	// Check if the message has children
	hasChildren, err := s.q.HasChildren(ctx, sql.NullString{String: messageID, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to check for children: %w", err)
	}

	if hasChildren > 0 {
		return fmt.Errorf("cannot delete message with ID %s: message has children", messageID)
	}

	// Delete the message (this will also delete associated blocks due to CASCADE)
	return s.q.DeleteMessage(ctx, messageID)
}

// DeleteMessageRecursive deletes a message and all of its children recursively
func (s *DialogStorage) DeleteMessageRecursive(ctx context.Context, messageID string) error {
	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	// Delete recursively using existing queries
	if err := s.deleteMessageAndDescendants(ctx, qtx, messageID); err != nil {
		return err
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// deleteMessageAndDescendants recursively deletes a message and all its descendants
func (s *DialogStorage) deleteMessageAndDescendants(ctx context.Context, qtx *Queries, messageID string) error {
	// Get all children of this message
	children, err := qtx.ListMessagesByParent(ctx, sql.NullString{String: messageID, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to list child messages: %w", err)
	}

	// Recursively delete all children first
	for _, child := range children {
		if err := s.deleteMessageAndDescendants(ctx, qtx, child.ID); err != nil {
			return err
		}
	}

	// Finally delete this message (which will cascade delete its blocks)
	if err := qtx.DeleteMessage(ctx, messageID); err != nil {
		return fmt.Errorf("failed to delete message %s: %w", messageID, err)
	}

	return nil
}

// generateUniqueID generates a unique ID and checks for collisions in the provided check function
func (s *DialogStorage) generateUniqueID(ctx context.Context, collisionCheck func(context.Context, string) (bool, error)) (string, error) {
	maxAttempts := 10
	for i := 0; i < maxAttempts; i++ {
		id := s.idGenerator()

		// Check for collision
		exists, err := collisionCheck(ctx, id)
		if err != nil {
			return "", fmt.Errorf("failed to check ID collision: %w", err)
		}

		if !exists {
			return id, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique ID after %d attempts", maxAttempts)
}

// checkMessageIDExists checks if a message ID already exists
func (s *DialogStorage) checkMessageIDExists(ctx context.Context, id string) (bool, error) {
	// First check if the messages table exists
	var tableExists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT name FROM sqlite_master WHERE name = 'messages')").Scan(&tableExists)
	if err != nil {
		return false, err
	}

	// If the table doesn't exist yet, then the ID definitely doesn't exist
	if !tableExists {
		return false, nil
	}

	// Otherwise, check if the ID exists in the table
	_, err = s.q.GetMessage(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
