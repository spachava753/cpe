package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"slices"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"

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
	modelRef      string
	thinkingLevel string
	runtime       acpRuntime
	cancelfunc    context.CancelFunc
	si            acp.SessionInfo
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
	s := session{
		si: si,
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
func (a *Agent) loadActiveSession(ctx context.Context, sessionId acp.SessionId) ([]acp.SessionConfigOption, error) {
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
		runtime, err := a.runtimeFactory(a.conn, modelRef)
		if err != nil {
			return nil, fmt.Errorf("could not create runtime: %v", err)
		}
		s := session{
			si:            getSessionResp.Session,
			modelRef:      modelRef,
			thinkingLevel: thinkingLevel,
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

	runtime, err := a.runtimeFactory(a.conn, modelRef)
	if err != nil {
		return nil, fmt.Errorf("could not create runtime: %v", err)
	}

	s := session{
		si:            getSessionResp.Session,
		modelRef:      getSessionResp.ModelRef,
		thinkingLevel: thinkingLevel,
		runtime:       runtime,
	}
	a.activeSessions.Store(sessionId, sync.NewGuard(s))
	return a.configOptions(ctx, sessionId), nil
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
//
// TODO: need to handle compaction, so when getting a dialog that can be a compaction restart branch, need to retrieve parent dialog of compaction dialog
func (a *Agent) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	opts, err := a.loadActiveSession(ctx, params.SessionId)
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
	for _, msg := range dialog {
		for update := range msgToSessionUpdate(msg) {
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

func msgToSessionUpdate(msg gai.Message) iter.Seq[acp.SessionUpdate] {
	return func(yield func(acp.SessionUpdate) bool) {
		for _, b := range msg.Blocks {
			content := b.Content.String()
			var acpBlock acp.ContentBlock
			switch b.ModalityType {
			case gai.Image:
				acpBlock = acp.ImageBlock(content, b.MimeType)
			case gai.Audio:
				acpBlock = acp.AudioBlock(content, b.MimeType)
			default:
				acpBlock = acp.TextBlock(content)
			}
			switch msg.Role {
			case gai.User:
				if !yield(acp.UpdateUserMessage(acpBlock)) {
					return
				}
			case gai.Assistant:
				switch b.BlockType {
				case gai.Thinking:
					if !yield(acp.UpdateAgentThought(acpBlock)) {
						return
					}
				case gai.ToolCall:
					var input gai.ToolCallInput
					if err := json.Unmarshal([]byte(content), &input); err != nil {
						panic(err)
					}
					// TODO: we should add support for diff content blocks based on calls to text_edit tool
					// TODO: we should add support for tool kind
					// TODO: we should add support for file locations
					if !yield(acp.StartToolCall(
						acp.ToolCallId(b.ID),
						input.Name,
						acp.WithStartStatus(acp.ToolCallStatusPending),
						acp.WithStartRawInput(input.Parameters),
					)) {
						return
					}
				case gai.Content:
					if !yield(acp.UpdateAgentMessage(acpBlock)) {
						return
					}
				default:
					panic(fmt.Sprintf("unknown block type: %s", b.BlockType))
				}
			case gai.ToolResult:
				status := acp.ToolCallStatusCompleted
				if msg.ToolResultError {
					status = acp.ToolCallStatusFailed
				}
				if !yield(acp.UpdateToolCall(
					acp.ToolCallId(b.ID),
					acp.WithUpdateStatus(status),
					acp.WithUpdateContent([]acp.ToolCallContent{
						acp.ToolContent(acpBlock),
					}),
				)) {
					return
				}
			default:
				panic("unknown role")
			}
		}
	}
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
