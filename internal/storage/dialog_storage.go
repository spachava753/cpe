package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"iter"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/spachava753/gai"
)

const idCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func generateId() string {
	return gonanoid.MustGenerate(idCharset, 6)
}

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
func InitDialogStorage(ctx context.Context, dbPath string) (*DialogStorage, error) {
	// Open or create the database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize schema from embedded SQL file
	_, err = db.ExecContext(ctx, schemaSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Create and return the dialog storage
	return &DialogStorage{
		db:          db,
		q:           New(db),
		idGenerator: generateId,
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

// saveMessageInTx saves a single message and its blocks within an existing transaction
func (s *DialogStorage) saveMessageInTx(ctx context.Context, qtx *Queries, message gai.Message, parentID string, title string) (string, error) {
	// Generate a unique message ID
	messageID, err := s.generateUniqueID(ctx, s.checkMessageIDExists)
	if err != nil {
		return "", fmt.Errorf("failed to generate message ID: %w", err)
	}

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
			blockID = sql.NullString{
				String: block.ID,
				Valid:  true,
			}
		}

		// Encode ExtraFields as JSON if it exists
		var extraFieldsParam sql.NullString
		if block.ExtraFields != nil {
			extraFieldsJSON, err := json.Marshal(block.ExtraFields)
			if err != nil {
				return "", fmt.Errorf("failed to marshal ExtraFields to JSON: %w", err)
			}
			extraFieldsParam = sql.NullString{
				String: string(extraFieldsJSON),
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
			ExtraFields:   extraFieldsParam,
			SequenceOrder: int64(j),
		})
		if err != nil {
			return "", fmt.Errorf("failed to create block: %w", err)
		}
	}

	return messageID, nil
}

// SaveMessages saves one or more messages and returns their generated IDs.
// All messages in a single call are saved atomically.
func (s *DialogStorage) SaveMessages(ctx context.Context, opts []SaveMessageOptions) (iter.Seq[string], error) {
	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	ids := make([]string, 0, len(opts))
	for _, opt := range opts {
		id, err := s.saveMessageInTx(ctx, qtx, opt.Message, opt.ParentID, opt.Title)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return func(yield func(string) bool) {
		for _, id := range ids {
			if !yield(id) {
				return
			}
		}
	}, nil
}

// getMessage retrieves a message by its ID
// Returns the message, the parent ID (empty string if no parent), and an error
func (s *DialogStorage) getMessage(ctx context.Context, messageID string) (gai.Message, string, error) {
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

		var extraFields map[string]any
		if block.ExtraFields.Valid && block.ExtraFields.String != "" {
			if err := json.Unmarshal([]byte(block.ExtraFields.String), &extraFields); err != nil {
				return gai.Message{}, "", fmt.Errorf("failed to unmarshal ExtraFields: %w", err)
			}
		}

		gaiBlocks = append(gaiBlocks, gai.Block{
			ID:           blockID,
			BlockType:    block.BlockType,
			ModalityType: gai.Modality(block.ModalityType),
			MimeType:     block.MimeType,
			Content:      gai.Str(block.Content),
			ExtraFields:  extraFields,
		})
	}

	// Extract parent ID
	parentID := ""
	if msg.ParentID.Valid {
		parentID = msg.ParentID.String
	}

	// Set the message ID and other metadata in ExtraFields so consumers can access them.
	// Note: Message-level ExtraFields are not persisted to the database (only block-level ExtraFields are),
	// so we create a fresh map here. This is intentional - message ExtraFields are runtime metadata only.
	msgExtraFields := map[string]any{
		MessageIDKey:        messageID,
		MessageCreatedAtKey: msg.CreatedAt,
	}
	if parentID != "" {
		msgExtraFields[MessageParentIDKey] = parentID
	}

	return gai.Message{
		Role:            role,
		Blocks:          gaiBlocks,
		ToolResultError: msg.ToolResultError,
		ExtraFields:     msgExtraFields,
	}, parentID, nil
}

// GetMessages retrieves messages by their IDs.
func (s *DialogStorage) GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
	messages := make([]gai.Message, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg, _, err := s.getMessage(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get message %s: %w", id, err)
		}
		messages = append(messages, msg)
	}

	return func(yield func(gai.Message) bool) {
		for _, msg := range messages {
			if !yield(msg) {
				return
			}
		}
	}, nil
}

// ListMessages returns messages ordered by creation timestamp.
func (s *DialogStorage) ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error) {
	var rows []Message
	var err error
	if opts.AscendingOrder {
		rows, err = s.q.ListMessagesAscending(ctx, int64(opts.Offset))
	} else {
		rows, err = s.q.ListMessagesDescending(ctx, int64(opts.Offset))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	// For each row, load blocks and build gai.Message
	messages := make([]gai.Message, 0, len(rows))
	for _, row := range rows {
		msg, _, err := s.getMessage(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get message %s: %w", row.ID, err)
		}
		messages = append(messages, msg)
	}

	return func(yield func(gai.Message) bool) {
		for _, msg := range messages {
			if !yield(msg) {
				return
			}
		}
	}, nil
}

// DeleteMessages deletes the specified messages.
func (s *DialogStorage) DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error {
	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	for _, id := range opts.MessageIDs {
		if opts.Recursive {
			if err := s.deleteMessageAndDescendantsInTx(ctx, qtx, id); err != nil {
				return err
			}
		} else {
			// Check if the message has children
			hasChildren, err := qtx.HasChildren(ctx, sql.NullString{String: id, Valid: true})
			if err != nil {
				return fmt.Errorf("failed to check for children: %w", err)
			}
			if hasChildren > 0 {
				return fmt.Errorf("cannot delete message with ID %s: message has children", id)
			}
			if err := qtx.DeleteMessage(ctx, id); err != nil {
				return fmt.Errorf("failed to delete message %s: %w", id, err)
			}
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// deleteMessageAndDescendantsInTx recursively deletes a message and all its descendants within a transaction
func (s *DialogStorage) deleteMessageAndDescendantsInTx(ctx context.Context, qtx *Queries, messageID string) error {
	// Get all children of this message
	children, err := qtx.ListMessagesByParent(ctx, sql.NullString{String: messageID, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to list child messages: %w", err)
	}

	// Recursively delete all children first
	for _, child := range children {
		if err := s.deleteMessageAndDescendantsInTx(ctx, qtx, child.ID); err != nil {
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
	for range maxAttempts {
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
	exists, err := s.q.CheckMessageIDExists(ctx, id)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}
