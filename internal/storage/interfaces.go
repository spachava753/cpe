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
)

// SaveMessageOptions describes a single message to be persisted.
type SaveMessageOptions struct {
	// Message is the gai.Message to save. Its Blocks are persisted alongside
	// the message row. The Role field determines the stored role (user,
	// assistant, tool_result).
	Message gai.Message

	// ParentID is the ID of the parent message in the conversation tree.
	// An empty string indicates this is a root message (start of a new
	// conversation thread).
	ParentID string

	// Title is an optional label for the conversation branch starting at this
	// message. Typically used to annotate subagent execution traces (e.g.,
	// "subagent:<name>:<run_id>"). Empty string means no title.
	Title string
}

// MessagesSaver persists messages to storage.
type MessagesSaver interface {
	// SaveMessages saves one or more messages and returns their generated IDs.
	// The returned iter.Seq yields IDs in the same order as the input opts slice.
	// All messages in a single call are saved atomically — either all succeed or
	// none are persisted. Each message is assigned a unique ID by the
	// implementation; callers do not provide IDs.
	SaveMessages(ctx context.Context, opts []SaveMessageOptions) (iter.Seq[string], error)
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
	MessagesSaver
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
