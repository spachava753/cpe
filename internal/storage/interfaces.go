package storage

import (
	"context"
	"fmt"
	"iter"
	"slices"

	"github.com/spachava753/gai"
)

const (
	// MessageIDKey is the gai.Message.ExtraFields key for the storage-assigned
	// message identifier.
	//
	// The value is a string. Read APIs (GetMessages, ListMessages,
	// GetDialogForMessage) populate it on every returned message. SaveDialog
	// treats a message carrying this key as already persisted and validates it
	// instead of inserting a duplicate row.
	MessageIDKey = "cpe_message_id"

	// MessageParentIDKey is the gai.Message.ExtraFields key for a message's
	// parent ID in the conversation tree.
	//
	// The value is a string. Root messages omit this key. Read APIs populate it
	// for non-root messages, and SaveDialog uses parent IDs to validate that an
	// appended chain is contiguous.
	MessageParentIDKey = "cpe_message_parent_id"

	// MessageCreatedAtKey is the gai.Message.ExtraFields key for the message
	// creation timestamp.
	//
	// The value is a time.Time and is populated by read APIs.
	MessageCreatedAtKey = "cpe_message_created_at"

	// MessageIsSubagentKey is the gai.Message.ExtraFields key that marks
	// subagent-originated messages.
	//
	// The value is a bool. SaveDialog persists true values to storage, and
	// continuation logic uses this marker to skip subagent traces when
	// auto-selecting a conversation to continue.
	MessageIsSubagentKey = "cpe_message_is_subagent"
)

// DialogSaver persists a dialog (a sequence of related messages) to storage.
type DialogSaver interface {
	// SaveDialog validates and persists a root-to-leaf dialog chain.
	//
	// The input iterator must yield messages in parent order (root first, then
	// descendants). For each message:
	//   - If ExtraFields[MessageIDKey] is present, the message is treated as
	//     already persisted. The implementation verifies that the stored message
	//     exists and that its stored parent matches the previous message in this
	//     SaveDialog call. The first message must be a root in storage.
	//   - If ExtraFields[MessageIDKey] is absent, the message is inserted as a
	//     new row whose parent is the previous message from this call. The
	//     returned message includes MessageIDKey and, for non-root messages,
	//     MessageParentIDKey.
	//   - ExtraFields[MessageIsSubagentKey] == true marks the inserted message as
	//     subagent-originated.
	//
	// Atomicity boundary:
	//   - All writes performed while consuming a single SaveDialog iterator are
	//     part of one transaction.
	//   - Validation, insert, or commit failures roll back writes from this call.
	//   - If the consumer stops iteration early (yield returns false), the
	//     processed prefix may still be committed; unconsumed input is ignored.
	//
	// Iterator contract:
	//   - Outputs are yielded in the same order as processed input.
	//   - On error, the iterator yields one non-nil error and then stops.
	//   - Existing messages are yielded without rewriting their content; newly
	//     inserted messages are yielded with storage IDs populated in ExtraFields.
	//
	// Example — saving a brand-new dialog:
	//
	//	dialog := []gai.Message{userMsg, assistantMsg}
	//	idx := 0
	//	for saved, err := range saver.SaveDialog(ctx, slices.Values(dialog)) {
	//		if err != nil {
	//			return err
	//		}
	//		dialog[idx] = saved
	//		idx++
	//	}
	//
	// Example — appending to a previously saved dialog. Messages that already
	// have ExtraFields[MessageIDKey] are validated but not re-saved; only new
	// trailing messages are inserted:
	//
	//	// previousDialog was returned by an earlier SaveDialog call,
	//	// so every message already has MessageIDKey set.
	//	fullDialog := append(previousDialog, newAssistantMsg)
	//	idx := 0
	//	for saved, err := range saver.SaveDialog(ctx, slices.Values(fullDialog)) {
	//		if err != nil {
	//			return err
	//		}
	//		fullDialog[idx] = saved // keep ExtraFields in sync
	//		idx++
	//	}
	//
	// Example — marking a message as subagent-generated:
	//
	//	msg := gai.Message{Role: gai.User, Blocks: blocks}
	//	msg.ExtraFields = map[string]any{
	//		MessageIsSubagentKey: true,
	//	}
	//	for _, err := range saver.SaveDialog(ctx, slices.Values([]gai.Message{msg})) {
	//		if err != nil {
	//			return err
	//		}
	//	}
	SaveDialog(ctx context.Context, msgs iter.Seq[gai.Message]) iter.Seq2[gai.Message, error]
}

