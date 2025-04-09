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

// DialogStorage provides operations for storing and retrieving gai.Dialog objects
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

// GetMostRecentUserMessageId retrieves the most recently created user message
func (s *DialogStorage) GetMostRecentUserMessageId(ctx context.Context) (string, error) {
	msg, err := s.q.GetMostRecentUserMessage(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get most recent user message: %w", err)
	}

	return msg.ID, nil
}

// GetDialogForUserMessage reconstructs a dialog starting from a user message and traversing up to the root
func (s *DialogStorage) GetDialogForUserMessage(ctx context.Context, userMessageID string) (gai.Dialog, error) {

	var dialog gai.Dialog

	// get user message first
	dbMsg, err := s.q.GetMessage(ctx, userMessageID)
	if err != nil {
		return gai.Dialog{}, fmt.Errorf("failed to get dialog: %w", err)
	}

	if dbMsg.Role != "user" {
		return gai.Dialog{}, fmt.Errorf("invalid role: %s", dbMsg.Role)
	}

	userMsg, err := s.GetMessage(ctx, dbMsg.ID)
	if err != nil {
		return gai.Dialog{}, fmt.Errorf("failed to get dialog: %w", err)
	}
	dialog = append(dialog, userMsg)

	// Then keep querying parent messages until we reach root message
	parentId := dbMsg.ParentID.String
	var msg gai.Message
	for parentId != "" {
		msg, err = s.GetMessage(ctx, parentId)
		if err != nil {
			return gai.Dialog{}, fmt.Errorf("failed to get dialog: %w", err)
		}
		dialog = append(dialog, msg)
		dbMsg, err = s.q.GetMessage(ctx, parentId)
		if err != nil {
			return gai.Dialog{}, fmt.Errorf("failed to get dialog: %w", err)
		}
		parentId = dbMsg.ParentID.String
	}

	// Reverse the order so now the user message is last
	slices.Reverse(dialog)

	// Then get associated assistant message for the user message
	children, err := s.q.GetMessageChildrenId(ctx, sql.NullString{
		String: userMessageID,
		Valid:  true,
	})
	if err != nil {
		return gai.Dialog{}, err
	}
	if len(children) != 1 {
		return gai.Dialog{}, fmt.Errorf("expected 1 child, got %d", len(children))
	}

	assistantMsg, err := s.q.GetMessage(ctx, children[0])
	if err != nil {
		return gai.Dialog{}, err
	}

	msg, err = s.GetMessage(ctx, assistantMsg.ID)
	if err != nil {
		return gai.Dialog{}, err
	}

	dialog = append(dialog, msg)

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

	assistantMsgId := assistantMsg.ID
	for {
		children, err = s.q.GetMessageChildrenId(ctx, sql.NullString{
			String: assistantMsgId,
			Valid:  true,
		})
		if err != nil {
			return gai.Dialog{}, err
		}

		if len(children) == 0 || len(children) > 1 {
			break
		}

		assistantMsgId = children[0]

		msg, err = s.GetMessage(ctx, assistantMsgId)
		if err != nil {
			return gai.Dialog{}, err
		}

		dialog = append(dialog, msg)
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
