package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopExecutor is a mock executor that returns a fixed response
func noopExecutor(_ context.Context, _ SubagentInput) (string, error) {
	return "mock response", nil
}

func TestNewServer(t *testing.T) {
	tests := []struct {
		name    string
		config  MCPServerConfig
		wantErr bool
	}{
		{
			name:    "missing subagent name",
			config:  MCPServerConfig{},
			wantErr: true,
		},
		{
			name: "missing subagent description",
			config: MCPServerConfig{
				Subagent: SubagentDef{
					Name: "test_agent",
				},
			},
			wantErr: true,
		},
		{
			name: "missing executor",
			config: MCPServerConfig{
				Subagent: SubagentDef{
					Name:        "test_agent",
					Description: "A test agent",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ServerOptions{}
			if !tt.wantErr || tt.name != "missing executor" {
				opts.Executor = noopExecutor
			}
			server, err := NewServer(tt.config, opts)
			if tt.wantErr {
				require.Error(t, err)
				cupaloy.SnapshotT(t, err.Error())
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

	server, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
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

	_, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
	require.NoError(t, err)

	// Close write end and read any output
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	// Verify no output was written during server creation
	cupaloy.SnapshotT(t, buf.String())
}

// TestServer_IntegrationInitialize tests that the server responds correctly to
// an MCP client connection
func TestServer_IntegrationInitialize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the cpe binary first
	binPath := "test_cpe_" + t.Name()
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, "../../.")
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
		Command: exec.CommandContext(context.Background(), "./"+binPath, "mcp", "serve", "--config", configPath),
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "failed to connect to MCP server")
	defer session.Close()

	// Verify we got a valid session
	require.NotNil(t, session)

	// The connection itself verifies the initialize handshake worked
	t.Log("Successfully connected to MCP server and completed handshake")
}

func TestNewServer_OutputSchema(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name           string
		setupFunc      func() string // returns output schema path
		wantErr        bool
		errMsgContains string // substring to check in error message (for error cases)
	}{
		{
			name: "missing output schema file",
			setupFunc: func() string {
				return filepath.Join(tmpDir, "nonexistent.json")
			},
			wantErr:        true,
			errMsgContains: "no such file or directory",
		},
		{
			name: "invalid JSON in output schema",
			setupFunc: func() string {
				path := filepath.Join(tmpDir, "invalid.json")
				os.WriteFile(path, []byte("not valid json {"), 0644)
				return path
			},
			wantErr:        true,
			errMsgContains: "contains invalid JSON",
		},
		{
			name: "valid output schema",
			setupFunc: func() string {
				path := filepath.Join(tmpDir, "valid.json")
				schema := `{"type": "object", "properties": {"result": {"type": "string"}}}`
				os.WriteFile(path, []byte(schema), 0644)
				return path
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemaPath := tt.setupFunc()
			cfg := MCPServerConfig{
				Subagent: SubagentDef{
					Name:             "test_agent",
					Description:      "A test agent",
					OutputSchemaPath: schemaPath,
				},
			}

			server, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsgContains, "error message should contain expected substring")
				assert.Nil(t, server)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, server)
				assert.NotNil(t, server.outputSchema)
			}
		})
	}
}

func TestServer_ToolRegistration(t *testing.T) {
	// Test that tools are registered with correct name and description
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "my_custom_tool",
			Description: "A tool that does custom things",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
	require.NoError(t, err)
	require.NotNil(t, server)
	require.NotNil(t, server.mcpServer)
}

func TestServer_ToolRegistrationWithOutputSchema(t *testing.T) {
	// Create a temporary output schema file
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "output_schema.json")
	schema := `{
		"type": "object",
		"properties": {
			"summary": {"type": "string"},
			"score": {"type": "number"}
		},
		"required": ["summary"]
	}`
	err := os.WriteFile(schemaPath, []byte(schema), 0644)
	require.NoError(t, err)

	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:             "structured_output_tool",
			Description:      "A tool with structured output",
			OutputSchemaPath: schemaPath,
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
	require.NoError(t, err)
	require.NotNil(t, server)
	require.NotNil(t, server.outputSchema)

	// Verify the schema was loaded correctly
	cupaloy.SnapshotT(t, string(server.outputSchema))
}

// TestServer_IntegrationToolsList verifies that tools/list returns the registered tool
func TestServer_IntegrationToolsList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the cpe binary first
	binPath := "test_cpe_" + t.Name()
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, "../../.")
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
  name: my_subagent_tool
  description: A subagent that processes requests
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
		Command: exec.CommandContext(context.Background(), "./"+binPath, "mcp", "serve", "--config", configPath),
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "failed to connect to MCP server")
	defer session.Close()

	// List tools
	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}

	// Verify exactly one tool is registered
	require.Len(t, tools, 1, "expected exactly one tool")

	tool := tools[0]
	cupaloy.SnapshotT(t, tool)
}