// DeleteMessagesOptions configures a message deletion operation.
type DeleteMessagesOptions struct {
	// MessageIDs is the list of message IDs to delete.
	MessageIDs []string

	// Recursive controls whether child messages are also deleted. When false,
	// attempting to delete a message that has children returns an error. When
	// true, the message and all of its descendants are deleted.
	Recursive bool
}

// MessagesDeleter deletes messages from storage.
type MessagesDeleter interface {
	// DeleteMessages deletes the specified messages. If Recursive is true, each
	// message's entire subtree of descendants is also deleted. If Recursive is
	// false and any message has children, an error is returned and no messages
	// are deleted. The entire operation is atomic.
	DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error
}

// ListMessagesOptions configures message listing behavior.
type ListMessagesOptions struct {
	// Offset is the number of messages to skip before returning results,
	// enabling pagination. Zero means start from the beginning.
	Offset uint

	// AscendingOrder controls sort direction on message timestamp. When false
	// (the default zero value), messages are returned in descending order
	// (newest first). When true, messages are returned in ascending order
	// (oldest first).
	AscendingOrder bool
}

// MessagesLister lists messages from storage with ordering and pagination.
type MessagesLister interface {
	// ListMessages returns a snapshot of stored messages ordered by creation time.
	//
	// Ordering is descending by default (newest first) or ascending when
	// opts.AscendingOrder is true. opts.Offset is applied after ordering.
	//
	// Every yielded message has fully populated Blocks and storage metadata in
	// ExtraFields:
	//   - MessageIDKey (always)
	//   - MessageCreatedAtKey (always)
	//   - MessageIsSubagentKey (always)
	//   - MessageParentIDKey (only for non-root messages)
	ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error)
}

// MessagesGetter fetches specific messages by ID.
type MessagesGetter interface {
	// GetMessages retrieves specific messages by ID.
	//
	// If any requested ID is missing, the call returns an error and no iterator.
	// The returned iter.Seq is not guaranteed to preserve the input ID order.
	//
	// Every yielded message has fully populated Blocks and storage metadata in
	// ExtraFields:
	//   - MessageIDKey (always)
	//   - MessageCreatedAtKey (always)
	//   - MessageIsSubagentKey (always)
	//   - MessageParentIDKey (only for non-root messages)
	GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error)
}

// MessageDB is the unified interface for message persistence operations.
// It composes the four single-method interfaces so that consumers requiring
// full storage access can depend on one type, while consumers needing only a
// subset (e.g., only saving or only reading) can depend on the narrower
// interface.
type MessageDB interface {
	DialogSaver
	MessagesDeleter
	MessagesLister
	MessagesGetter
}

// GetDialogForMessage reconstructs the ancestor chain for messageID.
//
// It repeatedly calls getter.GetMessages for one ID at a time, follows
// ExtraFields[MessageParentIDKey], and stops at the first root (message with
// no parent key). The returned dialog is ordered root-to-leaf and includes the
// target message as the last element.
//
// If any message in the chain cannot be loaded, an error is returned.
func GetDialogForMessage(ctx context.Context, getter MessagesGetter, messageID string) (gai.Dialog, error) {
	// Collect messages from the target up to the root (leaf-to-root order)
	collected, err := collectAncestorMessages(ctx, getter, messageID)
	if err != nil {
		return nil, err
	}

	// Reverse so root comes first
	slices.Reverse(collected)

	return gai.Dialog(collected), nil
}

// collectAncestorMessages walks from messageID to the root by following
// MessageParentIDKey, returning messages in leaf-to-root order.
func collectAncestorMessages(ctx context.Context, getter MessagesGetter, messageID string) ([]gai.Message, error) {
	var result []gai.Message
	currentID := messageID

	for {
		msgs, err := getter.GetMessages(ctx, []string{currentID})
		if err != nil {
			return nil, fmt.Errorf("failed to get message %s: %w", currentID, err)
		}

		var found bool
		var msg gai.Message
		for m := range msgs {
			msg = m
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("message %s not found", currentID)
		}

		result = append(result, msg)

		// Read parent ID from ExtraFields
		parentID, _ := msg.ExtraFields[MessageParentIDKey].(string)
		if parentID == "" {
			return result, nil
		}
		currentID = parentID
	}
}
