package acp

import (
	"context"
	"fmt"
	"slices"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xacp"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
)

type runtime interface {
	Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error)
	Close() error
}

// session represents an active session in ACP. Note that
// while not described in the protocol, sessions may be
// accessed and mutated in parallel
type session struct {
	id         acp.SessionId
	cwd        string
	model      string
	thinking   string
	mcpServers []acp.McpServer

	// session state
	runtime    runtime
	cancelfunc context.CancelFunc
}

func (a *Agent) activeSession(sessionID acp.SessionId) (*sync.Guard[session], error) {
	s, ok := a.activeSessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}
	return s, nil
}

// NewSession implements [acp.SessionHandler].
func (a *Agent) NewSession(ctx context.Context, params *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	id := a.genId()
	si := acp.SessionInfo{
		Cwd:       params.Cwd,
		Title:     new("untitled"),
		SessionID: id,
	}
	modelRef := a.rawCfg.Models[0].Ref
	var thinkingVal string
	if len(a.rawCfg.Models[0].ThinkingValues) > 0 {
		thinkingVal = a.rawCfg.Models[0].ThinkingValues[0].Value
	}
	s := session{
		id:         id,
		cwd:        params.Cwd,
		model:      modelRef,
		thinking:   thinkingVal,
		mcpServers: params.McpServers,
	}

	if err := a.db.CreateACPSession(ctx, storage.CreateACPSessionParams{
		Session:       si,
		LastMessageID: "",
		ModelRef:      s.model,
		ThinkingLevel: s.thinking,
	}); err != nil {
		return nil, fmt.Errorf("could not save created session: %v", err)
	}
	a.activeSessions.Store(id, sync.NewGuard(s))
	opts := a.configOptions(ctx, id)
	return &acp.NewSessionResponse{
		SessionID:     id,
		ConfigOptions: &opts,
	}, nil
}

// ListSessions implements [acp.SessionHandler].
func (a *Agent) ListSessions(ctx context.Context, params *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	sessionInfos, err := a.db.ListACPSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list sessions: %v", err)
	}
	resp := &acp.ListSessionsResponse{
		Sessions: make([]acp.SessionInfo, 0, len(sessionInfos)),
	}
	resp.Sessions = append(resp.Sessions, sessionInfos...)
	return resp, nil
}

// loadActiveSession loads an active session from storage
func (a *Agent) loadActiveSession(
	ctx context.Context,
	sessionId acp.SessionId,
	cwd string,
	mcpServers []acp.McpServer,
) ([]acp.SessionConfigOption, error) {
	// TODO: should we always load from db? Maybe be better, especially since config can change
	if s, ok := a.activeSessions.Load(sessionId); ok {
		// session already exists, but maybe we should reload the runtime based on stored config options?
		_ = s
		return a.configOptions(ctx, sessionId), nil
	}

	getSessionResp, err := a.db.GetACPSession(ctx, sessionId)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch acp session %s from storage: %v",
			sessionId,
			err,
		)
	}

	// config options may be stale, double check them
	modelRef := getSessionResp.ModelRef
	thinkingLevel := getSessionResp.ThinkingLevel
	idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
		return m.Ref == getSessionResp.ModelRef
	})
	if idx == -1 {
		// model profile is stale, default the first one in config
		modelRef = a.rawCfg.Models[0].Ref
		thinkingLevel = ""
		if len(a.rawCfg.Models[0].ThinkingValues) > 0 {
			thinkingLevel = a.rawCfg.Models[0].ThinkingValues[0].Value
		}

		if err := a.db.SetACPSessionModelRef(ctx, sessionId, modelRef); err != nil {
			return nil, fmt.Errorf("could not update model ref config: %v", err)
		}
		if err := a.db.SetACPSessionThinkingLevel(ctx, sessionId, thinkingLevel); err != nil {
			return nil, fmt.Errorf("could not update thinking level config: %v", err)
		}
		s := session{
			id:         sessionId,
			cwd:        cwd,
			model:      modelRef,
			thinking:   thinkingLevel,
			mcpServers: mcpServers,
		}
		runtime, err := a.runtimeFactory.Create(ctx, s, a.clientCaps)
		if err != nil {
			return nil, fmt.Errorf("could not create runtime: %v", err)
		}
		s.runtime = runtime
		a.activeSessions.Store(sessionId, sync.NewGuard(s))
		return a.configOptions(ctx, sessionId), nil
	}

	// model ref is valid, double check thinking value
	if !slices.ContainsFunc(
		a.rawCfg.Models[idx].ThinkingValues,
		func(tv config.ThinkingValueConfig) bool {
			return tv.Value == thinkingLevel
		}) {
		thinkingLevel = ""
		if len(a.rawCfg.Models[idx].ThinkingValues) > 0 {
			thinkingLevel = a.rawCfg.Models[idx].ThinkingValues[0].Value
		}
		if err := a.db.SetACPSessionThinkingLevel(ctx, sessionId, thinkingLevel); err != nil {
			return nil, fmt.Errorf("could not update thinking level config: %v", err)
		}
	}

	s := session{
		id:         sessionId,
		cwd:        cwd,
		model:      modelRef,
		thinking:   thinkingLevel,
		mcpServers: mcpServers,
	}
	runtime, err := a.runtimeFactory.Create(ctx, s, a.clientCaps)
	if err != nil {
		return nil, fmt.Errorf("could not create runtime: %v", err)
	}
	s.runtime = runtime

	a.activeSessions.Store(sessionId, sync.NewGuard(s))
	return a.configOptions(ctx, sessionId), nil
}

