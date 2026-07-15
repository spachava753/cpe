package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spachava753/acp-sdk/acp"

	cpeacp "github.com/spachava753/cpe/internal/acp"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/version"
)

// Options configures the minimal terminal ACP client used by `cpe "prompt"`.
type Options struct {
	Prompt        string
	RawConfig     *config.RawConfig
	Store         *storage.Sqlite
	ModelRef      string
	ThinkingLevel string
	Cwd           string
	Stdout        io.Writer
	Stderr        io.Writer
}

// Run starts CPE's ACP agent over an in-memory transport, creates a fresh
// session, submits the prompt, and prints session updates to stdout.
func Run(ctx context.Context, opts Options) error {
	return run(ctx, opts, cpeAgentRunner)
}

type agentRunner func(context.Context, acp.Transport, Options) error

func cpeAgentRunner(ctx context.Context, transport acp.Transport, opts Options) error {
	return cpeacp.Run(ctx, transport, cpeacp.RunOptions{
		RawConfig: opts.RawConfig,
		Store:     opts.Store,
		Stderr:    opts.Stderr,
	})
}

func run(ctx context.Context, opts Options, runAgent agentRunner) error {
	if strings.TrimSpace(opts.Prompt) == "" {
		return errors.New("prompt cannot be empty")
	}
	if opts.Stdout == nil {
		return errors.New("provided stdout cannot be nil")
	}
	if opts.Stderr == nil {
		return errors.New("provided stderr cannot be nil")
	}
	if opts.Cwd == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not resolve working directory: %w", err)
		}
		opts.Cwd = cwd
	}

	clientTransport, agentTransport := acp.NewInMemoryTransports()
	agentErr := make(chan error, 1)
	go func() {
		agentErr <- runAgent(ctx, agentTransport, opts)
	}()

	handler := &handler{out: opts.Stdout}
	conn, err := acp.Connect(ctx, clientTransport, handler)
	if err != nil {
		return fmt.Errorf("could not connect ACP client: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Initialize(ctx, &acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(1),
		ClientInfo: &acp.Implementation{
			Name:    "cpe-cli",
			Title:   new("CPE CLI"),
			Version: version.Get(),
		},
		ClientCapabilities: &acp.ClientCapabilities{},
	}); err != nil {
		return fmt.Errorf("could not initialize ACP session: %w", err)
	}

	newSession, err := conn.NewSession(ctx, &acp.NewSessionRequest{
		Cwd:        opts.Cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return fmt.Errorf("could not create ACP session: %w", err)
	}
	defer conn.CloseSession(context.WithoutCancel(ctx), &acp.CloseSessionRequest{SessionID: newSession.SessionID})

	configOptions := []acp.SessionConfigOption{}
	if newSession.ConfigOptions != nil {
		configOptions = *newSession.ConfigOptions
	}
	if _, err := applySessionConfig(ctx, conn, newSession.SessionID, configOptions, opts); err != nil {
		return err
	}

	resp, err := conn.Prompt(ctx, &acp.PromptRequest{
		SessionID: newSession.SessionID,
		Prompt: []acp.ContentBlock{
			acp.TextContentBlock(opts.Prompt),
		},
	})
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}
	fmt.Fprintf(opts.Stdout, "\n\n[stop: %s]\n", resp.StopReason)

	select {
	case err := <-agentErr:
		if err != nil && !errors.Is(err, acp.ErrConnectionClosed) && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("ACP agent failed: %w", err)
		}
	default:
	}
	return nil
}
