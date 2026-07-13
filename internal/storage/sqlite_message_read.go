package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage/sqlcgen"
)

// getMessage loads one message and all of its blocks, then reconstructs a
// gai.Message with storage metadata in ExtraFields.
//
// It returns the reconstructed message, the parent ID ("" for roots), and an
// error. JSON-compatible message-level ExtraFields are restored from storage,
// with storage-owned and typed runtime metadata overlaid from dedicated columns.
func (s *Sqlite) getMessage(ctx context.Context, messageID string) (gai.Message, string, error) {
	msg, err := s.q.GetMessage(ctx, messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return gai.Message{}, "", fmt.Errorf("message %s not found: %w", messageID, ErrMessageNotFound)
		}
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

	// Extract parent IDs
	parentID := ""
	if msg.ParentID.Valid {
		parentID = msg.ParentID.String
	}
	compactionParentID := ""
	if msg.CompactionParentID.Valid {
		compactionParentID = msg.CompactionParentID.String
	}

	// Set storage-owned and typed runtime metadata in ExtraFields so consumers can
	// access a complete message snapshot without knowing the physical schema.
	msgExtraFields, err := decodeMessageExtraFields(msg.MessageExtraFields)
	if err != nil {
		return gai.Message{}, "", err
	}
	msgExtraFields[MessageIDKey] = messageID
	msgExtraFields[MessageCreatedAtKey] = msg.CreatedAt
	putNullString(msgExtraFields, AgentMetadataModelRefKey, msg.ModelRef)
	putNullString(msgExtraFields, AgentMetadataModelIDKey, msg.ModelID)
	putNullString(msgExtraFields, AgentMetadataModelTypeKey, msg.ModelType)
	putNullString(msgExtraFields, AgentMetadataModelDisplayNameKey, msg.ModelDisplayName)
	putNullInt64(msgExtraFields, AgentMetadataInputTokensKey, msg.InputTokens)
	putNullInt64(msgExtraFields, AgentMetadataOutputTokensKey, msg.OutputTokens)
	putNullInt64(msgExtraFields, AgentMetadataCacheReadTokensKey, msg.CacheReadTokens)
	putNullInt64(msgExtraFields, AgentMetadataCacheWriteTokensKey, msg.CacheWriteTokens)
	if parentID != "" {
		msgExtraFields[MessageParentIDKey] = parentID
	}
	if compactionParentID != "" {
		msgExtraFields[MessageCompactionParentIDKey] = compactionParentID
	}

	return gai.Message{
		Role:            role,
		Blocks:          gaiBlocks,
		ToolResultError: msg.ToolResultError,
		ExtraFields:     msgExtraFields,
	}, parentID, nil
}

// GetMessages returns fully populated messages for the requested IDs.
//
// The method fails if any ID cannot be loaded. Returned messages include all
// blocks plus storage metadata keys in ExtraFields.
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

// ListMessages returns a materialized snapshot of messages ordered by
// created_at with optional ascending order and offset.
//
// Each yielded message includes all blocks and storage metadata keys in
// ExtraFields.
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
