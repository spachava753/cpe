package acp

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
	"github.com/spachava753/gai"
)

type acpRuntime interface {
	Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error)
}

type dialogDb interface {
	storage.ACPSessionGetter
	storage.MessagesGetter
}

// session represents an active session in ACP. Note that
// while not described in the protocol, sessions may be
// accessed and mutated in parallel
type session struct {
	modelRef   string
	runtime    acpRuntime
	cancelfunc context.CancelFunc
	si         acp.SessionInfo
}

// NewSession implements [acp.Agent].
func (a *Agent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	id := a.genId()
	si := acp.SessionInfo{
		Cwd:       params.Cwd,
		Title:     new("untitled"),
		SessionId: id,
	}
	if err := a.db.CreateACPSession(ctx, storage.CreateACPSessionParams{
		Session:       si,
		LastMessageID: "",
		ModelRef:      "",
	}); err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("could not save created session: %v", err)
	}
	s := session{
		si: si,
	}
	a.activeSessions.Store(id, sync.NewGuard(s))
	return acp.NewSessionResponse{
		SessionId: id,
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: a.configOption(modelRef, s.modelRef),
			},
		},
	}, nil
}

// ListSessions implements [acp.Agent].
func (a *Agent) ListSessions(ctx context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	sessionInfos, err := a.db.ListACPSessions(ctx)
	if err != nil {
		return acp.ListSessionsResponse{}, fmt.Errorf("could not list sessions: %v", err)
	}
	resp := acp.ListSessionsResponse{
		Sessions: make([]acp.SessionInfo, 0, len(sessionInfos)),
	}
	for _, s := range sessionInfos {
		resp.Sessions = append(resp.Sessions, s)
	}
	return resp, nil
}

// loadActiveSession loads an active session from storage
func (a *Agent) loadActiveSession(ctx context.Context, sessionId acp.SessionId) ([]acp.SessionConfigOption, error) {
	if s, ok := a.activeSessions.Load(sessionId); ok {
		var modelRefVal string
		s.Do(func(t session) error {
			modelRefVal = t.modelRef
			return nil
		})
		return []acp.SessionConfigOption{
			{
				Select: a.configOption(modelRef, modelRefVal),
			},
		}, nil
	}

	getSessionResp, err := a.db.GetACPSession(ctx, sessionId)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch acp session %s from storage: %v",
			sessionId,
			err,
		)
	}
	s := session{
		si:      getSessionResp.Session,
		runtime: a.runtimeGen(getSessionResp.ModelRef),
	}
	a.activeSessions.Store(sessionId, sync.NewGuard(s))
	return []acp.SessionConfigOption{
		{
			Select: a.configOption(modelRef, getSessionResp.ModelRef),
		},
	}, nil
}

// ResumeSession implements [acp.Agent].
func (a *Agent) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionId)
	if err != nil {
		return acp.ResumeSessionResponse{}, fmt.Errorf("could not resume session: %v", err)
	}
	return acp.ResumeSessionResponse{
		ConfigOptions: opts,
	}, nil
}

// LoadSession implements [acp.AgentLoader].
func (a *Agent) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionId)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("could not load session: %v", err)
	}
	acpSession, err := a.db.GetACPSession(ctx, params.SessionId)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("could get acp session from db: %v", err)
	}
	dialog, err := storage.GetDialogForMessage(ctx, a.store, acpSession.LastMessageID)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("could not get dialog from db: %v", err)
	}
	for _, msg := range dialog {
		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: params.SessionId,
			Update:    msgToSessionUpdate(msg),
		}); err != nil {
			return acp.LoadSessionResponse{}, fmt.Errorf("could not send session update: %v", err)
		}
	}
	return acp.LoadSessionResponse{
		ConfigOptions: opts,
	}, nil
}

func msgToSessionUpdate(msg gai.Message) acp.SessionUpdate {
	panic("unimplemented")
}

// Cancel implements [acp.Agent].
func (a *Agent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	s, ok := a.activeSessions.Load(params.SessionId)
	if !ok {
		return fmt.Errorf("session %s not found", params.SessionId)
	}
	return s.Do(func(t session) error {
		if t.cancelfunc != nil {
			t.cancelfunc()
			t.cancelfunc = nil
		}
		return nil
	})
}

// CloseSession implements [acp.Agent].
func (a *Agent) CloseSession(ctx context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	panic("unimplemented")
}