// ResumeSession implements [acp.SessionHandler].
func (a *Agent) ResumeSession(ctx context.Context, params *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionID, params.Cwd, params.McpServers)
	if err != nil {
		return nil, fmt.Errorf("could not resume session: %v", err)
	}
	return &acp.ResumeSessionResponse{
		ConfigOptions: &opts,
	}, nil
}

// LoadSession implements [acp.SessionHandler].
func (a *Agent) LoadSession(ctx context.Context, params *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionID, params.Cwd, params.McpServers)
	if err != nil {
		return nil, fmt.Errorf("could not load session: %v", err)
	}
	acpSession, err := a.db.GetACPSession(ctx, params.SessionID)
	if err != nil {
		return nil, fmt.Errorf("could get acp session from db: %v", err)
	}
	dialog, err := storage.GetDialogForMessage(ctx, a.db, acpSession.LastMessageID)
	if err != nil {
		return nil, fmt.Errorf("could not get dialog from db: %v", err)
	}
	for len(dialog) > 0 {
		compactionParentID, _ := dialog[0].ExtraFields[storage.MessageCompactionParentIDKey].(string)
		if compactionParentID == "" {
			break
		}
		parentDialog, err := storage.GetDialogForMessage(ctx, a.db, compactionParentID)
		if err != nil {
			return nil, fmt.Errorf("could not get compaction parent dialog from db: %v", err)
		}
		dialog = append(parentDialog, dialog...)
	}
	for _, msg := range dialog {
		for update := range xacp.MsgToSessionUpdate(msg) {
			if err := a.conn.SessionUpdate(ctx, &acp.SessionNotification{
				SessionID: params.SessionID,
				Update:    update,
			}); err != nil {
				return nil, fmt.Errorf("could not send session update: %v", err)
			}
		}
	}
	return &acp.LoadSessionResponse{
		ConfigOptions: &opts,
	}, nil
}

