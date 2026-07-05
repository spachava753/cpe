package acp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xacp"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
	"github.com/spachava753/cpe/internal/version"
)

// runtimeFactory is the type to represent a factory that will
// construct the runtime that actually executes the agent loop
// on call the [Agent.Prompt]
type RuntimeCreator interface {
	Create(context.Context, session, acp.ClientCapabilities) (runtime, error)
}

// Agent implements CPE's ACP initialize, auth, logout, and session handlers.
//
// TODO: how should we split the functionality between
// Agent and session. Currently we operate under the assumption
// that sessions may be created concurrently, and that
// sessions may also be mutated concurrently.
type Agent struct {
	// conn is used to send updates to the client
	conn *acp.AgentConnection
	// clientCaps stores the client capabilities advertised during initialize.
	clientCaps acp.ClientCapabilities
	// activeSessions represents the active sessions for this process.
	// Note that multiple processes can be accessing the same database, and each
	// process can manage its own active sessions. On creation of a new session,
	// session resumption, or loading a session, an active session is created.
	activeSessions *sync.Map[acp.SessionId, *sync.Guard[session]]
	// genId is a factory function to create session ids
	genId func() acp.SessionId
	// runtimeFactory is a factory function to create runtimes for session execution
	runtimeFactory RuntimeCreator
	// rawCfg is the raw config loaded, used for model picking at the beginning of a new session
	rawCfg *config.RawConfig
	// skillHomeDir overrides the user home directory for skill discovery in tests.
	skillHomeDir string
	// db represents the API surface needed for persistent session management
	db interface {
		storage.ACPSessionCreator
		storage.ACPSessionDeleter
		storage.ACPSessionGetter
		storage.ACPSessionsLister
		storage.MessagesGetter
		storage.ACPSessionMessageAdder
		storage.ACPSessionModelSetter
		storage.ACPSessionThinkingLevelSetter
	}
}

// Authenticate implements [acp.AuthenticateHandler].
//
// Support logging into Chat GPT subscription, and other subscription accounts is possible
func (a *Agent) Authenticate(
	ctx context.Context,
	params *acp.AuthenticateRequest,
) (*acp.AuthenticateResponse, error) {
	return &acp.AuthenticateResponse{}, nil
}

// Logout implements [acp.LogoutHandler].
func (a *Agent) Logout(ctx context.Context, params *acp.LogoutRequest) (*acp.LogoutResponse, error) {
	return &acp.LogoutResponse{}, nil
}

// Initialize implements [acp.InitializeHandler].
func (a *Agent) Initialize(
	ctx context.Context,
	params *acp.InitializeRequest,
) (*acp.InitializeResponse, error) {
	resp := &acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion(1),
		AgentInfo: &acp.Implementation{
			Name:    "CPE",
			Title:   new("CPE"),
			Version: version.Get(),
		},
		AgentCapabilities: &acp.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: &acp.McpCapabilities{
				Acp:  false,
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: &acp.PromptCapabilities{
				Audio:           true,
				EmbeddedContext: true,
				Image:           true,
			},
			SessionCapabilities: &acp.SessionCapabilities{
				List:   &acp.SessionListCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
				Close:  &acp.SessionCloseCapabilities{},
				Delete: &acp.SessionDeleteCapabilities{},
				Fork:   &acp.SessionForkCapabilities{},
			},
		},
	}

	if params != nil && params.ClientCapabilities != nil {
		a.clientCaps = *params.ClientCapabilities
	} else {
		a.clientCaps = acp.ClientCapabilities{}
	}
	return resp, nil
}