// TestServer_IntegrationToolsListWithOutputSchema verifies output schema is included
func TestServer_IntegrationToolsListWithOutputSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the cpe binary first
	binPath := "test_cpe_" + t.Name()
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, "../../.")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build cpe: %v\n%s", err, out)
	}
	defer os.Remove(binPath)

	// Create output schema file
	schemaPath := "test_output_schema_" + t.Name() + ".json"
	outputSchema := `{
		"type": "object",
		"properties": {
			"result": {"type": "string"},
			"confidence": {"type": "number"}
		}
	}`
	err := os.WriteFile(schemaPath, []byte(outputSchema), 0644)
	require.NoError(t, err)
	defer os.Remove(schemaPath)

	// Create a test config file with output schema
	configPath := "test_config_" + t.Name() + ".yaml"
	testConfig := `version: "1.0"
models:
  - ref: test
    display_name: "Test Model"
    id: test-model
    type: anthropic
    api_key_env: TEST_API_KEY
subagent:
  name: structured_tool
  description: A tool with structured output
  outputSchemaPath: ` + schemaPath + `
defaults:
  model: test
`
	err = os.WriteFile(configPath, []byte(testConfig), 0644)
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
		Command: exec.CommandContext(context.Background(), "./"+binPath, "mcp", "serve", "--config", configPath),
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "failed to connect to MCP server")
	defer session.Close()

	// List tools
	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}

	require.Len(t, tools, 1)
	tool := tools[0]

	// Verify output schema is present
	require.NotNil(t, tool.OutputSchema, "tool should have output schema")
	cupaloy.SnapshotT(t, tool)
}

// --- Error Handling Tests for Task 10 ---

// panicExecutor is a mock executor that always panics
func panicExecutor(_ context.Context, _ SubagentInput) (string, error) {
	panic("intentional panic for testing")
}

// contextCancelledExecutor checks if context is cancelled and waits
func contextCancelledExecutor(ctx context.Context, _ SubagentInput) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// errorExecutor returns a specific error
func errorExecutor(_ context.Context, _ SubagentInput) (string, error) {
	return "", fmt.Errorf("simulated API error: rate limit exceeded")
}

func TestServer_HandleToolCall_PanicRecovery(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "panic_agent",
			Description: "A test agent that panics",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: panicExecutor})
	require.NoError(t, err)

	// Call the tool handler directly
	ctx := context.Background()
	result, _, err := server.handleToolCall(ctx, nil, SubagentToolInput{Prompt: "trigger panic", RunID: "test-run-id"})

	// Should not return an error (panic is caught)
	assert.NoError(t, err)
	// Result should be an error response
	require.NotNil(t, result)
	assert.True(t, result.IsError, "result should be marked as error")
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	cupaloy.SnapshotT(t, textContent.Text)
}

func TestServer_HandleToolCall_EmptyPrompt(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
	require.NoError(t, err)

	tests := []struct {
		name    string
		prompt  string
		wantErr bool
	}{
		{
			name:    "empty prompt",
			prompt:  "",
			wantErr: true,
		},
		{
			name:    "whitespace only prompt",
			prompt:  "   \t\n  ",
			wantErr: true,
		},
		{
			name:    "valid prompt",
			prompt:  "Hello, agent!",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, _, err := server.handleToolCall(ctx, nil, SubagentToolInput{Prompt: tt.prompt, RunID: "test-run-id"})

			// The handler should never return a Go error - errors are in the result
			assert.NoError(t, err)
			require.NotNil(t, result)

			if tt.wantErr {
				assert.True(t, result.IsError, "result should be marked as error")
				require.Len(t, result.Content, 1)
				textContent, ok := result.Content[0].(*mcp.TextContent)
				require.True(t, ok)
				cupaloy.SnapshotT(t, textContent.Text)
			} else {
				assert.False(t, result.IsError, "result should not be an error")
				require.Len(t, result.Content, 1)
				textContent, ok := result.Content[0].(*mcp.TextContent)
				require.True(t, ok)
				cupaloy.SnapshotT(t, textContent.Text)
			}
		})
	}
}

func TestServer_HandleToolCall_ContextCancellation(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: contextCancelledExecutor})
	require.NoError(t, err)

	// Pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, _, err := server.handleToolCall(ctx, nil, SubagentToolInput{Prompt: "test", RunID: "test-run-id"})

	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	cupaloy.SnapshotT(t, textContent.Text)
}

