package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/terminaltext"
	"github.com/spachava753/cpe/internal/toolruntime"
)

type sessionSpec struct {
	id  conversation.ID
	cwd string
	// servers are the MCP servers the ACP client supplied for this session.
	servers []mcpclient.ServerSpec
}

type session struct {
	record              conversation.Record
	deps                Dependencies
	lifetime            context.Context
	runtime             toolruntime.Host
	active              *turn
	client              *sessionClient
	mcp                 *mcpclient.Servers
	projectInstructions string
	// extensions lists skills, plugins, and MCP servers; it is fixed at setup.
	extensions string
	// tempDir holds tool overflow output and rejected media for this session.
	tempDir string
	catalog commandCatalog
	config  Config
	mu      sync.Mutex
	closed  bool
}

func newSession(setup context.Context, lifetime context.Context, spec sessionSpec, record conversation.Record, config Config, deps Dependencies, client *sessionClient) (*session, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	if err := checkOptions(
		config.Models,
		record.Options,
	); err != nil {
		return nil, err
	}
	info, err := deps.Filesystem.Stat(spec.cwd)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("session cwd must be a directory")
	}
	projectInstructions, err := loadProjectInstructions(
		setup,
		deps.Filesystem,
		spec.cwd,
	)
	if err != nil {
		return nil, err
	}
	catalog, err := loadCommands(
		setup,
		deps.Filesystem,
		spec.cwd,
		config,
	)
	if err != nil {
		return nil, err
	}
	specs, invalid, err := sessionServers(
		catalog,
		config,
		spec.servers,
	)
	if err != nil {
		return nil, err
	}
	prefixes, err := mcpclient.Prefixes(specs)
	if err != nil {
		return nil, err
	}
	servers, failures := mcpclient.Setup(
		setup,
		deps.MCP,
		specs,
		config.MCPTimeout,
	)
	if err = setup.Err(); err != nil {
		servers.Close()
		return nil, err
	}
	for _, failure := range append(
		invalid,
		failures...,
	) {
		// A failing server is skipped; stderr is the only diagnostic an ACP agent has.
		_, _ = fmt.Fprintf(
			deps.Stderr,
			"cpe: skipping MCP server %q: %s\n",
			terminaltext.Sanitize(string(failure.Spec.ID)),
			terminaltext.Sanitize(failure.Err.Error()),
		)
	}
	tools, err := mcpclient.NewCatalog(
		prefixes,
		servers.Connected(),
	)
	if err != nil {
		servers.Close()
		return nil, err
	}
	extensions, err := catalog.extensionInstructions(tools)
	if err != nil {
		servers.Close()
		return nil, err
	}
	tempDir, err := os.MkdirTemp(
		"",
		"cpe-session-",
	)
	if err != nil {
		servers.Close()
		return nil, err
	}
	// Calls without durable results must never be replayed after restoration.
	record.Dialog = settleDialog(record.Dialog)
	record.Info.ID = spec.id
	record.Info.Cwd = spec.cwd
	s := &session{
		lifetime:            lifetime,
		config:              config,
		deps:                deps,
		client:              client,
		record:              record,
		catalog:             catalog,
		projectInstructions: projectInstructions,
		extensions:          extensions,
		tempDir:             tempDir,
		mcp:                 servers,
	}
	host, err := deps.Hosts(
		setup,
		toolruntime.Config{
			Cwd:     spec.cwd,
			TempDir: tempDir,
			Catalog: tools,
		},
	)
	if err != nil {
		servers.Close()
		return nil, err
	}
	s.runtime = host
	return s, nil
}

// sessionServers merges settings, plugin, and client servers. A client server
// whose name clashes with a configured one fails the session so the user learns
// about the overlap. Invalid settings entries are returned to be skipped.
func sessionServers(catalog commandCatalog, config Config, client []mcpclient.ServerSpec) ([]mcpclient.ServerSpec, []mcpclient.Failure, error) {
	settings, invalid := mcpclient.SettingsSpecs(config.MCPServers)
	plugin, unusable, err := catalog.pluginSpecs()
	if err != nil {
		return nil, nil, err
	}
	invalid = append(
		invalid,
		unusable...,
	)
	// Invalid entries still claim their names, so a clash is reported rather
	// than hidden by the configuration error.
	claimed := slices.Clone(settings)
	for _, failure := range invalid {
		claimed = append(
			claimed,
			failure.Spec,
		)
	}
	if _, err = mcpclient.MergeSpecs(
		claimed,
		plugin,
		client,
	); err != nil {
		return nil, nil, err
	}
	specs, err := mcpclient.MergeSpecs(
		settings,
		plugin,
		client,
	)
	return specs, invalid, err
}

