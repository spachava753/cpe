package acp_test

import (
	"context"
	"io"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"
	"github.com/spachava753/cpe/internal/acp"
)

type testAcpClient struct{}

// CreateTerminal implements [acp.Client].
func (t *testAcpClient) CreateTerminal(ctx context.Context, params acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	panic("unimplemented")
}

// KillTerminal implements [acp.Client].
func (t *testAcpClient) KillTerminal(ctx context.Context, params acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	panic("unimplemented")
}

// ReadTextFile implements [acp.Client].
func (t *testAcpClient) ReadTextFile(ctx context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	panic("unimplemented")
}

// ReleaseTerminal implements [acp.Client].
func (t *testAcpClient) ReleaseTerminal(ctx context.Context, params acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	panic("unimplemented")
}

// RequestPermission implements [acp.Client].
func (t *testAcpClient) RequestPermission(ctx context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	panic("unimplemented")
}

// SessionUpdate implements [acp.Client].
func (t *testAcpClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	panic("unimplemented")
}

// TerminalOutput implements [acp.Client].
func (t *testAcpClient) TerminalOutput(ctx context.Context, params acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	panic("unimplemented")
}

// WaitForTerminalExit implements [acp.Client].
func (t *testAcpClient) WaitForTerminalExit(ctx context.Context, params acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	panic("unimplemented")
}

// WriteTextFile implements [acp.Client].
func (t *testAcpClient) WriteTextFile(ctx context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	panic("unimplemented")
}

var _ acpsdk.Client = (*testAcpClient)(nil)

func TestInit(t *testing.T) {
	client := testAcpClient{}
	ar, aw := io.Pipe()
	cr, cw := io.Pipe()

	go func() {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(func() {
			cancel()
		})
		if err := acp.Serve(ctx, acp.ServeOptions{
			Stdout:     aw,
			Stdin:      cr,
			Stderr:     io.Discard,
			ConfigPath: "",
			DbPath:     "",
		}); err != nil {
			t.Errorf("agent returned error: %v", err)
		}
	}()
	t.Log("started agent")

	clientConn := acpsdk.NewClientSideConnection(&client, cw, ar)
	t.Log("created connection")

	resp, err := clientConn.Initialize(t.Context(), acpsdk.InitializeRequest{
		ClientCapabilities: acpsdk.ClientCapabilities{
			Fs: acpsdk.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: false,
		},
		ClientInfo: &acpsdk.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
	})
	t.Log("called init")
	// we should not get an error on init connection
	be.Err(t, err, nil)
	// assert agent capabilities
	be.True(t, resp.AgentCapabilities.LoadSession)
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Close, &acpsdk.SessionCloseCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.List, &acpsdk.SessionListCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Resume, &acpsdk.SessionResumeCapabilities{})
	be.True(t, resp.AgentCapabilities.PromptCapabilities.Audio)
	be.True(t, resp.AgentCapabilities.PromptCapabilities.Image)
	be.True(t, !resp.AgentCapabilities.PromptCapabilities.EmbeddedContext)
}
