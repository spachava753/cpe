package conversation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/spachava753/cpe/internal/db"
	"github.com/oklog/ulid"
)

// Manager handles conversation persistence and retrieval
type Manager struct {
	queries *db.Queries
	db      *sql.DB
}

// NewManager creates a new conversation manager
func NewManager(dbPath string) (*Manager, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	queries := db.New(sqlDB)
	return &Manager{
		queries: queries,
		db:      sqlDB,
	}, nil
}

// Close closes the database connection
func (m *Manager) Close() error {
	return m.db.Close()
}

// CreateConversation creates a new conversation
func (m *Manager) CreateConversation(ctx context.Context, parentID *string, userMessage string, executorData []byte, model string) (string, error) {
	// Generate ULID
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()

	// Create conversation params
	var parentIDSQL sql.NullString
	if parentID != nil {
		parentIDSQL = sql.NullString{
			String: *parentID,
			Valid:  true,
		}
	}

	err := m.queries.CreateConversation(ctx, db.CreateConversationParams{
		ID:           id,
		ParentID:     parentIDSQL,
		UserMessage:  userMessage,
		ExecutorData: executorData,
		CreatedAt:    time.Now(),
		Model:        model,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create conversation: %w", err)
	}

	return id, nil
}

// GetConversation retrieves a conversation by ID
func (m *Manager) GetConversation(ctx context.Context, id string) (*db.Conversation, error) {
	conv, err := m.queries.GetConversation(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("conversation not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}
	return &conv, nil
}

// GetLatestConversation retrieves the most recent conversation
func (m *Manager) GetLatestConversation(ctx context.Context) (*db.Conversation, error) {
	conv, err := m.queries.GetLatestConversation(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no conversations found")
		}
		return nil, fmt.Errorf("failed to get latest conversation: %w", err)
	}
	return &conv, nil
}

// ListConversations lists all conversations
func (m *Manager) ListConversations(ctx context.Context) ([]db.ListConversationsRow, error) {
	return m.queries.ListConversations(ctx)
}

// DeleteConversation deletes a conversation and optionally its children
func (m *Manager) DeleteConversation(ctx context.Context, id string, cascade bool) error {
	if !cascade {
		// Check for children
		children, err := m.queries.GetChildConversations(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to check for children: %w", err)
		}
		if len(children) > 1 { // More than 1 because the query includes the conversation itself
			return errors.New("cannot delete conversation with children without cascade flag")
		}
	}

	// Start transaction
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := m.queries.WithTx(tx)

	if cascade {
		// Get all children
		children, err := qtx.GetChildConversations(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get children: %w", err)
		}

		// Delete all children
		for _, childID := range children {
			if err := qtx.DeleteConversation(ctx, childID); err != nil {
				return fmt.Errorf("failed to delete child conversation: %w", err)
			}
		}
	} else {
		// Delete single conversation
		if err := qtx.DeleteConversation(ctx, id); err != nil {
			return fmt.Errorf("failed to delete conversation: %w", err)
		}
	}

	return tx.Commit()
}

// DecodeExecutorData decodes the executor data from a conversation
func (m *Manager) DecodeExecutorData(conv *db.Conversation, dest interface{}) error {
	return gob.NewDecoder(bytes.NewReader(conv.ExecutorData)).Decode(dest)
}