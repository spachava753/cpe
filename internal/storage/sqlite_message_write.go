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

// saveMessageInTx saves a single message and its blocks within a transaction.
func (s *Sqlite) saveMessageInTx(ctx context.Context, qtx *sqlcgen.Queries, message gai.Message, parentID, compactionParentID string) (string, error) {
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

	// Prepare compaction parent ID parameter.
	var compactionParentIDParam sql.NullString
	if compactionParentID != "" {
		compactionParentIDParam = sql.NullString{String: compactionParentID, Valid: true}
	}

	messageExtraFields, err := encodeMessageExtraFields(message.ExtraFields)
	if err != nil {
		return "", err
	}
	modelRef, err := messageMetadataString(message.ExtraFields, AgentMetadataModelRefKey)
	if err != nil {
		return "", err
	}
	modelID, err := messageMetadataString(message.ExtraFields, AgentMetadataModelIDKey)
	if err != nil {
		return "", err
	}
	modelType, err := messageMetadataString(message.ExtraFields, AgentMetadataModelTypeKey)
	if err != nil {
		return "", err
	}
	modelDisplayName, err := messageMetadataString(message.ExtraFields, AgentMetadataModelDisplayNameKey)
	if err != nil {
		return "", err
	}
	inputTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataInputTokensKey)
	if err != nil {
		return "", err
	}
	outputTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataOutputTokensKey)
	if err != nil {
		return "", err
	}
	cacheReadTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataCacheReadTokensKey)
	if err != nil {
		return "", err
	}
	cacheWriteTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataCacheWriteTokensKey)
	if err != nil {
		return "", err
	}

	// Create the message
	err = qtx.CreateMessage(ctx, sqlcgen.CreateMessageParams{
		ID:                 messageID,
		ParentID:           parentIDParam,
		CompactionParentID: compactionParentIDParam,
		Role:               roleToString(message.Role),
		ToolResultError:    message.ToolResultError,
		MessageExtraFields: messageExtraFields,
		ModelRef:           modelRef,
		ModelID:            modelID,
		ModelType:          modelType,
		ModelDisplayName:   modelDisplayName,
		InputTokens:        inputTokens,
		OutputTokens:       outputTokens,
		CacheReadTokens:    cacheReadTokens,
		CacheWriteTokens:   cacheWriteTokens,
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

// SaveDialog validates and persists a root-to-leaf chain in one SQL
// transaction.
//
// Existing messages (MessageIDKey already present) are verified against stored
// parent links. New messages are inserted with parent IDs pointing to the
// previously processed message.
//
// Commit/rollback boundary: when iteration starts, a transaction is opened.
// Any validation or insert failure rolls back writes from this call. If the
// consumer stops iteration early, the processed prefix is still committed when
// possible; remaining input is not read.
//
// See DialogSaver for cross-implementation contract details.
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
					if errors.Is(dbErr, sql.ErrNoRows) {
						yield(gai.Message{}, fmt.Errorf("message %s not found: %w", existingID, ErrMessageNotFound))
						return
					}
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
			parentID := prevID
			compactionParentID := ""
			if parentID == "" {
				compactionParentID = getExtraFieldString(msg.ExtraFields, MessageCompactionParentIDKey)
			}

			savedID, saveErr := s.saveMessageInTx(ctx, qtx, msg, parentID, compactionParentID)
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
				delete(msg.ExtraFields, MessageCompactionParentIDKey)
			} else if compactionParentID != "" {
				msg.ExtraFields[MessageCompactionParentIDKey] = compactionParentID
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
