package storage

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/matoous/go-nanoid/v2"
	"github.com/spachava753/gai"
)

// DialogStorage provides operations for storing and retrieving gai.Dialog objects
type DialogStorage struct {
	db          *sql.DB
	q           *Queries
	idGenerator func() string
}

// NewDialogStorage creates a new DialogStorage instance
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
		// Generate a unique block ID
		blockID, err := s.generateUniqueID(ctx, s.checkBlockIDExists)
		if err != nil {
			return "", fmt.Errorf("failed to generate block ID: %w", err)
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
func (s *DialogStorage) GetMessage(ctx context.Context, messageID string) (gai.Message, error) {
	msg, err := s.q.GetMessage(ctx, messageID)
	if err != nil {
		return gai.Message{}, fmt.Errorf("failed to get message: %w", err)
	}

	role, err := stringToRole(msg.Role)
	if err != nil {
		return gai.Message{}, fmt.Errorf("invalid role in database: %w", err)
	}

	blocks, err := s.q.GetBlocksByMessage(ctx, messageID)
	if err != nil {
		return gai.Message{}, fmt.Errorf("failed to get blocks: %w", err)
	}

	var gaiBlocks []gai.Block
	for _, block := range blocks {
		gaiBlocks = append(gaiBlocks, gai.Block{
			ID:           block.ID,
			BlockType:    block.BlockType,
			ModalityType: gai.Modality(block.ModalityType),
			MimeType:     block.MimeType,
			Content:      gai.Str(block.Content),
		})
	}

	return gai.Message{
		Role:            role,
		Blocks:          gaiBlocks,
		ToolResultError: msg.ToolResultError,
	}, nil
}

// GetMostRecentMessage retrieves the most recently created message
func (s *DialogStorage) GetMostRecentMessage(ctx context.Context) (gai.Message, string, error) {
	msg, err := s.q.GetMostRecentMessage(ctx)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("failed to get most recent message: %w", err)
	}

	message, err := s.GetMessage(ctx, msg.ID)
	if err != nil {
		return gai.Message{}, "", err
	}

	return message, msg.ID, nil
}

// GetDialogFromLeaf reconstructs a dialog starting from a leaf message and traversing up to the root
func (s *DialogStorage) GetDialogFromLeaf(ctx context.Context, leafMessageID string) (gai.Dialog, error) {
	// Get the dialog path from leaf to root
	rows, err := s.q.GetDialogPath(ctx, leafMessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dialog path: %w", err)
	}

	// Process the results to reconstruct the dialog
	messagesMap := make(map[string]*gai.Message)
	var orderedMessageIDs []string
	var dialog gai.Dialog

	for _, row := range rows {
		// Process the message if we haven't seen it before
		if _, exists := messagesMap[row.ID]; !exists {
			role, err := stringToRole(row.Role)
			if err != nil {
				return nil, fmt.Errorf("invalid role in database: %s", row.Role)
			}

			message := &gai.Message{
				Role:            role,
				Blocks:          []gai.Block{},
				ToolResultError: row.ToolResultError,
			}

			messagesMap[row.ID] = message
			orderedMessageIDs = append([]string{row.ID}, orderedMessageIDs...)
		}

		// Skip if there's no block for this row
		if !row.BlockID.Valid {
			continue
		}

		// Process the block
		block := gai.Block{
			ID:           row.BlockID.String,
			BlockType:    row.BlockType.String,
			ModalityType: gai.Modality(row.ModalityType.Int64),
			MimeType:     row.MimeType.String,
			Content:      gai.Str(row.Content.String),
		}

		// Add the block to its message
		messagesMap[row.ID].Blocks = append(messagesMap[row.ID].Blocks, block)
	}

	// Reconstruct the dialog in the correct order
	for _, id := range orderedMessageIDs {
		dialog = append(dialog, *messagesMap[id])
	}

	return dialog, nil
}

// ListMessages returns all messages, sorted by created_at timestamp (newest first)
func (s *DialogStorage) ListMessages(ctx context.Context) ([]Message, error) {
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

// checkBlockIDExists checks if a block ID already exists
func (s *DialogStorage) checkBlockIDExists(ctx context.Context, id string) (bool, error) {
	// First check if the blocks table exists
	var tableExists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT name FROM sqlite_master WHERE name = 'blocks')").Scan(&tableExists)
	if err != nil {
		return false, err
	}

	// If the table doesn't exist yet, then the ID definitely doesn't exist
	if !tableExists {
		return false, nil
	}

	// Otherwise, check if the ID exists in the table
	_, err = s.q.GetBlock(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
