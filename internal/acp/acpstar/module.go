package acpstar

import (
	"context"
	"fmt"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/storage"
)

const modulePath = "acp.star"

// SessionStore is the persisted-session read interface required by acp.star.
type SessionStore interface {
	storage.ACPSessionGetter
	storage.ACPSessionsLister
	storage.MessagesGetter
}

type module struct {
	store            SessionStore
	currentSessionID acpsdk.SessionId
	currentCwd       string
}

// Module returns the session-scoped acp.star module source.
func Module(store SessionStore, sessionID acpsdk.SessionId, cwd string) dyson.ModuleSet {
	m := &module{
		store:            store,
		currentSessionID: sessionID,
		currentCwd:       cwd,
	}
	value := &starlarkstruct.Module{
		Name: "acp",
		Members: starlark.StringDict{
			"get_session":   starlark.NewBuiltin("acp.get_session", m.getSession),
			"list_sessions": starlark.NewBuiltin("acp.list_sessions", m.listSessions),
		},
	}
	return dyson.ModuleSet{modulePath: starlark.StringDict{"acp": value}}
}

func (m *module) getSession(
	thread *starlark.Thread,
	fn *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	sessionID := string(m.currentSessionID)
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "id??", &sessionID); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, fmt.Errorf("%s: session ID must not be empty", fn.Name())
	}
	if m.store == nil {
		return nil, fmt.Errorf("%s: persisted session store is unavailable", fn.Name())
	}
	ctx := dyson.EvaluationContext(thread)
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", fn.Name(), err)
	}

	stored, err := m.store.GetACPSession(ctx, acpsdk.SessionId(sessionID))
	if err != nil {
		return nil, fmt.Errorf("%s: get session %q: %w", fn.Name(), sessionID, err)
	}
	lastMessageID := stored.LastMessageID
	if acpsdk.SessionId(sessionID) == m.currentSessionID {
		lastMessageID = xctx.ExecutionMessageIDFrom(ctx)
		if lastMessageID == "" {
			return nil, fmt.Errorf("%s: current tool call has no persisted assistant message ID", fn.Name())
		}
	}

	var dialog gai.Dialog
	if lastMessageID != "" {
		dialog, err = storage.GetDialogWithCompactions(ctx, m.store, lastMessageID)
		if err != nil {
			return nil, fmt.Errorf("%s: get session %q history: %w", fn.Name(), sessionID, err)
		}
	}
	value, err := newSessionValue(ctx, stored, lastMessageID, dialog)
	if err != nil {
		return nil, fmt.Errorf("%s: convert session %q: %w", fn.Name(), sessionID, err)
	}
	value.Freeze()
	return value, nil
}

func (m *module) listSessions(
	thread *starlark.Thread,
	fn *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	cwd := m.currentCwd
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "cwd??", &cwd); err != nil {
		return nil, err
	}
	if m.store == nil {
		return nil, fmt.Errorf("%s: persisted session store is unavailable", fn.Name())
	}
	ctx := dyson.EvaluationContext(thread)
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", fn.Name(), err)
	}
	sessions, err := m.store.ListACPSessions(ctx, &cwd)
	if err != nil {
		return nil, fmt.Errorf("%s: list persisted sessions: %w", fn.Name(), err)
	}
	ids := make([]starlark.Value, len(sessions))
	for i, session := range sessions {
		if err := context.Cause(ctx); err != nil {
			return nil, fmt.Errorf("%s: %w", fn.Name(), err)
		}
		ids[i] = starlark.String(session.SessionID)
	}
	return starlark.NewList(ids), nil
}
