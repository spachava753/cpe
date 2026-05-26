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

const (
	modelRef acp.SessionConfigId = "modelRef"
)

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
	activeSessions *sync.Map[acp.SessionId, sync.Guard[session]]
	// genId is a factory function to create session ids
	genId func() acp.SessionId
	// runtimeFactory is a factory function to create runtimes for session execution
	runtimeFactory func(modelRef string) (acpRuntime, error)
	// rawCfg is the raw config loaded, used for model picking at the beginning of a new session
	rawCfg *config.RawConfig
	// db represents the API surface needed for persistent session management
	db interface {
		storage.ACPSessionCreator
		storage.ACPSessionGetter
		storage.ACPSessionsLister
		storage.MessagesGetter
		storage.ACPSessionMessageAdder
		storage.ACPSessionModelSetter
	}

	// client capabilities we care about
	readFsTool  bool
	writeFsTool bool
}

// Authenticate implements [acp.Agent].
func (a *Agent) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

// Initialize implements [acp.Agent].
func (a *Agent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
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
			// TODO: eventually support loading mcp from acp connection
			McpCapabilities: acp.McpCapabilities{
				Acp:  false,
				Http: false,
				Sse:  false,
			},
			PromptCapabilities: acp.PromptCapabilities{
				Audio:           true,
				EmbeddedContext: false, // TODO: eventually support loading embedded context
				Image:           true,
			},
			SessionCapabilities: acp.SessionCapabilities{
				List:   &acp.SessionListCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
			},
		},
	}, nil
}

// Prompt implements [acp.Agent].
//
// TODO: add support for return usage.
// TODO: add synctest type test for testing concurrency semantics
func (a *Agent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	s, ok := a.activeSessions.Load(params.SessionId)
	if !ok {
		panic(fmt.Sprintf("unknown session: %s", params.SessionId)) // TODO: should we panic or return error?
	}
	cancelCtx, cancelFunc := context.WithCancel(ctx)

	// we launch a go routine within the gaurd because we don't want to hold onto the active session the entire time we are generating. Otherwise, when [Agent.Cancel] is called, and it tries to lock the gaurd to call the cancellation function, it can't because the gaurd is held by this prompt call here
	type generateResult struct {
		dialog gai.Dialog
		err    error
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
			runtime, err := a.runtimeFactory(t.modelRef)
			if err != nil {
				return fmt.Errorf("could not create runtime: %v", err)
			}
			t.runtime = runtime
		}
		runtime := t.runtime
		inputDialog := dialog
		t.cancelfunc = cancelFunc
		go func() {
			generatedDialog, err := runtime.Generate(cancelCtx, inputDialog, nil)
			resultChan <- generateResult{dialog: generatedDialog, err: err}
		}()
		return nil
	}); err != nil {
		return acp.PromptResponse{}, err
	}

	// here we wait until the result is given, which could be because the context was cancelled
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
	_, err := a.db.AddACPSessionMessage(acpCtx, params.SessionId, lastMessageID)
	if err != nil {
		return acp.PromptResponse{}, fmt.Errorf("cannot update acp session in db: %v", err)
	}

	if result.err != nil {
		if errors.Is(result.err, gai.ErrMaxGenerationLimit) {
			return acp.PromptResponse{
				StopReason: acp.StopReasonMaxTokens,
			}, nil
		}
		if _, ok := errors.AsType[*gai.ContentPolicyErr](result.err); ok {
			return acp.PromptResponse{
				StopReason: acp.StopReasonRefusal,
			}, nil
		}
		if errors.Is(result.err, context.Canceled) {
			return acp.PromptResponse{
				StopReason: acp.StopReasonCancelled,
			}, nil
		}
		return acp.PromptResponse{}, fmt.Errorf("unknown error while generating: %v", result.err)
	}

	return acp.PromptResponse{
		StopReason:    acp.StopReasonEndTurn,
		UserMessageId: params.MessageId,
	}, nil
}

func (a *Agent) promptToMessage(contentBlocks []acp.ContentBlock) gai.Message {
	msg := gai.Message{
		Role:   gai.User,
		Blocks: make([]gai.Block, 0, len(contentBlocks)),
	}
	for _, contentBlock := range contentBlocks {
		var block gai.Block
		switch {
		case contentBlock.Text != nil:
			block = gai.TextBlock(contentBlock.Text.Text)
		case contentBlock.Image != nil:
			block = gai.ImageBlock([]byte(contentBlock.Image.Data), contentBlock.Image.MimeType)
		case contentBlock.Audio != nil:
			block = gai.AudioBlock([]byte(contentBlock.Audio.Data), contentBlock.Audio.MimeType)
		case contentBlock.ResourceLink != nil: // TODO: support resource links better
			block = gai.TextBlock(fmt.Sprintf("Resource %s: %s", contentBlock.ResourceLink.Name, contentBlock.ResourceLink.Uri))
		case contentBlock.Resource != nil: // TODO: support embedded resources better
			resource := contentBlock.Resource.Resource
			if resource.TextResourceContents != nil {
				block = gai.TextBlock(resource.TextResourceContents.Text)
			}
			if resource.BlobResourceContents != nil {
				block = gai.TextBlock(fmt.Sprintf("Resource %s: %s", resource.BlobResourceContents.Uri, resource.BlobResourceContents.Blob))
			}
		}
		msg.Blocks = append(msg.Blocks, block)
	}
	return msg
}

var _ acp.Agent = (*Agent)(nil)
var _ acp.AgentLoader = (*Agent)(nil)
