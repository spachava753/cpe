package mcp

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
			name: "missing executor",
			config: MCPServerConfig{
				Subagent: SubagentDef{
					Name:        "test_agent",
					Description: "A test agent",
				},
			},
			wantErr: "executor is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ServerOptions{}
			if tt.wantErr == "" || !strings.Contains(tt.wantErr, "executor") {
				opts.Executor = noopExecutor
			}
			server, err := NewServer(tt.config, opts)
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

func TestNewServer_OutputSchema(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		setupFunc func() string // returns output schema path
		wantErr   string
	}{
		{
			name: "missing output schema file",
			setupFunc: func() string {
				return filepath.Join(tmpDir, "nonexistent.json")
			},
			wantErr: "failed to read output schema file",
		},
		{
			name: "invalid JSON in output schema",
			setupFunc: func() string {
				path := filepath.Join(tmpDir, "invalid.json")
				os.WriteFile(path, []byte("not valid json {"), 0644)
				return path
			},
			wantErr: "contains invalid JSON",
		},
		{
			name: "valid output schema",
			setupFunc: func() string {
				path := filepath.Join(tmpDir, "valid.json")
				schema := `{"type": "object", "properties": {"result": {"type": "string"}}}`
				os.WriteFile(path, []byte(schema), 0644)
				return path
			},
			wantErr: "",
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
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
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
	assert.JSONEq(t, schema, string(server.outputSchema))
}

// TestServer_IntegrationToolsList verifies that tools/list returns the registered tool
func TestServer_IntegrationToolsList(t *testing.T) {
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
		Command: exec.Command("./"+binPath, "mcp", "serve", "--config", configPath),
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
	assert.Equal(t, "my_subagent_tool", tool.Name)
	assert.Equal(t, "A subagent that processes requests", tool.Description)

	// Verify input schema has the expected structure
	inputSchema, ok := tool.InputSchema.(map[string]any)
	require.True(t, ok, "input schema should be a map")
	assert.Equal(t, "object", inputSchema["type"])

	props, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")

	// Verify prompt property exists
	_, hasPrompt := props["prompt"]
	assert.True(t, hasPrompt, "input schema should have 'prompt' property")

	// Verify inputs property exists
	_, hasInputs := props["inputs"]
	assert.True(t, hasInputs, "input schema should have 'inputs' property")
}

// TestServer_IntegrationToolsListWithOutputSchema verifies output schema is included
func TestServer_IntegrationToolsListWithOutputSchema(t *testing.T) {
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
		Command: exec.Command("./"+binPath, "mcp", "serve", "--config", configPath),
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

	outputSchemaMap, ok := tool.OutputSchema.(map[string]any)
	require.True(t, ok, "output schema should be a map")
	assert.Equal(t, "object", outputSchemaMap["type"])

	props, ok := outputSchemaMap["properties"].(map[string]any)
	require.True(t, ok)
	_, hasResult := props["result"]
	assert.True(t, hasResult, "output schema should have 'result' property")
}
