package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/spachava753/cpe/internal/storage/sqlcgen"
)

// DeleteMessages deletes the requested messages in a single transaction.
//
// With opts.Recursive=true, each message's descendant subtree is removed.
// With opts.Recursive=false, deletion fails if any target has children.
//
// The operation is atomic across all IDs in opts.MessageIDs.
func (s *Sqlite) DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error {
	// Begin transaction
	tx, err := beginWriteTx(ctx, s.db)
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

// deleteMessageAndDescendantsInTx performs depth-first subtree deletion inside
// the caller's transaction.
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