// Prompt implements [acp.SessionHandler].
//
// TODO: add synctest type test for testing concurrency semantics
func (a *Agent) Prompt(
	ctx context.Context,
	params *acp.PromptRequest,
) (*acp.PromptResponse, error) {
	s, err := a.activeSession(params.SessionID)
	if err != nil {
		return nil, err
	}

	cancelCtx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	cancelled := func(err error) bool {
		return errors.Is(err, context.Canceled) || errors.Is(cancelCtx.Err(), context.Canceled)
	}
	cancelledResponse := &acp.PromptResponse{StopReason: acp.StopReasonCancelled}

	if err := s.Do(func(t *session) error {
		if t.cancelfunc != nil {
			return errors.New("cannot do prompt turn in actively generating session")
		}
		if t.model == "" {
			return errors.New("cannot prompt before selecting a model")
		}
		t.cancelfunc = cancelFunc
		return nil
	}); err != nil {
		return nil, err
	}
	defer func() {
		_ = s.Do(func(t *session) error {
			t.cancelfunc = nil
			return nil
		})
	}()

	if err := a.refreshAvailableSkillCommands(cancelCtx, params.SessionID, s); err != nil {
		if cancelled(err) {
			return cancelledResponse, nil
		}
		return nil, fmt.Errorf("could not refresh available skill commands: %v", err)
	}

	var (
		runtime         runtime
		sessionSnapshot session
		genOpts         *gai.GenOpts
	)
	if err := s.Do(func(t *session) error {
		runtime = t.runtime
		sessionSnapshot = *t
		// Per-turn opts carry only ACP session overrides (thinking level).
		// The runtime's Loop.Generate layers these over the model profile's
		// generation parameters from the resolved config.
		if t.thinking != "" {
			genOpts = &gai.GenOpts{ThinkingBudget: t.thinking}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	acpSession, err := a.db.GetACPSession(cancelCtx, params.SessionID)
	if err != nil {
		if cancelled(err) {
			return cancelledResponse, nil
		}
		return nil, fmt.Errorf("could not get acp session from db: %v", err)
	}
	var dialog gai.Dialog
	if acpSession.LastMessageID != "" {
		dialog, err = storage.GetDialogForMessage(cancelCtx, a.db, acpSession.LastMessageID)
		if err != nil {
			if cancelled(err) {
				return cancelledResponse, nil
			}
			return nil, fmt.Errorf("could not get dialog from db: %v", err)
		}
	}
	expandedPrompt := expandSkillSlashCommands(params.Prompt, sessionSnapshot.skillCatalog)
	dialog = append(dialog, xacp.PromptToMessage(expandedPrompt))

	if runtime == nil {
		runtime, err = a.runtimeFactory.Create(cancelCtx, sessionSnapshot, a.clientCaps)
		if err != nil {
			if cancelled(err) {
				return cancelledResponse, nil
			}
			return nil, fmt.Errorf("could not create runtime: %v", err)
		}
		if err := cancelCtx.Err(); err != nil {
			_ = runtime.Close()
			return cancelledResponse, nil
		}
		if err := s.Do(func(t *session) error {
			if t.cancelfunc == nil {
				return context.Canceled
			}
			t.runtime = runtime
			return nil
		}); err != nil {
			_ = runtime.Close()
			if cancelled(err) {
				return cancelledResponse, nil
			}
			return nil, err
		}
	}

	inputLen := len(dialog)
	generatedDialog, err := runtime.Generate(
		withSessionID(cancelCtx, params.SessionID),
		dialog,
		genOpts,
	)
	result := struct {
		dialog   gai.Dialog
		inputLen int
		err      error
	}{dialog: generatedDialog, inputLen: inputLen, err: err}

	dialog = result.dialog
	if len(dialog) == 0 {
		if cancelled(result.err) {
			return cancelledResponse, nil
		}
		return nil, errors.New("cannot persist empty prompt dialog")
	}

	// Persist even if the prompt context was cancelled, while still bounding how
	// long the client waits for session bookkeeping.
	acpCtx, acpCtxCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer acpCtxCancel()
	lastMessageID := storage.GetMessageID(dialog[len(dialog)-1])
	_, err = a.db.AddACPSessionMessage(acpCtx, params.SessionID, lastMessageID)
	if err != nil {
		return nil, fmt.Errorf("cannot update acp session in db: %v", err)
	}

	// Compaction can replace the input history with a shorter rebased dialog.
	// In that case, the returned dialog is the only safe turn boundary we have.
	usageDialog := dialog
	if result.inputLen >= 0 && result.inputLen <= len(dialog) {
		usageDialog = dialog[result.inputLen:]
	}
	usage := xacp.PromptTurnUsage(usageDialog)
	if result.err != nil {
		if errors.Is(result.err, gai.ErrMaxGenerationLimit) {
			return &acp.PromptResponse{
				StopReason: acp.StopReasonMaxTokens,
				Usage:      usage,
			}, nil
		}
		if _, ok := errors.AsType[gai.ContentPolicyErr](result.err); ok {
			return &acp.PromptResponse{
				StopReason: acp.StopReasonRefusal,
				Usage:      usage,
			}, nil
		}
		if _, ok := errors.AsType[*gai.ContentPolicyErr](result.err); ok {
			return &acp.PromptResponse{
				StopReason: acp.StopReasonRefusal,
				Usage:      usage,
			}, nil
		}
		if cancelled(result.err) {
			return &acp.PromptResponse{
				StopReason: acp.StopReasonCancelled,
				Usage:      usage,
			}, nil
		}
		return nil, fmt.Errorf("unknown error while generating: %v", result.err)
	}

	return &acp.PromptResponse{
		StopReason: acp.StopReasonEndTurn,
		Usage:      usage,
	}, nil
}
