package acp

import (
	"context"
	"fmt"
	"slices"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xacp"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
)

type acpRuntime interface {
	Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error)
	Close() error
}

// session represents an active session in ACP. Note that
// while not described in the protocol, sessions may be
// accessed and mutated in parallel
type session struct {
	cwd           string
	modelRef      string
	thinkingLevel string
	mcpServers    []acp.McpServer
	runtime       acpRuntime
	cancelfunc    context.CancelFunc
}

func (a *Agent) activeSession(sessionID acp.SessionId) (*sync.Guard[session], error) {
	s, ok := a.activeSessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}
	return s, nil
}

// NewSession implements [acp.Agent].
func (a *Agent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	id := a.genId()
	si := acp.SessionInfo{
		Cwd:       params.Cwd,
		Title:     new("untitled"),
		SessionId: id,
	}
	modelRef := a.rawCfg.Models[0].Ref
	var thinkingVal string
	if len(a.rawCfg.Models[0].ThinkingValues) > 0 {
		thinkingVal = a.rawCfg.Models[0].ThinkingValues[0].Value
	}
	s := session{
		cwd:           params.Cwd,
		modelRef:      modelRef,
		thinkingLevel: thinkingVal,
		mcpServers:    params.McpServers,
	}

	if err := a.db.CreateACPSession(ctx, storage.CreateACPSessionParams{
		Session:       si,
		LastMessageID: "",
		ModelRef:      s.modelRef,
		ThinkingLevel: s.thinkingLevel,
	}); err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("could not save created session: %v", err)
	}
	a.activeSessions.Store(id, sync.NewGuard(s))
	return acp.NewSessionResponse{
		SessionId:     id,
		ConfigOptions: a.configOptions(ctx, id),
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
		runtime, err := a.runtimeFactory(runtimeOpts{
			conn:       a.conn,
			modelRef:   modelRef,
			mcpServers: mcpServers,
		})
		if err != nil {
			return nil, fmt.Errorf("could not create runtime: %v", err)
		}
		s := session{
			cwd:           cwd,
			modelRef:      modelRef,
			thinkingLevel: thinkingLevel,
			mcpServers:    mcpServers,
			runtime:       runtime,
		}
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

	runtime, err := a.runtimeFactory(runtimeOpts{
		conn:       a.conn,
		modelRef:   modelRef,
		mcpServers: mcpServers,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create runtime: %v", err)
	}

	s := session{
		cwd:           cwd,
		modelRef:      getSessionResp.ModelRef,
		thinkingLevel: thinkingLevel,
		runtime:       runtime,
	}
	a.activeSessions.Store(sessionId, sync.NewGuard(s))
	return a.configOptions(ctx, sessionId), nil
}

// ResumeSession implements [acp.Agent].
func (a *Agent) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionId, params.Cwd, params.McpServers)
	if err != nil {
		return acp.ResumeSessionResponse{}, fmt.Errorf("could not resume session: %v", err)
	}
	return acp.ResumeSessionResponse{
		ConfigOptions: opts,
	}, nil
}

// LoadSession implements [acp.AgentLoader].
func (a *Agent) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionId, params.Cwd, params.McpServers)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("could not load session: %v", err)
	}
	acpSession, err := a.db.GetACPSession(ctx, params.SessionId)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("could get acp session from db: %v", err)
	}
	dialog, err := storage.GetDialogForMessage(ctx, a.db, acpSession.LastMessageID)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("could not get dialog from db: %v", err)
	}
	for len(dialog) > 0 {
		compactionParentID, _ := dialog[0].ExtraFields[storage.MessageCompactionParentIDKey].(string)
		if compactionParentID == "" {
			break
		}
		parentDialog, err := storage.GetDialogForMessage(ctx, a.db, compactionParentID)
		if err != nil {
			return acp.LoadSessionResponse{}, fmt.Errorf("could not get compaction parent dialog from db: %v", err)
		}
		dialog = append(parentDialog, dialog...)
	}
	for _, msg := range dialog {
		for update := range xacp.MsgToSessionUpdate(msg) {
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: params.SessionId,
				Update:    update,
			}); err != nil {
				return acp.LoadSessionResponse{}, fmt.Errorf("could not send session update: %v", err)
			}
		}
	}
	return acp.LoadSessionResponse{
		ConfigOptions: opts,
	}, nil
}