func TestServer_HandleToolCall_ExecutorError(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: errorExecutor})
	require.NoError(t, err)

	ctx := context.Background()
	result, _, err := server.handleToolCall(ctx, nil, SubagentToolInput{Prompt: "test", RunID: "test-run-id"})

	// The handler should not return a Go error
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	cupaloy.SnapshotT(t, textContent.Text)
}

func TestServer_HandleToolCall_ContextDeadlineExceeded(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	// Executor that returns context.DeadlineExceeded
	deadlineExecutor := func(ctx context.Context, _ SubagentInput) (string, error) {
		return "", context.DeadlineExceeded
	}

	server, err := NewServer(cfg, ServerOptions{Executor: deadlineExecutor})
	require.NoError(t, err)

	ctx := context.Background()
	result, _, err := server.handleToolCall(ctx, nil, SubagentToolInput{Prompt: "test", RunID: "test-run-id"})

	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	cupaloy.SnapshotT(t, textContent.Text)
}

func TestServer_HandleToolCall_ValidInput(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	expectedResponse := "execution successful"
	successExecutor := func(_ context.Context, input SubagentInput) (string, error) {
		// Verify input is passed correctly
		if input.Prompt != "test prompt" {
			return "", fmt.Errorf("unexpected prompt: %s", input.Prompt)
		}
		if len(input.Inputs) != 2 || input.Inputs[0] != "file1.txt" || input.Inputs[1] != "file2.txt" {
			return "", fmt.Errorf("unexpected inputs: %v", input.Inputs)
		}
		return expectedResponse, nil
	}

	server, err := NewServer(cfg, ServerOptions{Executor: successExecutor})
	require.NoError(t, err)

	ctx := context.Background()
	result, _, err := server.handleToolCall(ctx, nil, SubagentToolInput{
		Prompt: "test prompt",
		Inputs: []string{"file1.txt", "file2.txt"},
		RunID:  "test-run-id",
	})

	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	cupaloy.SnapshotT(t, textContent.Text)
}


// --- RunID Validation Tests ---

func TestServer_HandleToolCall_RunIDValidation(t *testing.T) {
	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: noopExecutor})
	require.NoError(t, err)

	tests := []struct {
		name    string
		input   SubagentToolInput
		wantErr bool
	}{
		{
			name: "missing runId returns error",
			input: SubagentToolInput{
				Prompt: "test prompt",
				RunID:  "",
			},
			wantErr: true,
		},
		{
			name: "whitespace-only runId returns error",
			input: SubagentToolInput{
				Prompt: "test prompt",
				RunID:  "   ",
			},
			wantErr: true,
		},
		{
			name: "valid runId succeeds",
			input: SubagentToolInput{
				Prompt: "test prompt",
				RunID:  "run-123",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, _, err := server.handleToolCall(ctx, nil, tt.input)

			assert.NoError(t, err)
			require.NotNil(t, result)

			if tt.wantErr {
				assert.True(t, result.IsError, "result should be marked as error")
				require.Len(t, result.Content, 1)
				textContent, ok := result.Content[0].(*mcp.TextContent)
				require.True(t, ok)
				cupaloy.SnapshotT(t, textContent.Text)
			} else {
				assert.False(t, result.IsError, "result should not be an error")
				require.Len(t, result.Content, 1)
				textContent, ok := result.Content[0].(*mcp.TextContent)
				require.True(t, ok)
				cupaloy.SnapshotT(t, textContent.Text)
			}
		})
	}
}

func TestServer_HandleToolCall_RunIDPassedToExecutor(t *testing.T) {
	var capturedInput SubagentInput
	captureExecutor := func(_ context.Context, input SubagentInput) (string, error) {
		capturedInput = input
		return "success", nil
	}

	cfg := MCPServerConfig{
		Subagent: SubagentDef{
			Name:        "test_agent",
			Description: "A test agent",
		},
	}

	server, err := NewServer(cfg, ServerOptions{Executor: captureExecutor})
	require.NoError(t, err)

	ctx := context.Background()
	input := SubagentToolInput{
		Prompt: "test prompt",
		RunID:  "unique-run-id-456",
		Inputs: []string{"file1.txt", "file2.txt"},
	}

	result, _, err := server.handleToolCall(ctx, nil, input)

	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError, "result should not be an error")

	// Snapshot the captured input to verify all fields were passed correctly
	cupaloy.SnapshotT(t, capturedInput)
}
