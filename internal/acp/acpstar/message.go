package acpstar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/storage"
)

type messageValue struct {
	id                 string
	parentID           string
	compactionParentID string
	createdAt          string
	role               string
	toolResultError    bool
	blocks             starlark.Tuple
	frozen             bool
}

var messageAttrNames = []string{
	"blocks",
	"compaction_parent_id",
	"created_at",
	"id",
	"parent_id",
	"role",
	"tool_result_error",
}

func newMessageValue(ctx context.Context, message gai.Message) (*messageValue, error) {
	messageID, ok := extraString(message.ExtraFields, storage.MessageIDKey)
	if !ok || messageID == "" {
		return nil, errors.New("missing storage message ID")
	}
	createdAt, ok := message.ExtraFields[storage.MessageCreatedAtKey].(time.Time)
	if !ok {
		return nil, fmt.Errorf("message %q has no creation timestamp", messageID)
	}
	role, err := starlarkRole(message.Role)
	if err != nil {
		return nil, fmt.Errorf("message %q: %w", messageID, err)
	}

	blocks := make(starlark.Tuple, len(message.Blocks))
	for i, block := range message.Blocks {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		value, err := newBlockValue(block)
		if err != nil {
			return nil, fmt.Errorf("message %q block %d: %w", messageID, i, err)
		}
		blocks[i] = value
	}
	parentID, _ := extraString(message.ExtraFields, storage.MessageParentIDKey)
	compactionParentID, _ := extraString(message.ExtraFields, storage.MessageCompactionParentIDKey)
	return &messageValue{
		id:                 messageID,
		parentID:           parentID,
		compactionParentID: compactionParentID,
		createdAt:          createdAt.UTC().Format(time.RFC3339Nano),
		role:               role,
		toolResultError:    message.ToolResultError,
		blocks:             blocks,
	}, nil
}

func (m *messageValue) String() string {
	return fmt.Sprintf("acp.Message(id=%q, role=%q, blocks=%d)", m.id, m.role, len(m.blocks))
}

func (*messageValue) Type() string { return "acp.Message" }

func (m *messageValue) Freeze() {
	if m.frozen {
		return
	}
	m.frozen = true
	m.blocks.Freeze()
}

func (*messageValue) Truth() starlark.Bool { return starlark.True }

func (m *messageValue) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable: %s", m.Type())
}

func (m *messageValue) Attr(name string) (starlark.Value, error) {
	switch name {
	case "id":
		return starlark.String(m.id), nil
	case "parent_id":
		return optionalStarlarkString(m.parentID), nil
	case "compaction_parent_id":
		return optionalStarlarkString(m.compactionParentID), nil
	case "created_at":
		return starlark.String(m.createdAt), nil
	case "role":
		return starlark.String(m.role), nil
	case "tool_result_error":
		return starlark.Bool(m.toolResultError), nil
	case "blocks":
		return m.blocks, nil
	default:
		return nil, nil
	}
}

func (*messageValue) AttrNames() []string { return messageAttrNames }

func starlarkRole(role gai.Role) (string, error) {
	switch role {
	case gai.User:
		return "user", nil
	case gai.Assistant:
		return "assistant", nil
	case gai.ToolResult:
		return "tool_result", nil
	default:
		return "", fmt.Errorf("unknown message role %d", role)
	}
}

var _ starlark.HasAttrs = (*messageValue)(nil)
