package storage

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"time"

	acp "github.com/spachava753/acp-sdk/acp"
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

	// MessageCompactionParentIDKey is the gai.Message.ExtraFields key for the
	// message ID from which a compacted branch was created.
	//
	// The value is a string. It is only set on the root message of a compacted
	// branch and points to the last message ID of the pre-compaction branch.
	MessageCompactionParentIDKey = "cpe_message_compaction_parent_id"

	// MessageCreatedAtKey is the gai.Message.ExtraFields key for the message
	// creation timestamp.
	//
	// The value is a time.Time and is populated by read APIs.
	MessageCreatedAtKey = "cpe_message_created_at"

	// AgentMetadataModelRefKey is the gai.Message.ExtraFields key for the CPE
	// model profile reference used to generate an assistant message.
	AgentMetadataModelRefKey = "cpe_agent_model_ref"

	// AgentMetadataModelIDKey is the gai.Message.ExtraFields key for the provider
	// model identifier used to generate an assistant message.
	AgentMetadataModelIDKey = "cpe_agent_model_id"

	// AgentMetadataModelTypeKey is the gai.Message.ExtraFields key for the model
	// provider/type used to generate an assistant message.
	AgentMetadataModelTypeKey = "cpe_agent_model_type"

	// AgentMetadataModelDisplayNameKey is the gai.Message.ExtraFields key for the
	// configured display name of the model used to generate an assistant message.
	AgentMetadataModelDisplayNameKey = "cpe_agent_model_display_name"

	// AgentMetadataInputTokensKey is the gai.Message.ExtraFields key for prompt
	// tokens reported by the model provider for an assistant message.
	AgentMetadataInputTokensKey = "cpe_agent_input_tokens"

	// AgentMetadataOutputTokensKey is the gai.Message.ExtraFields key for
	// completion tokens reported by the model provider for an assistant message.
	AgentMetadataOutputTokensKey = "cpe_agent_output_tokens"

	// AgentMetadataCacheReadTokensKey is the gai.Message.ExtraFields key for cache
	// read tokens reported by the model provider for an assistant message.
	AgentMetadataCacheReadTokensKey = "cpe_agent_cache_read_tokens"

	// AgentMetadataCacheWriteTokensKey is the gai.Message.ExtraFields key for
	// cache write tokens reported by the model provider for an assistant message.
	AgentMetadataCacheWriteTokensKey = "cpe_agent_cache_write_tokens"
)

// deleteMessagesOptions configures a message deletion operation.
type deleteMessagesOptions struct {
	// MessageIDs is the list of message IDs to delete.
	MessageIDs []string

	// Recursive controls whether child messages are also deleted. When false,
	// attempting to delete a message that has children returns an error. When
	// true, the message and all of its descendants are deleted.
	Recursive bool
}

// listMessagesOptions configures message listing behavior.
type listMessagesOptions struct {
	// Offset is the number of messages to skip before returning results,
	// enabling pagination. Zero means start from the beginning.
	Offset uint

	// AscendingOrder controls sort direction on message timestamp. When false
	// (the default zero value), messages are returned in descending order
	// (newest first). When true, messages are returned in ascending order
	// (oldest first).
	AscendingOrder bool
}

// MessagesGetter fetches specific messages by ID.
type MessagesGetter interface {
	// GetMessages retrieves specific messages by ID.
	//
	// If any requested ID is missing, the call returns an error wrapping
	// ErrMessageNotFound and no iterator.
	// The returned iter.Seq is not guaranteed to preserve the input ID order.
	//
	// Every yielded message has fully populated Blocks and storage metadata in
	// ExtraFields:
	//   - MessageIDKey (always)
	//   - MessageCreatedAtKey (always)
	//   - MessageParentIDKey (only for non-root messages)
	//   - MessageCompactionParentIDKey (only for compacted branch roots)
	GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error)
}

// acpSessionSummary is the persisted metadata shown by session inspection
// clients. LastModified is the later of the session creation time and its head
// message creation time.
type acpSessionSummary struct {
	SessionID    acp.SessionId
	Title        string
	CreatedAt    time.Time
	LastModified time.Time
}

