package acpstar

import (
	"context"
	"fmt"

	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/storage"
)

type sessionValue struct {
	id            string
	cwd           string
	title         string
	lastMessageID string
	messages      starlark.Tuple
	frozen        bool
}

var sessionAttrNames = []string{"cwd", "id", "last_message_id", "messages", "title"}

func newSessionValue(
	ctx context.Context,
	stored storage.GetACPSessionResponse,
	lastMessageID string,
	dialog gai.Dialog,
) (*sessionValue, error) {
	messages := make(starlark.Tuple, len(dialog))
	for i, message := range dialog {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		value, err := newMessageValue(ctx, message)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", i, err)
		}
		messages[i] = value
	}
	if len(messages) > 0 {
		last, ok := messages[len(messages)-1].(*messageValue)
		if !ok || last.id != lastMessageID {
			return nil, fmt.Errorf("last returned message does not match cutoff %q", lastMessageID)
		}
	} else if lastMessageID != "" {
		return nil, fmt.Errorf("history for cutoff %q is empty", lastMessageID)
	}

	title := string(stored.Session.SessionID)
	if stored.Session.Title != nil && *stored.Session.Title != "" {
		title = *stored.Session.Title
	}
	return &sessionValue{
		id:            string(stored.Session.SessionID),
		cwd:           stored.Session.Cwd,
		title:         title,
		lastMessageID: lastMessageID,
		messages:      messages,
	}, nil
}

func (s *sessionValue) String() string {
	return fmt.Sprintf("acp.Session(id=%q, messages=%d)", s.id, len(s.messages))
}

func (*sessionValue) Type() string { return "acp.Session" }

func (s *sessionValue) Freeze() {
	if s.frozen {
		return
	}
	s.frozen = true
	s.messages.Freeze()
}

func (*sessionValue) Truth() starlark.Bool { return starlark.True }

func (s *sessionValue) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable: %s", s.Type())
}

func (s *sessionValue) Attr(name string) (starlark.Value, error) {
	switch name {
	case "id":
		return starlark.String(s.id), nil
	case "cwd":
		return starlark.String(s.cwd), nil
	case "title":
		return starlark.String(s.title), nil
	case "last_message_id":
		return optionalStarlarkString(s.lastMessageID), nil
	case "messages":
		return s.messages, nil
	default:
		return nil, nil
	}
}

func (*sessionValue) AttrNames() []string { return sessionAttrNames }

var _ starlark.HasAttrs = (*sessionValue)(nil)