func (s *session) announce(ctx context.Context) error {
	for _, warning := range s.catalog.warnings {
		// Diagnostic output failure must not turn a warning into a session failure.
		_, _ = fmt.Fprintf(
			s.deps.Stderr,
			"cpe: plugin warning: %s\n",
			terminaltext.Sanitize(warning.Error()),
		)
	}
	return s.client.update(
		ctx,
		acp.AvailableCommandsUpdateSessionUpdate(s.catalog.available()),
	)
}

// configure changes only idle sessions; a model change leaves history and tools intact.
func (s *session) configure(ctx context.Context, id string, value string) ([]acp.SessionConfigOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil {
		return nil, errors.New("session must be open and idle to change configuration")
	}
	options := s.record.Options
	switch id {
	case "model":
		options.Model = models.ID(value)
		// Reasoning names differ by provider. A model switch resets effort explicitly.
		options.Reasoning = ""
	case "reasoning":
		if value == defaultReasoning {
			value = ""
		}
		options.Reasoning = models.ReasoningLevel(value)
	default:
		return nil, errors.New("unknown session configuration option")
	}
	if err := checkOptions(
		s.config.Models,
		options,
	); err != nil {
		return nil, err
	}
	record := s.record
	record.Options = options
	record.Info.UpdatedAt = time.Now().UTC()
	if err := s.deps.History.Save(
		ctx,
		record,
	); err != nil {
		return nil, err
	}
	s.record = record
	if id == "model" {
		s.rememberModel(
			ctx,
			options.Model,
		)
	}
	return configOptions(
		s.config,
		options,
	), nil
}

func (s *session) cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.active.cancel()
	}
}

func (s *session) snapshot() (conversation.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil {
		return conversation.Record{}, errors.New("session must be open and idle")
	}
	return s.record.Clone()
}

func (s *session) close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	running := s.active
	if running != nil {
		running.cancel()
	}
	s.mu.Unlock()
	if running != nil {
		// Host evaluation must exit before discarding session state and MCP connections.
		<-running.done
	}
	err := s.runtime.Close(ctx)
	s.mcp.Close()
	return err
}

const interruptedToolResult = "Execution interrupted; outcome uncertain. Effects may have occurred. Inspect state before retrying."

// settleDialog records uncertain outcomes rather than executing historical calls again.
func settleDialog(dialog gai.Dialog) gai.Dialog {
	pending := make(map[string]bool)
	for _, message := range dialog {
		for _, block := range message.Blocks {
			if block.BlockType == gai.ToolCall {
				pending[block.ID] = true
			} else if message.Role == gai.ToolResult {
				delete(
					pending,
					block.ID,
				)
			}
		}
	}
	for id := range pending {
		message := gai.ToolResultMessage(
			id,
			gai.TextBlock(interruptedToolResult),
		)
		message.ToolResultError = true
		dialog = append(
			dialog,
			message,
		)
	}
	return dialog
}

func (s *session) rememberModel(ctx context.Context, model models.ID) {
	if err := s.deps.LastModel.Save(
		ctx,
		model,
	); err != nil {
		_, _ = fmt.Fprintf(
			s.deps.Stderr,
			"cpe: could not remember last-used model: %s\n",
			terminaltext.Sanitize(err.Error()),
		)
	}
}

// MCPServers resolves the settings and plugin servers a session started in cwd
// would use, without connecting. Servers supplied by an ACP client are not
// included. Invalid settings entries are returned as failures.
func MCPServers(ctx context.Context, filesystem afero.Fs, cwd string, config Config) ([]mcpclient.ServerSpec, []mcpclient.Failure, error) {
	catalog, err := loadCommands(
		ctx,
		filesystem,
		cwd,
		config,
	)
	if err != nil {
		return nil, nil, err
	}
	return sessionServers(
		catalog,
		config,
		nil,
	)
}
