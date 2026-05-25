package acp

import (
	"context"
	"fmt"

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
	conn *acp.AgentSideConnection
	// activeSessions represents the active sessions for this process.
	// Note that multiple processes can be accessing the same database, and each
	// process can manage its own active sessions. On creation of a new session,
	// session resumption, or loading a session, an active session is created.
	activeSessions *sync.Map[acp.SessionId, sync.Guard[session]]
	// genId is a factory function to create session ids
	genId func() acp.SessionId
	// runtimeGen is a factory function to create runtimes for session execution
	runtimeGen func(modelRef string) acpRuntime
	rawCfg     config.RawConfig
	store      *storage.Sqlite
	db         interface {
		storage.ACPSessionCreator
		storage.ACPSessionGetter
		storage.ACPSessionsLister
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
				Close:  &acp.SessionCloseCapabilities{},
				List:   &acp.SessionListCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
			},
		},
	}, nil
}

// Prompt implements [acp.Agent].
func (a *Agent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	s, ok := a.activeSessions.Load(params.SessionId)
	if !ok {
		panic(fmt.Sprintf("unknown session: %s", params.SessionId)) // TODO: should we panic or return error?
	}
	_ = s
	panic("unimplemented")
}

var _ acp.Agent = (*Agent)(nil)
var _ acp.AgentLoader = (*Agent)(nil)
