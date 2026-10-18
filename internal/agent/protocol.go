package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/version"
)

const defaultReasoning = "default"

// Initialize negotiates the implemented ACP capabilities.
func (s *Server) Initialize(_ context.Context, request *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized || s.closed {
		return nil, errors.New("connection already initialized or closed")
	}
	if request == nil || request.ProtocolVersion != 1 {
		return nil, errors.New("only ACP protocol version 1 is supported")
	}
	s.initialized = true
	if caps := request.ClientCapabilities; caps != nil && caps.Session != nil {
		s.notices = caps.Session.Notices != nil
	}
	openai := acp.TerminalAuthMethod(
		"openai",
		"Sign in with ChatGPT",
	)
	openai.Args = []string{
		"auth",
		"login",
		"openai",
	}
	opencode := acp.TerminalAuthMethod(
		"opencode",
		"Sign in with OpenCode Go",
	)
	opencode.Args = []string{
		"auth",
		"login",
		"opencode",
	}
	return &acp.InitializeResponse{
		ProtocolVersion: 1,
		AgentInfo: &acp.Implementation{
			Name:    "cpe",
			Version: version.Get(),
		},
		AuthMethods: []acp.AuthMethod{
			openai,
			opencode,
		},
		AgentCapabilities: &acp.AgentCapabilities{
			LoadSession: true,
			Auth: &acp.AgentAuthCapabilities{
				Logout: &acp.LogoutCapabilities{},
			},
			McpCapabilities: &acp.McpCapabilities{
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: &acp.PromptCapabilities{
				Image: true,
			},
			SessionCapabilities: &acp.SessionCapabilities{
				Close:  &acp.SessionCloseCapabilities{},
				Delete: &acp.SessionDeleteCapabilities{},
				Fork:   &acp.SessionForkCapabilities{},
				List:   &acp.SessionListCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
			},
		},
	}, nil
}

// Authenticate verifies credentials established by the terminal login flow.
func (s *Server) Authenticate(ctx context.Context, request *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	provider, err := auth.ParseProvider(string(request.MethodID))
	if err != nil {
		return nil, err
	}
	if err := s.deps.Authentication.Authenticate(
		ctx,
		provider,
	); err != nil {
		return nil, err
	}
	return &acp.AuthenticateResponse{}, nil
}

// Logout clears locally stored subscription credentials.
func (s *Server) Logout(ctx context.Context, _ *acp.LogoutRequest) (*acp.LogoutResponse, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.deps.Authentication.Logout(ctx); err != nil {
		return nil, err
	}
	return &acp.LogoutResponse{}, nil
}

// NewSession creates isolated execution state and an initial saved checkpoint.
func (s *Server) NewSession(ctx context.Context, request *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	spec, err := parseSessionSpec(
		SessionID(rand.Text()),
		request.Cwd,
		request.McpServers,
		request.AdditionalDirectories,
	)
	if err != nil {
		return nil, err
	}
	live, err := s.openSession(
		ctx,
		spec,
		SessionRecord{
			Info: SessionInfo{
				ID:        spec.id,
				Cwd:       spec.cwd,
				UpdatedAt: time.Now().UTC(),
			},
			Options: s.config.Initial,
		},
	)
	if err != nil {
		return nil, err
	}
	if err := live.announce(ctx); err != nil {
		return nil, err
	}
	options := configOptions(
		s.config,
		live.record.Options,
	)
	return &acp.NewSessionResponse{
		SessionID:     acp.SessionId(spec.id),
		ConfigOptions: &options,
	}, nil
}

// Prompt runs one turn; different sessions may execute concurrently.
func (s *Server) Prompt(ctx context.Context, request *acp.PromptRequest) (*acp.PromptResponse, error) {
	live, err := s.findSession(SessionID(request.SessionID))
	if err != nil {
		return nil, err
	}
	message, err := parseContent(request.Prompt)
	if err != nil {
		return nil, err
	}
	return live.prompt(
		ctx,
		message,
	)
}

// Cancel stops the active turn without discarding session state.
func (s *Server) Cancel(_ context.Context, request *acp.CancelNotification) error {
	live, err := s.findSession(SessionID(request.SessionID))
	if err != nil {
		return err
	}
	live.cancel()
	return nil
}

// CloseSession cancels and joins execution, retaining saved conversation history.
func (s *Server) CloseSession(ctx context.Context, request *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if err := s.closeSession(
		ctx,
		SessionID(request.SessionID),
	); err != nil {
		return nil, err
	}
	return &acp.CloseSessionResponse{}, nil
}

func (s *Server) closeSession(ctx context.Context, id SessionID) error {
	live, err := s.findSession(id)
	if err != nil {
		return err
	}
	err = live.close(ctx)
	s.mu.Lock()
	delete(
		s.sessions,
		id,
	)
	s.mu.Unlock()
	return err
}

// DeleteSession closes live resources before removing the saved checkpoint.
func (s *Server) DeleteSession(ctx context.Context, request *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if err := s.ready(); err != nil {
		return nil, err
	}
	id := SessionID(request.SessionID)
	s.mu.Lock()
	live := s.sessions[id]
	s.mu.Unlock()
	if live != nil {
		if err := s.closeSession(
			ctx,
			id,
		); err != nil {
			return nil, err
		}
	}
	if err := s.deps.History.Delete(
		ctx,
		id,
	); err != nil {
		return nil, err
	}
	return &acp.DeleteSessionResponse{}, nil
}

// LoadSession restores fresh resources and replays conversation content to the client.
func (s *Server) LoadSession(ctx context.Context, request *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	live, err := s.restore(
		ctx,
		request.SessionID,
		request.Cwd,
		request.McpServers,
		request.AdditionalDirectories,
	)
	if err != nil {
		return nil, err
	}
	if err := live.client.replay(
		ctx,
		live.record,
	); err != nil {
		return nil, err
	}
	options := configOptions(
		s.config,
		live.record.Options,
	)
	return &acp.LoadSessionResponse{
		ConfigOptions: &options,
	}, nil
}

// ResumeSession restores fresh resources without replaying history.
func (s *Server) ResumeSession(ctx context.Context, request *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	live, err := s.restore(
		ctx,
		request.SessionID,
		request.Cwd,
		request.McpServers,
		request.AdditionalDirectories,
	)
	if err != nil {
		return nil, err
	}
	options := configOptions(
		s.config,
		live.record.Options,
	)
	return &acp.ResumeSessionResponse{
		ConfigOptions: &options,
	}, nil
}

func (s *Server) restore(ctx context.Context, id acp.SessionId, cwd string, servers []acp.McpServer, additional []string) (*session, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	spec, err := parseSessionSpec(
		SessionID(id),
		cwd,
		servers,
		additional,
	)
	if err != nil {
		return nil, err
	}
	record, err := s.deps.History.Load(
		ctx,
		spec.id,
	)
	if err != nil {
		return nil, err
	}
	live, err := s.openSession(
		ctx,
		spec,
		record,
	)
	if err != nil {
		return nil, err
	}
	if err := live.announce(ctx); err != nil {
		return nil, err
	}
	return live, nil
}

// ForkSession copies an idle conversation into a fresh runtime with a new identifier.
func (s *Server) ForkSession(ctx context.Context, request *acp.ForkSessionRequest) (*acp.ForkSessionResponse, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	source, err := s.findSession(SessionID(request.SessionID))
	if err != nil {
		return nil, err
	}
	record, err := source.snapshot()
	if err != nil {
		return nil, err
	}
	spec, err := parseSessionSpec(
		SessionID(rand.Text()),
		request.Cwd,
		request.McpServers,
		request.AdditionalDirectories,
	)
	if err != nil {
		return nil, err
	}
	parent := record.Info.ID
	record.Info.ParentID = &parent
	record.Info.UpdatedAt = time.Now().UTC()
	live, err := s.openSession(
		ctx,
		spec,
		record,
	)
	if err != nil {
		return nil, err
	}
	options := configOptions(
		s.config,
		live.record.Options,
	)
	if err := live.announce(ctx); err != nil {
		return nil, err
	}
	return &acp.ForkSessionResponse{
		SessionID:     acp.SessionId(spec.id),
		ConfigOptions: &options,
	}, nil
}

// ListSessions lists stored conversations, including those without a live runtime.
func (s *Server) ListSessions(ctx context.Context, request *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	query := SessionQuery{
		Limit: 100,
	}
	if request.Cwd != nil {
		query.Cwd = *request.Cwd
	}
	if request.Cursor != nil {
		query.Cursor = *request.Cursor
	}
	page, err := s.deps.History.List(
		ctx,
		query,
	)
	if err != nil {
		return nil, err
	}
	response := &acp.ListSessionsResponse{
		Sessions: []acp.SessionInfo{},
	}
	for _, info := range page.Sessions {
		updated := info.UpdatedAt.Format(time.RFC3339Nano)
		response.Sessions = append(
			response.Sessions,
			acp.SessionInfo{
				SessionID: acp.SessionId(info.ID),
				Cwd:       info.Cwd,
				Title:     new(info.Title),
				UpdatedAt: &updated,
			},
		)
	}
	if page.NextCursor != "" {
		response.NextCursor = &page.NextCursor
	}
	return response, nil
}

// SetSessionConfigOption changes model or reasoning effort while the session is idle.
func (s *Server) SetSessionConfigOption(ctx context.Context, request *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	live, err := s.findSession(SessionID(request.SessionID))
	if err != nil {
		return nil, err
	}
	var value string
	switch v := request.Value.(type) {
	case string:
		value = v
	case acp.SessionConfigValueId:
		value = string(v)
	default:
		return nil, errors.New("configuration value must be a string")
	}
	options, err := live.configure(
		ctx,
		string(request.ConfigID),
		value,
	)
	if err != nil {
		return nil, err
	}
	if err := live.client.update(
		ctx,
		acp.ConfigOptionUpdateSessionUpdate(options),
	); err != nil {
		return nil, err
	}
	return &acp.SetSessionConfigOptionResponse{
		ConfigOptions: options,
	}, nil
}

// SetSessionMode reports that CPE uses configuration selectors rather than modes.
func (*Server) SetSessionMode(context.Context, *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return nil, errors.New("session modes are not supported; use model/reasoning configuration")
}

func parseSessionSpec(id SessionID, cwd string, servers []acp.McpServer, additional []string) (sessionSpec, error) {
	spec := sessionSpec{
		id:      id,
		cwd:     filepath.Clean(cwd),
		servers: make(map[MCPServerID]mcpServer),
	}
	if id == "" || !filepath.IsAbs(cwd) {
		return spec, errors.New("session ID and absolute cwd are required")
	}
	if len(additional) != 0 {
		return spec, errors.New("additional directories are not supported")
	}
	for _, server := range servers {
		id := MCPServerID(server.Name)
		if id == "" {
			return spec, errors.New("MCP server name is required")
		}
		if _, exists := spec.servers[id]; exists {
			return spec, errors.New("duplicate MCP server name")
		}
		config := mcpServer{
			Cwd:     spec.cwd,
			Args:    server.Args,
			Env:     make(map[string]string),
			Headers: make(http.Header),
		}
		switch server.Type {
		case "":
			if server.Command == "" {
				return spec, errors.New("MCP command is required")
			}
			config.Type = plugins.Stdio
			config.Command = server.Command
			for _, variable := range server.Env {
				config.Env[variable.Name] = variable.Value
			}
		case acp.McpServerTypeHttp, acp.McpServerTypeSse:
			parsed, err := url.Parse(server.Url)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != httpsScheme) {
				return spec, errors.New("invalid MCP HTTP(S) URL")
			}
			config.URL = *parsed
			config.Type = plugins.StreamableHTTP
			if server.Type == acp.McpServerTypeSse {
				config.Type = plugins.SSE
			}
			for _, header := range server.Headers {
				config.Headers.Set(
					header.Name,
					header.Value,
				)
			}
		default:
			return spec, errors.New("ACP-tunneled MCP is not supported")
		}
		spec.servers[id] = config
	}
	return spec, nil
}

