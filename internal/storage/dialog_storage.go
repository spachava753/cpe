package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/matoous/go-nanoid/v2"
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
	genIds      []string // Used only for testing to track generated IDs
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
		idGenerator: generateId,
	}, nil
}

// NewDialogStorage creates a new DialogStorage instance with an existing database connection
func NewDialogStorage(db *sql.DB) (*DialogStorage, error) {
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

		var extraFields map[string]interface{}
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

// GetDialogForMessage reconstructs a dialog up to and including the given message by traversing up to the root.
// It returns the dialog path from root to the specified message, without including any children of that message.
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

	return dialog, msgIds, nil
}

// MessageIdNode represents a message and its relationship with its parent and children
// CreatedAt is the creation timestamp of the message.
type MessageIdNode struct {
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id"`
	CreatedAt time.Time       `json:"created_at"`
	Content   string          `json:"content"` // Short snippet or modality type
	Role      string          `json:"role"`    // user, assistant, or tool_result
	Children  []MessageIdNode `json:"children"`
}

// ListMessages returns a hierarchical representation of messages as a forest of trees.
// Each tree represents a conversation thread, starting with a root message (a message with no parent).
// The returned slice contains the root messages, with their children accessible via the Children field.
func (s *DialogStorage) ListMessages(ctx context.Context) ([]MessageIdNode, error) {
	// First, get all root messages (messages with no parent)
	rootMessageIDs, err := s.q.ListRootMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list root messages: %w", err)
	}

	// Create a map to store all messages for quick lookup
	allMessages := make(map[string]Message)

	// Get all messages
	allMessageIDs, err := s.q.ListMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all messages: %w", err)
	}

	// Fetch all messages and store them in the map
	for _, id := range allMessageIDs {
		msg, err := s.q.GetMessage(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get message %s: %w", id, err)
		}
		allMessages[id] = msg
	}

	// Construct parent-child relationships
	childrenMap := make(map[string][]string)
	for id, msg := range allMessages {
		if msg.ParentID.Valid {
			parentID := msg.ParentID.String
			childrenMap[parentID] = append(childrenMap[parentID], id)
		}
	}

	// Build the forest starting with root nodes
	var forest []MessageIdNode
	for _, rootID := range rootMessageIDs {
		node, err := s.buildMessageTree(ctx, rootID, childrenMap)
		if err != nil {
			return nil, err
		}
		forest = append(forest, node)
	}

	return forest, nil
}

// buildMessageTree recursively builds a message tree starting from the given node ID
func (s *DialogStorage) buildMessageTree(ctx context.Context, nodeID string, childrenMap map[string][]string) (MessageIdNode, error) {
	// Get the message from the database to get its parent ID and created_at/role
	msg, err := s.q.GetMessage(ctx, nodeID)
	if err != nil {
		return MessageIdNode{}, fmt.Errorf("failed to get message %s: %w", nodeID, err)
	}

	// Retrieve all blocks for Content extraction
	blocks, err := s.q.GetBlocksByMessage(ctx, nodeID)
	if err != nil {
		return MessageIdNode{}, fmt.Errorf("failed to get blocks for message %s: %w", nodeID, err)
	}

	escapeNewlines := func(s string) string {
		// Replace both literal \n and actual newlines
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\r", " ")
		return s
	}

	content := ""
	modalityTypeName := func(mType int64) string {
		switch mType {
		case 0:
			return "Text"
		case 1:
			return "Image"
		case 2:
			return "Audio"
		case 3:
			return "Video"
		default:
			return fmt.Sprintf("Unknown(%d)", mType)
		}
	}

	foundText := false
	for _, blk := range blocks {
		if blk.ModalityType == 0 { // gai.Text
			// Truncate to first 50 chars, replacing newlines
			snippet := blk.Content
			snippet = escapeNewlines(snippet)
			if len(snippet) > 50 {
				snippet = snippet[:50]
			}
			content = snippet
			foundText = true
			break
		}
	}
	if !foundText && len(blocks) > 0 {
		content = modalityTypeName(blocks[0].ModalityType)
	}

	role := msg.Role

	node := MessageIdNode{
		ID:        nodeID,
		ParentID:  "",
		CreatedAt: msg.CreatedAt,
		Content:   content,
		Role:      role,
	}
	if msg.ParentID.Valid {
		node.ParentID = msg.ParentID.String
	}

	children, exists := childrenMap[nodeID]
	if exists {
		for _, childID := range children {
			childNode, err := s.buildMessageTree(ctx, childID, childrenMap)
			if err != nil {
				return MessageIdNode{}, err
			}
			node.Children = append(node.Children, childNode)
		}
	}
	return node, nil
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