// ListACPSessionSummariesOptions configures paginated session inspection.
type ListACPSessionSummariesOptions struct {
	Limit  uint64
	Offset uint64
}

// GetACPSessionResponse is the result of loading ACP session metadata from
// storage.
type GetACPSessionResponse struct {
	Session       acp.SessionInfo
	LastMessageID string
	ModelRef      string
	ThinkingLevel string
	// CostUSD is the cumulative cost in US dollars accrued by the session
	// across all prompt turns, models, and process restarts.
	CostUSD float64
}

// CreateACPSessionParams configures ACP session creation.
type CreateACPSessionParams struct {
	// Session is the ACP protocol metadata to persist.
	Session acp.SessionInfo

	// LastMessageID is the session's current latest persisted message. Empty
	// means the ACP session has no persisted messages yet.
	LastMessageID string

	// ModelRef is the selected CPE model profile reference for the session.
	ModelRef string

	// ThinkingLevel is the selected reasoning effort level for the session.
	ThinkingLevel string
}

// ACPSessionGetter fetches ACP session metadata by session ID.
type ACPSessionGetter interface {
	// GetACPSession returns ACP session metadata, the latest persisted message ID,
	// selected model profile reference, and reasoning effort level for sessionID.
	//
	// The returned SessionInfo.UpdatedAt is an ISO 8601 timestamp derived from
	// the session's creation time. It returns an error wrapping
	// ErrSessionNotFound when sessionID is missing.
	GetACPSession(ctx context.Context, sessionID acp.SessionId) (GetACPSessionResponse, error)
}

// ACPSessionsLister lists ACP session metadata.
type ACPSessionsLister interface {
	// ListACPSessions returns ACP sessions ordered by last activity, newest first.
	// When cwd is non-nil, only sessions with an exactly matching working
	// directory are returned. A nil cwd returns sessions from all directories.
	//
	// Each returned SessionInfo.UpdatedAt is an ISO 8601 timestamp derived from
	// the session's creation time.
	ListACPSessions(ctx context.Context, cwd *string) ([]acp.SessionInfo, error)
}

// GetDialogForMessage reconstructs the ancestor chain for messageID.
//
// It repeatedly calls getter.GetMessages for one ID at a time, follows
// ExtraFields[MessageParentIDKey], and stops at the first root (message with
// no parent key). The returned dialog is ordered root-to-leaf and includes the
// target message as the last element.
//
// If any message in the chain cannot be loaded, an error is returned. Missing
// messages are reported with errors wrapping ErrMessageNotFound.
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

// GetDialogWithCompactions reconstructs the complete history ending at
// messageID, including every prior dialog replaced by compaction. Compacted
// branch roots remain in the result and retain MessageCompactionParentIDKey so
// callers can render the boundaries explicitly.
func GetDialogWithCompactions(ctx context.Context, getter MessagesGetter, messageID string) (gai.Dialog, error) {
	dialog, err := GetDialogForMessage(ctx, getter, messageID)
	if err != nil {
		return nil, err
	}
	for len(dialog) > 0 {
		compactionParentID, _ := dialog[0].ExtraFields[MessageCompactionParentIDKey].(string)
		if compactionParentID == "" {
			return dialog, nil
		}
		parentDialog, err := GetDialogForMessage(ctx, getter, compactionParentID)
		if err != nil {
			return nil, fmt.Errorf("get compaction parent dialog: %w", err)
		}
		dialog = append(parentDialog, dialog...)
	}
	return dialog, nil
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
			return nil, fmt.Errorf("message %s not found: %w", currentID, ErrMessageNotFound)
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

// GetMessageID retrieves the message ID from a message's ExtraFields.
// Returns an empty string if no ID is set.
func GetMessageID(msg gai.Message) string {
	if msg.ExtraFields == nil {
		return ""
	}
	id, _ := msg.ExtraFields[MessageIDKey].(string)
	return id
}