func configOptions(config Config, current SessionOptions) []acp.SessionConfigOption {
	ids := make(
		[]string,
		0,
		len(config.Models),
	)
	for id := range config.Models {
		ids = append(
			ids,
			string(id),
		)
	}
	sort.Strings(ids)
	models := acp.UngroupedSessionConfigSelectOptions{}
	for _, id := range ids {
		models = append(
			models,
			acp.SessionConfigSelectOption{
				Name:  id,
				Value: acp.SessionConfigValueId(id),
			},
		)
	}
	efforts := acp.UngroupedSessionConfigSelectOptions{
		{
			Name:  "Provider default",
			Value: defaultReasoning,
		},
	}
	for _, effort := range config.Models[current.Model].ReasoningLevels {
		if effort != defaultReasoning {
			efforts = append(
				efforts,
				acp.SessionConfigSelectOption{
					Name:  effort,
					Value: acp.SessionConfigValueId(effort),
				},
			)
		}
	}
	effort := acp.SessionConfigValueId(current.Reasoning)
	if effort == "" {
		effort = defaultReasoning
	}
	return []acp.SessionConfigOption{
		acp.SelectSessionConfigOption(
			"model",
			"Model",
			acp.SessionConfigValueId(current.Model),
			acp.SessionConfigSelectOptions{
				Ungrouped: &models,
			},
		),
		acp.SelectSessionConfigOption(
			"reasoning",
			"Reasoning effort",
			effort,
			acp.SessionConfigSelectOptions{
				Ungrouped: &efforts,
			},
		),
	}
}

