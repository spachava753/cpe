package mcp

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	tests := []struct {
		name    string
		config  MCPServerConfig
		wantErr string
	}{
		{
			name:    "missing subagent name",
			config:  MCPServerConfig{},
			wantErr: "subagent name is required",
		},
		{
			name: "missing subagent description",
			config: MCPServerConfig{
				Subagent: SubagentDef{
					Name: "test_agent",
				},
			},
			wantErr: "subagent description is required",
		},
		{
			name: "valid config",
			config: MCPServerConfig{
				Subagent: SubagentDef{
					Name:        "test_agent",
					Description: "A test agent",
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.config, ServerOptions{})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, server)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, server)
			}
		})
	}
}

func TestServer_Serve_ContextCancellation(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	server, err := NewServer(cfg, ServerOptions{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	// Start the server in a goroutine
	var wg sync.WaitGroup
	var serveErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Replace stdin/stdout with pipes for the test
		// The server will block on reading from stdin
		oldStdin := os.Stdin
		oldStdout := os.Stdout
		defer func() {
			os.Stdin = oldStdin
			os.Stdout = oldStdout
		}()

		// Create pipes
		stdinReader, stdinWriter, _ := os.Pipe()
		stdoutReader, stdoutWriter, _ := os.Pipe()
		os.Stdin = stdinReader
		os.Stdout = stdoutWriter

		// Close the pipes after the test
		defer stdinReader.Close()
		defer stdinWriter.Close()
		defer stdoutReader.Close()
		defer stdoutWriter.Close()

		serveErr = server.Serve(ctx)
	}()

	// Give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for the server to shut down with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Server shut down cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}

	// The error should be nil or context cancelled
	if serveErr != nil && !strings.Contains(serveErr.Error(), "context canceled") {
		t.Errorf("unexpected error: %v", serveErr)
	}
}

func TestServer_StdoutClean(t *testing.T) {
	// This test verifies that creating and starting a server doesn't
	// write non-protocol data to stdout before the MCP handshake
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	// Capture stdout during server creation
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, err := NewServer(cfg, ServerOptions{})
	require.NoError(t, err)

	// Close write end and read any output
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	// Verify no output was written during server creation
	assert.Empty(t, buf.String(), "NewServer should not write to stdout")
}

// TestServer_IntegrationInitialize tests that the server responds correctly to
// an MCP client connection
func TestServer_IntegrationInitialize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the cpe binary first
	binPath := "test_cpe_" + t.Name()
	cmd := exec.Command("go", "build", "-o", binPath, "../../.")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build cpe: %v\n%s", err, out)
	}
	defer os.Remove(binPath)

	// Create a test config file
	configPath := "test_config_" + t.Name() + ".yaml"
	testConfig := `version: "1.0"
models:
  - ref: test
    display_name: "Test Model"
    id: test-model
    type: anthropic
    api_key_env: TEST_API_KEY
subagent:
  name: test_agent
  description: A test agent for integration testing
defaults:
  model: test
`
	err := os.WriteFile(configPath, []byte(testConfig), 0644)
	require.NoError(t, err)
	defer os.Remove(configPath)

	// Create an MCP client
	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		},
		nil,
	)

	// Connect to the server using CommandTransport
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport := &mcp.CommandTransport{
		Command: exec.Command("./"+binPath, "mcp", "serve", "--config", configPath),
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "failed to connect to MCP server")
	defer session.Close()

	// Verify we got a valid session
	require.NotNil(t, session)

	// The connection itself verifies the initialize handshake worked
	t.Log("Successfully connected to MCP server and completed handshake")
}
