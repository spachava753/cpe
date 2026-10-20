package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/terminaltext"
	"github.com/spachava753/cpe/internal/toolruntime"
)

type sessionSpec struct {
	id      conversation.ID
	cwd     string
	servers map[mcpclient.ServerID]plugins.MCPServer
}

type session struct {
	mu                  sync.Mutex
	closed              bool
	active              *turn
	lifetime            context.Context
	config              Config
	deps                Dependencies
	client              *sessionClient
	record              conversation.Record
	catalog             commandCatalog
	projectInstructions string
	runtime             toolruntime.Host
	runtimeReset        *runtimeReset
	mcp                 *mcpclient.Registry
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
	servers, err := catalog.servers(
		spec.servers,
		spec.cwd,
	)
	if err != nil {
		return nil, err
	}
	registry, err := mcpclient.NewRegistry(
		deps.MCP,
		servers,
	)
	if err != nil {
		return nil, err
	}
	host, err := deps.Hosts(
		setup,
		toolruntime.Config{
			Cwd:          spec.cwd,
			MCP:          registry,
			Descriptions: catalog.serverDescriptions(),
		},
	)
	if err != nil {
		return nil, errors.Join(
			err,
			registry.Close(),
		)
	}
	var reset *runtimeReset
	if len(record.Dialog) != 0 {
		reset = &runtimeReset{
			afterMessages: len(record.Dialog),
		}
	}
	record.Info.ID = spec.id
	record.Info.Cwd = spec.cwd
	return &session{
		lifetime:            lifetime,
		config:              config,
		deps:                deps,
		client:              client,
		record:              record,
		catalog:             catalog,
		projectInstructions: projectInstructions,
		runtime:             host,
		runtimeReset:        reset,
		mcp:                 registry,
	}, nil
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
		// Host evaluation must exit before closing a non-concurrent Dyson sphere.
		<-running.done
	}
	return errors.Join(
		s.runtime.Close(ctx),
		s.mcp.Close(),
	)
}

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
			gai.TextBlock("Execution was interrupted; effects may have occurred. Inspect state before retrying."),
		)
		message.ToolResultError = true
		dialog = append(
			dialog,
			message,
		)
	}
	return dialog
}
