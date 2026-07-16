package acp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xacp"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/skills"
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
	// skillCatalog is refreshed before prompt turns and session config mutations,
	// then used for system prompt rendering and slash-command expansion.
	skillCatalog skills.Catalog

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

// discoverSkills loads the skill catalog for an ACP session. Tests can override
// Agent.skillHomeDir so global user skills do not leak into session fixtures.
func (a *Agent) discoverSkills(ctx context.Context, cwd string) skills.Catalog {
	return skills.Discover(ctx, skills.DiscoverOptions{
		Cwd:     cwd,
		HomeDir: a.skillHomeDir,
	})
}

// sendAvailableSkillCommands publishes ACP autocomplete metadata for the
// session's discovered skills. It intentionally sends nothing when no skills are
// available so clients do not receive empty command updates.
func (a *Agent) sendAvailableSkillCommands(ctx context.Context, sessionID acp.SessionId, catalog skills.Catalog) error {
	commands := availableSkillCommands(catalog)
	if len(commands) == 0 || a.conn == nil {
		return nil
	}
	return a.conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: sessionID,
		Update:    acp.AvailableCommandsUpdateSessionUpdate(commands),
	})
}

func (a *Agent) refreshAvailableSkillCommands(ctx context.Context, sessionID acp.SessionId, s *sync.Guard[session]) error {
	var cwd string
	if err := s.Do(func(t *session) error {
		cwd = t.cwd
		return nil
	}); err != nil {
		return err
	}
	ctx = withSessionLogAttrs(ctx, sessionID, cwd)
	catalog := a.discoverSkills(ctx, cwd)
	if err := a.sendAvailableSkillCommands(ctx, sessionID, catalog); err != nil {
		return err
	}
	return s.Do(func(t *session) error {
		t.skillCatalog = catalog
		return nil
	})
}

// NewSession implements [acp.SessionHandler].
func (a *Agent) NewSession(ctx context.Context, params *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	id := a.genId()
	si := acp.SessionInfo{
		Cwd:       params.Cwd,
		Title:     new(string(id)),
		SessionID: id,
	}
	var modelRef string
	var thinkingVal string
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
	var cwd *string
	if params != nil {
		cwd = params.Cwd
	}
	sessionInfos, err := a.db.ListACPSessions(ctx, cwd)
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
	getSessionResp, err := a.db.GetACPSession(ctx, sessionId)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch acp session %s from storage: %v",
			sessionId,
			err,
		)
	}
	if getSessionResp.Session.Cwd != cwd {
		return nil, fmt.Errorf(
			"session %s belongs to working directory %q, not %q",
			sessionId,
			getSessionResp.Session.Cwd,
			cwd,
		)
	}

	// Reuse active state only after validating the request against persisted
	// session metadata.
	if _, ok := a.activeSessions.Load(sessionId); ok {
		return a.configOptions(ctx, sessionId), nil
	}

	// config options may be stale, double check them
	modelRef := getSessionResp.ModelRef
	thinkingLevel := getSessionResp.ThinkingLevel
	if modelRef == "" {
		if thinkingLevel != "" {
			thinkingLevel = ""
			if err := a.db.SetACPSessionThinkingLevel(ctx, sessionId, thinkingLevel); err != nil {
				return nil, fmt.Errorf("could not clear thinking level config: %v", err)
			}
		}
		a.activeSessions.Store(sessionId, sync.NewGuard(session{
			id:         sessionId,
			cwd:        cwd,
			mcpServers: mcpServers,
		}))
		return a.configOptions(ctx, sessionId), nil
	}
	idx := slices.IndexFunc(a.rawCfg.Models, func(m config.ModelConfig) bool {
		return m.Ref == getSessionResp.ModelRef
	})
	if idx == -1 {
		// A stale model invalidates its thinking level too; require the client to pick a model before prompting.
		modelRef = ""
		thinkingLevel = ""
		if err := a.db.SetACPSessionModelRef(ctx, sessionId, modelRef); err != nil {
			return nil, fmt.Errorf("could not update model ref config: %v", err)
		}
		if err := a.db.SetACPSessionThinkingLevel(ctx, sessionId, thinkingLevel); err != nil {
			return nil, fmt.Errorf("could not update thinking level config: %v", err)
		}
		a.activeSessions.Store(sessionId, sync.NewGuard(session{
			id:         sessionId,
			cwd:        cwd,
			mcpServers: mcpServers,
		}))
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
		// A stale model invalidates its thinking level too; fork into an unpicked model state.
		modelRef = ""
		thinkingLevel = ""
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
	if err := a.db.CreateACPSession(ctx, storage.CreateACPSessionParams{
		Session: acp.SessionInfo{
			Cwd:       params.Cwd,
			Title:     new(string(id)),
			SessionID: id,
		},
		LastMessageID: src.LastMessageID,
		ModelRef:      modelRef,
		ThinkingLevel: thinkingLevel,
	}); err != nil {
		return nil, fmt.Errorf("could not save forked session: %v", err)
	}

	a.activeSessions.Store(id, sync.NewGuard(session{
		id:         id,
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
	s, ok := a.activeSessions.Load(params.SessionID)
	if !ok {
		slog.InfoContext(
			withSessionLogAttrs(ctx, params.SessionID, ""),
			"session close requested for unknown session",
		)
		return &acp.CloseSessionResponse{}, nil
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
	s, ok := a.activeSessions.Load(params.SessionID)
	if ok {
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
		if errors.Is(err, storage.ErrSessionNotFound) {
			slog.InfoContext(
				withSessionLogAttrs(ctx, params.SessionID, ""),
				"session delete requested for unknown session",
			)
			a.activeSessions.Delete(params.SessionID)
			return &acp.DeleteSessionResponse{}, nil
		}
		return nil, fmt.Errorf("could not delete session %s: %v", params.SessionID, err)
	}
	a.activeSessions.Delete(params.SessionID)
	return &acp.DeleteSessionResponse{}, nil
}
