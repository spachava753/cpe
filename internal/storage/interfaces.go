package storage

import (
	"context"
	"fmt"
	"iter"
	"slices"

	"github.com/spachava753/gai"
)

const (
	// MessageIDKey is the key used in gai.Message.ExtraFields to store a message's
	// unique identifier. Methods that return gai.Message values (GetMessages,
	// ListMessages, GetDialogForMessage) always populate this field. The saving
	// middleware also uses this key to track which messages have already been
	// persisted, and printers read it to display message IDs to the user.
	MessageIDKey = "cpe_message_id"

	// MessageParentIDKey is the key used in gai.Message.ExtraFields to store the
	// ID of a message's parent in the conversation tree. Root messages (those with
	// no parent) will not have this key set in their ExtraFields map. Methods that
	// return gai.Message values (GetMessages, ListMessages, GetDialogForMessage)
	// populate this field when the message has a parent.
	MessageParentIDKey = "cpe_message_parent_id"

	// MessageCreatedAtKey is the key used in gai.Message.ExtraFields to store
	// the message's creation timestamp as a time.Time value. Methods that return
	// gai.Message values (GetMessages, ListMessages, GetDialogForMessage) always
	// populate this field.
	MessageCreatedAtKey = "cpe_message_created_at"

	// MessageTitleKey is the key used in gai.Message.ExtraFields to store an
	// optional label for a message. Typically used to annotate subagent
	// execution traces (e.g., "subagent:<name>:<run_id>"). When saving a
	// dialog, any message with this key set will have its title persisted.
	// Messages without this key have no title.
	MessageTitleKey = "cpe_message_title"
)

// DialogSaver persists a dialog (a sequence of related messages) to storage.
type DialogSaver interface {
	// SaveDialog saves a dialog — a sequence of related messages that form a
	// single conversation thread. The input iterator yields messages in order
	// from root to leaf. The entire operation is performed in a single
	// transaction; if saving any message fails, all changes are rolled back.
	//
	// For each message in the iterator:
	//   - If the message has ExtraFields[MessageIDKey] set, it is treated as
	//     already persisted. The implementation verifies the message exists in
	//     storage. The first message must be a root message (no parent in
	//     storage). For subsequent messages, it verifies that the stored parent
	//     ID matches the previous message's ID. No data is written for
	//     existing messages.
	//   - If the message does not have ExtraFields[MessageIDKey] set, it is
	//     saved to storage. The implementation assigns a unique ID, sets
	//     ExtraFields[MessageIDKey] and ExtraFields[MessageParentIDKey]
	//     (for non-root messages) on the message, and persists it.
	//   - If the message has ExtraFields[MessageTitleKey] set, the title is
	//     persisted with the message.
	//
	// The returned iter.Seq2 yields (gai.Message, error) pairs in the same
	// order as the input. Each yielded message has its ExtraFields populated
	// with at least MessageIDKey (and MessageParentIDKey for non-root messages).
	// On the first error the iterator stops and the transaction is rolled back.
	// Callers must consume the iterator (or break early) to trigger persistence;
	// the transaction is committed when the iterator completes or when the
	// consumer breaks out of the range loop. An empty input iterator results
	// in a no-op (empty transaction committed).
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
	// Example — appending to a previously saved dialog. Messages that
	// already have ExtraFields[MessageIDKey] are verified but not re-saved;
	// only the new message at the end is persisted:
	//
	//	// previousDialog was returned by an earlier SaveDialog call,
	//	// so every message already has MessageIDKey set.
	//	fullDialog := append(previousDialog, newAssistantMsg)
	//	for saved, err := range saver.SaveDialog(ctx, slices.Values(fullDialog)) {
	//		if err != nil {
	//			return err
	//		}
	//		fullDialog[i] = saved // keep ExtraFields in sync
	//	}
	//
	// Example — annotating messages with a title (e.g. for subagent traces):
	//
	//	msg := gai.Message{Role: gai.User, Blocks: blocks}
	//	msg.ExtraFields = map[string]any{
	//		MessageTitleKey: "subagent:researcher:run_abc",
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
	// ListMessages returns messages ordered by creation timestamp. The default
	// order is descending (newest first). Each yielded gai.Message has its ID
	// stored in ExtraFields[MessageIDKey] and, if the message has a parent, its
	// parent ID stored in ExtraFields[MessageParentIDKey]. The message's Blocks
	// are fully populated.
	ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error)
}

// MessagesGetter fetches specific messages by ID.
type MessagesGetter interface {
	// GetMessages retrieves messages by their IDs. The returned iter.Seq is not
	// guaranteed to yield messages in the same order as the input IDs. Each
	// yielded gai.Message has its ID stored in ExtraFields[MessageIDKey] and, if
	// the message has a parent, its parent ID stored in
	// ExtraFields[MessageParentIDKey]. If any requested ID does not exist, an
	// error is returned.
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

// GetDialogForMessage is a utility function that reconstructs the full
// conversation history leading up to the given message ID. It works by
// fetching the message via getter, reading its parent ID from
// ExtraFields[MessageParentIDKey], and repeating until a root message (one
// with no parent) is reached. The returned gai.Dialog is ordered from root
// to the target message. Each message in the dialog has MessageIDKey and
// (where applicable) MessageParentIDKey set in its ExtraFields.
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

// collectAncestorMessages walks up the parent chain from messageID to the root,
// returning the messages in leaf-to-root order.
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
