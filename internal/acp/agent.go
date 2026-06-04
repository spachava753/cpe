package acp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
	"github.com/spachava753/cpe/internal/version"

	"github.com/coder/acp-go-sdk"
)

// runtimeFactory is the type to represent a factory that will
// construct the runtime that actually executes the agent loop
// on call the [Agent.Prompt]
type runtimeFactory func(
	conn *acp.AgentSideConnection,
	modelRef string,
	mcpServers []acp.McpServer,
) (acpRuntime, error)

// Agent is an implementation of an [acp.Agent]
//
// TODO: how should we split the functionality between
// Agent and session. Currently we operate under the assumption
// that sessions may be created concurrently, and that
// sessions may also be mutated concurrently.
type Agent struct {
	// conn is used to send updates to the client
	conn *acp.AgentSideConnection
	// activeSessions represents the active sessions for this process.
	// Note that multiple processes can be accessing the same database, and each
	// process can manage its own active sessions. On creation of a new session,
	// session resumption, or loading a session, an active session is created.
	activeSessions *sync.Map[acp.SessionId, *sync.Guard[session]]
	// genId is a factory function to create session ids
	genId func() acp.SessionId
	// runtimeFactory is a factory function to create runtimes for session execution
	runtimeFactory runtimeFactory
	// rawCfg is the raw config loaded, used for model picking at the beginning of a new session
	rawCfg *config.RawConfig
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

	// client capabilities we care about
	readFsTool  bool
	writeFsTool bool
}

// Authenticate implements [acp.Agent].
func (a *Agent) Authenticate(
	ctx context.Context,
	params acp.AuthenticateRequest,
) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

// Logout implements [acp.Agent].
func (a *Agent) Logout(ctx context.Context, params acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}

// Initialize implements [acp.Agent].
func (a *Agent) Initialize(
	ctx context.Context,
	params acp.InitializeRequest,
) (acp.InitializeResponse, error) {
	a.readFsTool = params.ClientCapabilities.Fs.ReadTextFile
	a.writeFsTool = params.ClientCapabilities.Fs.WriteTextFile
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentInfo: &acp.Implementation{
			Name:    "CPE",
			Title:   new("CPE"),
			Version: version.Get(),
		},
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acp.McpCapabilities{
				Acp:  false,
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: acp.PromptCapabilities{
				Audio:           true,
				EmbeddedContext: false, // TODO: eventually support loading embedded context
				Image:           true,
			},
			SessionCapabilities: acp.SessionCapabilities{
				List:   &acp.SessionListCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
				Close:  &acp.SessionCloseCapabilities{},
				Delete: &acp.SessionDeleteCapabilities{},
			},
		},
	}, nil
}

// Prompt implements [acp.Agent].
//
// TODO: add synctest type test for testing concurrency semantics
func (a *Agent) Prompt(
	ctx context.Context,
	params acp.PromptRequest,
) (acp.PromptResponse, error) {
	s, err := a.activeSession(params.SessionId)
	if err != nil {
		return acp.PromptResponse{}, err
	}

	if err := s.Do(func(t *session) error {
		if t.runtime != nil {
			return nil
		}
		var err error
		t.runtime, err = a.runtimeFactory(a.conn, t.modelRef, t.mcpServers)
		return err
	}); err != nil {
		return acp.PromptResponse{}, fmt.Errorf("failed to create runtime: %v", err)
	}

	cancelCtx, cancelFunc := context.WithCancel(ctx)

	// we launch a go routine within the gaurd because we don't want
	// to hold onto the active session the entire time we are generating.
	// Otherwise, when [Agent.Cancel] is called, and it tries to lock the
	// gaurd to call the cancellation function, it can't because the gaurd
	// is held by this prompt call here
	type generateResult struct {
		dialog   gai.Dialog
		inputLen int
		err      error
	}
	resultChan := make(chan generateResult)
	if err := s.Do(func(t *session) error {
		if t.cancelfunc != nil {
			return errors.New("cannot do prompt turn in actively generating session")
		}
		acpSession, err := a.db.GetACPSession(ctx, params.SessionId)
		if err != nil {
			return fmt.Errorf("could not get acp session from db: %v", err)
		}
		var dialog gai.Dialog
		if acpSession.LastMessageID != "" {
			var err error
			dialog, err = storage.GetDialogForMessage(ctx, a.db, acpSession.LastMessageID)
			if err != nil {
				return fmt.Errorf("could not get dialog from db: %v", err)
			}
		}
		dialog = append(dialog, a.promptToMessage(params.Prompt))
		if t.runtime == nil {
			runtime, err := a.runtimeFactory(a.conn, t.modelRef, t.mcpServers)
			if err != nil {
				return fmt.Errorf("could not create runtime: %v", err)
			}
			t.runtime = runtime
		}
		runtime := t.runtime
		inputDialog := dialog
		inputLen := len(inputDialog)
		// TODO: we should set generation opts from the config, and overide with acp config options
		var genOpts *gai.GenOpts
		if t.thinkingLevel != "" {
			genOpts = &gai.GenOpts{ThinkingBudget: t.thinkingLevel}
		}
		t.cancelfunc = cancelFunc
		go func() {
			generatedDialog, err := runtime.Generate(
				withSessionID(cancelCtx, params.SessionId),
				inputDialog,
				genOpts,
			)
			resultChan <- generateResult{dialog: generatedDialog, inputLen: inputLen, err: err}
		}()
		return nil
	}); err != nil {
		return acp.PromptResponse{}, err
	}

	// here we wait until the result is given, which could be because the
	// context was cancelled
	// TODO: THIS ACTUALLY HANGS THE ENTIRE ACP SERVER, since we aren't
	// listening for context for cancellation
	result := <-resultChan

	dialog := result.dialog
	if len(dialog) == 0 {
		return acp.PromptResponse{}, errors.New("cannot persist empty prompt dialog")
	}

	// we should reset the cancellation func, now that generation is over
	s.Do(func(t *session) error {
		t.cancelfunc = nil
		return nil
	})

	// Persist even if the prompt context was cancelled, while still bounding how
	// long the client waits for session bookkeeping.
	acpCtx, acpCtxCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer acpCtxCancel()
	lastMessageID := storage.GetMessageID(dialog[len(dialog)-1])
	_, err = a.db.AddACPSessionMessage(acpCtx, params.SessionId, lastMessageID)
	if err != nil {
		return acp.PromptResponse{}, fmt.Errorf("cannot update acp session in db: %v", err)
	}

	usage := promptTurnUsage(promptTurnDialog(dialog, result.inputLen))
	if result.err != nil {
		if errors.Is(result.err, gai.ErrMaxGenerationLimit) {
			return acp.PromptResponse{
				StopReason: acp.StopReasonMaxTokens,
				Usage:      usage,
			}, nil
		}
		if _, ok := errors.AsType[*gai.ContentPolicyErr](result.err); ok {
			return acp.PromptResponse{
				StopReason: acp.StopReasonRefusal,
				Usage:      usage,
			}, nil
		}
		if errors.Is(result.err, context.Canceled) {
			return acp.PromptResponse{
				StopReason: acp.StopReasonCancelled,
				Usage:      usage,
			}, nil
		}
		return acp.PromptResponse{}, fmt.Errorf("unknown error while generating: %v", result.err)
	}

	return acp.PromptResponse{
		StopReason:    acp.StopReasonEndTurn,
		Usage:         usage,
		UserMessageId: params.MessageId,
	}, nil
}

var _ acp.Agent = (*Agent)(nil)
var _ acp.AgentLoader = (*Agent)(nil)
