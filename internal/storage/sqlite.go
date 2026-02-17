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

	"github.com/spachava753/cpe/internal/storage/sqlcgen"
)

// DB is the interface accepted by NewSqlite. It abstracts the database
// operations needed by Sqlite so that callers can supply a real *sql.DB or a
// wrapper that injects faults, records calls, etc.
type DB interface {
	sqlcgen.DBTX
	// ExecContext executes a query without returning any rows. It is used to
	// initialise the database schema.
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	// BeginTx starts a transaction.
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

const idCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func generateId() string {
	return gonanoid.MustGenerate(idCharset, 6)
}

//go:embed schema.sql
var schemaSQL string

// Sqlite provides operations for storing and retrieving gai.Dialog objects.
// It serves as an abstraction over the implementation details of how messages
// are actually stored. Do not access its internal database directly.
type Sqlite struct {
	db          DB
	q           *sqlcgen.Queries
	idGenerator func() string
}

// NewSqlite initializes and returns a new Sqlite instance backed by the given
// DB. It runs the embedded schema DDL against db before returning. The caller
// is responsible for opening and closing the underlying database connection.
func NewSqlite(ctx context.Context, db DB) (*Sqlite, error) {
	// Initialize schema from embedded SQL file
	_, err := db.ExecContext(ctx, schemaSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Create and return the dialog storage
	return &Sqlite{
		db:          db,
		q:           sqlcgen.New(db),
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

// saveMessageInTx saves a single message and its blocks within a transaction.
func (s *Sqlite) saveMessageInTx(ctx context.Context, qtx *sqlcgen.Queries, message gai.Message, parentID string, title string) (string, error) {
	// Generate a unique message ID
	messageID, err := s.generateUniqueIDInTx(ctx, qtx)
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
	err = qtx.CreateMessage(ctx, sqlcgen.CreateMessageParams{
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

		err = qtx.CreateBlock(ctx, sqlcgen.CreateBlockParams{
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

// getExtraFieldString safely extracts a string value from an ExtraFields map.
func getExtraFieldString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// generateUniqueIDInTx generates a unique ID checking for collisions within a transaction.
func (s *Sqlite) generateUniqueIDInTx(ctx context.Context, qtx *sqlcgen.Queries) (string, error) {
	maxAttempts := 10
	for range maxAttempts {
		id := s.idGenerator()

		exists, err := qtx.CheckMessageIDExists(ctx, id)
		if err != nil {
			return "", fmt.Errorf("failed to check ID collision: %w", err)
		}

		if exists == 0 {
			return id, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique ID after %d attempts", maxAttempts)
}

// SaveDialog saves a dialog — a sequence of related messages that form a
// single conversation thread. The input iterator yields messages in order
// from root to leaf. The entire operation is performed in a single
// transaction; if saving any message fails, all changes are rolled back.
//
// See the DialogSaver interface documentation for full semantics.
func (s *Sqlite) SaveDialog(ctx context.Context, msgs iter.Seq[gai.Message]) iter.Seq2[gai.Message, error] {
	return func(yield func(gai.Message, error) bool) {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			yield(gai.Message{}, fmt.Errorf("failed to begin transaction: %w", err))
			return
		}
		committed := false
		defer func() {
			if !committed {
				tx.Rollback()
			}
		}()

		qtx := s.q.WithTx(tx)

		var prevID string
		first := true
		consumerStopped := false

		for msg := range msgs {
			existingID := getExtraFieldString(msg.ExtraFields, MessageIDKey)

			if existingID != "" {
				// Message claims to be already persisted — verify it exists
				// and has the correct parent.
				dbMsg, dbErr := qtx.GetMessage(ctx, existingID)
				if dbErr != nil {
					yield(gai.Message{}, fmt.Errorf("failed to verify message %s exists: %w", existingID, dbErr))
					return
				}

				// Verify parent chain
				dbParent := ""
				if dbMsg.ParentID.Valid {
					dbParent = dbMsg.ParentID.String
				}
				if first {
					// First message must be a root (no parent in storage)
					if dbParent != "" {
						yield(gai.Message{}, fmt.Errorf("first message %s must be a root message but has parent %q in storage", existingID, dbParent))
						return
					}
				} else {
					if dbParent != prevID {
						yield(gai.Message{}, fmt.Errorf("message %s has parent %q in storage but expected %q", existingID, dbParent, prevID))
						return
					}
				}

				prevID = existingID
				first = false

				// Yield the message as-is (it's already persisted)
				if !yield(msg, nil) {
					consumerStopped = true
					break
				}
				continue
			}

			// Message needs to be saved
			title := getExtraFieldString(msg.ExtraFields, MessageTitleKey)
			parentID := prevID

			savedID, saveErr := s.saveMessageInTx(ctx, qtx, msg, parentID, title)
			if saveErr != nil {
				yield(gai.Message{}, fmt.Errorf("failed to save message: %w", saveErr))
				return
			}

			// Set ExtraFields on the message
			if msg.ExtraFields == nil {
				msg.ExtraFields = make(map[string]any)
			}
			msg.ExtraFields[MessageIDKey] = savedID
			if parentID != "" {
				msg.ExtraFields[MessageParentIDKey] = parentID
			}

			prevID = savedID
			first = false

			if !yield(msg, nil) {
				consumerStopped = true
				break
			}
		}

		if err := tx.Commit(); err != nil {
			if !consumerStopped {
				// Consumer is still active; propagate the error.
				yield(gai.Message{}, fmt.Errorf("failed to commit transaction: %w", err))
			}
			// If consumer stopped (break/return), we cannot call yield again.
			// The deferred Rollback will clean up.
			return
		}
		committed = true
	}
}

// getMessage retrieves a message by its ID
// Returns the message, the parent ID (empty string if no parent), and an error
func (s *Sqlite) getMessage(ctx context.Context, messageID string) (gai.Message, string, error) {
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
	// Note: Arbitrary message-level ExtraFields are not persisted to the database
	// (only block-level ExtraFields are). We create a fresh map here and populate it
	// with the known keys that are stored as dedicated columns.
	msgExtraFields := map[string]any{
		MessageIDKey:        messageID,
		MessageCreatedAtKey: msg.CreatedAt,
	}
	if parentID != "" {
		msgExtraFields[MessageParentIDKey] = parentID
	}
	if msg.Title.Valid {
		msgExtraFields[MessageTitleKey] = msg.Title.String
	}

	return gai.Message{
		Role:            role,
		Blocks:          gaiBlocks,
		ToolResultError: msg.ToolResultError,
		ExtraFields:     msgExtraFields,
	}, parentID, nil
}

// GetMessages retrieves messages by their IDs.
func (s *Sqlite) GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
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
func (s *Sqlite) ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error) {
	var rows []sqlcgen.Message
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
func (s *Sqlite) DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error {
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
func (s *Sqlite) deleteMessageAndDescendantsInTx(ctx context.Context, qtx *sqlcgen.Queries, messageID string) error {
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
