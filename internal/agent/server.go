package agent

import (
	"context"
	"io/fs"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/cpe/internal/version"
	"github.com/spachava753/gai/agent"
)

// skill is a type to represent skills loaded from skill paths or agent plugins
type skill struct {
	name        string
	description string
	path        string
}

// Server implements the ACP server spec, and is meant to be passed to [acp.RunAgent]
//
// TODO: skills will be loaded as slash commands. Skills can be loaded from file system paths, or from agent plugins
type Server struct {
	ac     *acp.AgentConnection
	loop   agent.Agent
	// skills are skills loaded from filesystem or plugins, are used to construct slash commands
	skills []skill
}

// Logout implements [acp.LogoutHandler].
func (s *Server) Logout(context.Context, *acp.LogoutRequest) (*acp.LogoutResponse, error) {
	panic("unimplemented")
}

// Authenticate implements [acp.AuthenticateHandler].
func (s *Server) Authenticate(context.Context, *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	panic("unimplemented")
}

// Initialize implements [acp.InitializeHandler].
func (s *Server) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{
		Meta: acp.Meta{},
		AgentCapabilities: &acp.AgentCapabilities{
			Meta: acp.Meta{},
			Auth: &acp.AgentAuthCapabilities{
				Logout: &acp.LogoutCapabilities{},
			},
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
				AdditionalDirectories: &acp.SessionAdditionalDirectoriesCapabilities{},
				Close:                 &acp.SessionCloseCapabilities{},
				Delete:                &acp.SessionDeleteCapabilities{},
				Fork:                  &acp.SessionForkCapabilities{},
				List:                  &acp.SessionListCapabilities{},
				Resume:                &acp.SessionResumeCapabilities{},
			},
		},
		AgentInfo: &acp.Implementation{
			Name:    "CPE",
			Title:   new("CPE"),
			Version: version.Get(),
		},
		AuthMethods:     []acp.AuthMethod{},
		ProtocolVersion: acp.ProtocolVersion(1),
	}, nil
}

// Cancel implements [acp.SessionHandler].
func (s *Server) Cancel(context.Context, *acp.CancelNotification) error {
	panic("unimplemented")
}

// CloseSession implements [acp.SessionHandler].
func (s *Server) CloseSession(context.Context, *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	panic("unimplemented")
}

// DeleteSession implements [acp.SessionHandler].
func (s *Server) DeleteSession(context.Context, *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	panic("unimplemented")
}

// ForkSession implements [acp.SessionHandler].
func (s *Server) ForkSession(context.Context, *acp.ForkSessionRequest) (*acp.ForkSessionResponse, error) {
	panic("unimplemented")
}

// ListSessions implements [acp.SessionHandler].
func (s *Server) ListSessions(context.Context, *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	panic("unimplemented")
}

// LoadSession implements [acp.SessionHandler].
func (s *Server) LoadSession(context.Context, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	panic("unimplemented")
}

// NewSession implements [acp.SessionHandler].
func (s *Server) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	panic("unimplemented")
}

// Prompt implements [acp.SessionHandler].
func (s *Server) Prompt(context.Context, *acp.PromptRequest) (*acp.PromptResponse, error) {
	panic("unimplemented")
}

// ResumeSession implements [acp.SessionHandler].
func (s *Server) ResumeSession(context.Context, *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	panic("unimplemented")
}

// SetSessionConfigOption implements [acp.SessionHandler].
func (s *Server) SetSessionConfigOption(context.Context, *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	panic("unimplemented")
}

// SetSessionMode implements [acp.SessionHandler].
func (s *Server) SetSessionMode(context.Context, *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	panic("unimplemented")
}

// ServerFactory returns a [acp.AgentFactory] that constructs a [Server].
//
// TODO: need to accept
// - model configs
// - skills path
// - dyson host boundary modules
// - agent plugin loading
func ServerFactory(
	skillsPaths []fs.ReadDirFS,
	pluginsPath []fs.ReadDirFS,
) acp.AgentFactory {
	return func(ac *acp.AgentConnection) any {
		return &Server{
			ac: ac,
		}
	}
}

var (
	_ acp.InitializeHandler   = (*Server)(nil)
	_ acp.AuthenticateHandler = (*Server)(nil)
	_ acp.LogoutHandler       = (*Server)(nil)
	_ acp.SessionHandler      = (*Server)(nil)
)