type sessionClient struct {
	ac      *acp.AgentConnection
	id      SessionID
	sendMu  sync.Mutex
	notices bool
}

func (c *sessionClient) update(ctx context.Context, update acp.SessionUpdate) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.ac.SessionUpdate(
		ctx,
		&acp.SessionNotification{
			SessionID: acp.SessionId(c.id),
			Update:    update,
		},
	)
}

func (c *sessionClient) notice(ctx context.Context, title, description string) error {
	if c.notices {
		update := acp.NoticeSessionUpdate(
			acp.NoticeSeverityWarning,
			title,
		)
		update.Description = &description
		return c.update(
			ctx,
			update,
		)
	}
	// Standard text updates are supported even by clients without advisory notices.
	return c.update(
		ctx,
		acp.AgentMessageChunkSessionUpdate(acp.TextContentBlock("\n["+title+": "+description+"]\n")),
	)
}

func (c *sessionClient) toolResult(ctx context.Context, id string, message gai.Message) error {
	content := make(
		[]acp.ToolCallContent,
		0,
		len(message.Blocks),
	)
	for _, block := range message.Blocks {
		item, err := acpContent(block)
		if err != nil {
			return err
		}
		content = append(
			content,
			acp.ContentToolCallContent(item),
		)
	}
	update := acp.ToolCallUpdateSessionUpdate(acp.ToolCallId(id))
	update.Content = content
	update.Status = new(acp.ToolCallStatusCompleted)
	if message.ToolResultError {
		update.Status = new(acp.ToolCallStatusFailed)
	}
	return c.update(
		ctx,
		update,
	)
}

func (c *sessionClient) replay(ctx context.Context, record SessionRecord) error {
	for _, message := range record.Dialog {
		if message.Role == gai.ToolResult {
			if len(message.Blocks) != 0 {
				if err := c.toolResult(
					ctx,
					message.Blocks[0].ID,
					message,
				); err != nil {
					return err
				}
			}
			continue
		}
		for _, block := range message.Blocks {
			if block.BlockType == gai.ToolCall {
				var call gai.ToolCallInput
				if err := json.Unmarshal(
					[]byte(block.Content.String()),
					&call,
				); err != nil {
					return err
				}
				update := acp.ToolCallSessionUpdate(
					acp.ToolCallId(block.ID),
					call.Name,
				)
				update.RawInput = call.Parameters
				if err := c.update(
					ctx,
					update,
				); err != nil {
					return err
				}
				continue
			}
			content, err := acpContent(block)
			if err != nil {
				return err
			}
			update := acp.AgentMessageChunkSessionUpdate(content)
			if message.Role == gai.User {
				update = acp.UserMessageChunkSessionUpdate(content)
			}
			if block.BlockType == gai.Thinking {
				update = acp.AgentThoughtChunkSessionUpdate(content)
			}
			if err := c.update(
				ctx,
				update,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