// UnstableForkSession implements ACP's unstable session/fork method.
//
// The forked session shares the source session's persisted history: the new
// session points at the same last message, so prompts on either session
// diverge as separate branches of the message tree without copying rows.
// Stale stored config is resolved against the loaded config the same way
// session resumption does. The forked session's runtime is created lazily on
// the first prompt, like sessions created via session/new.
func (a *Agent) UnstableForkSession(
	ctx context.Context,
	params acp.UnstableForkSessionRequest,
) (acp.UnstableForkSessionResponse, error) {
	src, err := a.db.GetACPSession(ctx, params.SessionId)
	if err != nil {
		return acp.UnstableForkSessionResponse{}, fmt.Errorf(
			"could not fetch acp session %s from storage: %v",
			params.SessionId,
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
			SessionId: id,
		},
		LastMessageID: src.LastMessageID,
		ModelRef:      modelRef,
		ThinkingLevel: thinkingLevel,
	}); err != nil {
		return acp.UnstableForkSessionResponse{}, fmt.Errorf("could not save forked session: %v", err)
	}

	a.activeSessions.Store(id, sync.NewGuard(session{
		cwd:           params.Cwd,
		modelRef:      modelRef,
		thinkingLevel: thinkingLevel,
		mcpServers:    stableMCPServers(params.McpServers),
	}))

	return acp.UnstableForkSessionResponse{
		SessionId:     id,
		ConfigOptions: unstableConfigOptions(a.configOptions(ctx, id)),
	}, nil
}

// stableMCPServers converts the unstable MCP server descriptors used by
// session/fork into their stable equivalents. The variants carry identical
// fields; only the Go types differ.
func stableMCPServers(servers []acp.UnstableMcpServer) []acp.McpServer {
	if len(servers) == 0 {
		return nil
	}
	converted := make([]acp.McpServer, len(servers))
	for i, s := range servers {
		converted[i] = acp.McpServer{
			Http:  (*acp.McpServerHttpInline)(s.Http),
			Sse:   (*acp.McpServerSseInline)(s.Sse),
			Stdio: s.Stdio,
		}
		if s.Acp != nil {
			converted[i].Acp = &acp.McpServerAcpInline{
				Meta: s.Acp.Meta,
				Id:   acp.McpServerAcpId(s.Acp.Id),
				Name: s.Acp.Name,
				Type: s.Acp.Type,
			}
		}
	}
	return converted
}

// unstableConfigOptions converts session config options into the unstable
// wrapper type used by unstable session responses such as session/fork.
func unstableConfigOptions(opts []acp.SessionConfigOption) []acp.UnstableSessionConfigOption {
	if len(opts) == 0 {
		return nil
	}
	converted := make([]acp.UnstableSessionConfigOption, len(opts))
	for i, o := range opts {
		converted[i] = acp.UnstableSessionConfigOption{
			Select:  (*acp.UnstableSessionConfigOptionSelect)(o.Select),
			Boolean: (*acp.UnstableSessionConfigOptionBoolean)(o.Boolean),
		}
	}
	return converted
}

// Cancel implements [acp.Agent].
func (a *Agent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	s, ok := a.activeSessions.Load(params.SessionId)
	if !ok {
		return fmt.Errorf("session %s not found", params.SessionId)
	}
	return s.Do(func(t *session) error {
		if t.cancelfunc != nil {
			t.cancelfunc()
			t.cancelfunc = nil
		}
		return nil
	})
}

// CloseSession implements [acp.Agent].
func (a *Agent) CloseSession(ctx context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	s, err := a.activeSession(params.SessionId)
	if err != nil {
		return acp.CloseSessionResponse{}, err
	}
	if err := s.Do(func(t *session) error {
		if t.runtime == nil {
			return nil
		}
		return t.runtime.Close()
	}); err != nil {
		return acp.CloseSessionResponse{}, fmt.Errorf("could not close session %s, %v", params.SessionId, err)
	}
	a.activeSessions.Delete(params.SessionId)
	return acp.CloseSessionResponse{}, nil
}

// UnstableDeleteSession implements ACP's unstable session/delete method.
func (a *Agent) UnstableDeleteSession(
	ctx context.Context,
	params acp.UnstableDeleteSessionRequest,
) (acp.UnstableDeleteSessionResponse, error) {
	if s, ok := a.activeSessions.Load(params.SessionId); ok {
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
			return acp.UnstableDeleteSessionResponse{}, fmt.Errorf("could not close session %s before delete: %v", params.SessionId, err)
		}
	}
	if err := a.db.DeleteACPSession(ctx, params.SessionId); err != nil {
		return acp.UnstableDeleteSessionResponse{}, fmt.Errorf("could not delete session %s: %v", params.SessionId, err)
	}
	a.activeSessions.Delete(params.SessionId)
	return acp.UnstableDeleteSessionResponse{}, nil
}