// ForkSession implements ACP's unstable session/fork method.
//
// The forked session shares the source session's persisted history: the new
// session points at the same last message, so prompts on either session
// diverge as separate branches of the message tree without copying rows.
// The forked session's cumulative cost starts at zero; cost already accrued
// by the source session is not inherited.
// Stale stored config is resolved against the loaded config the same way
// session resumption does. The forked session's runtime is created lazily on
// the first prompt, like sessions created via session/new.
func (a *Agent) ForkSession(
	ctx context.Context,
	params *acp.ForkSessionRequest,
) (*acp.ForkSessionResponse, error) {
	src, err := a.db.GetACPSession(ctx, params.SessionID)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch acp session %s from storage: %v",
			params.SessionID,
			err,
		)
	}

	modelRef := src.ModelRef
	thinkingLevel := src.ThinkingLevel
	idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
		return m.Ref == modelRef
	})
	if idx == -1 {
		// model profile is stale, default to the first one in config
		modelRef = a.rawCfg.Models[0].Ref
		thinkingLevel = ""
		if len(a.rawCfg.Models[0].ThinkingValues) > 0 {
			thinkingLevel = a.rawCfg.Models[0].ThinkingValues[0].Value
		}
	} else if !slices.ContainsFunc(
		a.rawCfg.Models[idx].ThinkingValues,
		func(tv config.ThinkingValueConfig) bool {
			return tv.Value == thinkingLevel
		}) {
		thinkingLevel = ""
		if len(a.rawCfg.Models[idx].ThinkingValues) > 0 {
			thinkingLevel = a.rawCfg.Models[idx].ThinkingValues[0].Value
		}
	}

	id := a.genId()
	title := src.Session.Title
	if title == nil {
		title = new("untitled")
	}
	if err := a.db.CreateACPSession(ctx, storage.CreateACPSessionParams{
		Session: acp.SessionInfo{
			Cwd:       params.Cwd,
			Title:     title,
			SessionID: id,
		},
		LastMessageID: src.LastMessageID,
		ModelRef:      modelRef,
		ThinkingLevel: thinkingLevel,
	}); err != nil {
		return nil, fmt.Errorf("could not save forked session: %v", err)
	}

	a.activeSessions.Store(id, sync.NewGuard(session{
		cwd:        params.Cwd,
		model:      modelRef,
		thinking:   thinkingLevel,
		mcpServers: params.McpServers,
	}))

	opts := a.configOptions(ctx, id)
	return &acp.ForkSessionResponse{
		SessionID:     id,
		ConfigOptions: &opts,
	}, nil
}

// Cancel implements [acp.SessionHandler].
func (a *Agent) Cancel(ctx context.Context, params *acp.CancelNotification) error {
	s, ok := a.activeSessions.Load(params.SessionID)
	if !ok {
		return fmt.Errorf("session %s not found", params.SessionID)
	}
	return s.Do(func(t *session) error {
		if t.cancelfunc != nil {
			t.cancelfunc()
		}
		return nil
	})
}

// CloseSession implements [acp.SessionHandler].
func (a *Agent) CloseSession(ctx context.Context, params *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	s, err := a.activeSession(params.SessionID)
	if err != nil {
		return nil, err
	}
	if err := s.Do(func(t *session) error {
		if t.runtime == nil {
			return nil
		}
		return t.runtime.Close()
	}); err != nil {
		return nil, fmt.Errorf("could not close session %s, %v", params.SessionID, err)
	}
	a.activeSessions.Delete(params.SessionID)
	return &acp.CloseSessionResponse{}, nil
}

// DeleteSession implements [acp.SessionHandler].
func (a *Agent) DeleteSession(
	ctx context.Context,
	params *acp.DeleteSessionRequest,
) (*acp.DeleteSessionResponse, error) {
	if s, ok := a.activeSessions.Load(params.SessionID); ok {
		if err := s.Do(func(t *session) error {
			if t.cancelfunc != nil {
				t.cancelfunc()
				t.cancelfunc = nil
			}
			if t.runtime == nil {
				return nil
			}
			return t.runtime.Close()
		}); err != nil {
			return nil, fmt.Errorf("could not close session %s before delete: %v", params.SessionID, err)
		}
	}
	if err := a.db.DeleteACPSession(ctx, params.SessionID); err != nil {
		return nil, fmt.Errorf("could not delete session %s: %v", params.SessionID, err)
	}
	a.activeSessions.Delete(params.SessionID)
	return &acp.DeleteSessionResponse{}, nil
}
