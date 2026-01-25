package mcp

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateTransport_StdioWithLoggingAddress(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		Command: "echo",
		Args:    []string{"hello"},
		Type:    "stdio",
	}
	loggingAddress := "http://localhost:8080/log"

	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		t.Fatalf("CreateTransport failed: %v", err)
	}

	// Type assert to CommandTransport to access the Command
	cmdTransport, ok := transport.(*mcp.CommandTransport)
	if !ok {
		t.Fatalf("expected *mcp.CommandTransport, got %T", transport)
	}

	// Check that the command has the logging address in its environment
	found := false
	expectedEnv := subagentLoggingAddressEnv + "=" + loggingAddress
	for _, env := range cmdTransport.Command.Env {
		if env == expectedEnv {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected env var %s to be set in command environment", expectedEnv)
	}
}

func TestCreateTransport_StdioWithEmptyLoggingAddress(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		Command: "echo",
		Args:    []string{"hello"},
		Type:    "stdio",
	}
	loggingAddress := ""

	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		t.Fatalf("CreateTransport failed: %v", err)
	}

	// Type assert to CommandTransport to access the Command
	cmdTransport, ok := transport.(*mcp.CommandTransport)
	if !ok {
		t.Fatalf("expected *mcp.CommandTransport, got %T", transport)
	}

	// When loggingAddress is empty, cmd.Env should be nil (uses default environment)
	if cmdTransport.Command.Env != nil {
		// Check that CPE_SUBAGENT_LOGGING_ADDRESS is not set
		for _, env := range cmdTransport.Command.Env {
			if strings.HasPrefix(env, subagentLoggingAddressEnv+"=") {
				t.Errorf("expected env var %s to NOT be set when loggingAddress is empty", subagentLoggingAddressEnv)
			}
		}
	}
}

func TestCreateTransport_StdioInheritsOSEnviron(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		Command: "echo",
		Args:    []string{"hello"},
		Type:    "stdio",
	}
	loggingAddress := "http://localhost:8080/log"

	// Set a test env var to verify os.Environ() is inherited
	testEnvKey := "CPE_TEST_ENV_VAR_12345"
	testEnvValue := "test_value"
	os.Setenv(testEnvKey, testEnvValue)
	defer os.Unsetenv(testEnvKey)

	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		t.Fatalf("CreateTransport failed: %v", err)
	}

	cmdTransport, ok := transport.(*mcp.CommandTransport)
	if !ok {
		t.Fatalf("expected *mcp.CommandTransport, got %T", transport)
	}

	// Check that the test env var is inherited
	foundTestEnv := false
	expectedTestEnv := testEnvKey + "=" + testEnvValue
	for _, env := range cmdTransport.Command.Env {
		if env == expectedTestEnv {
			foundTestEnv = true
			break
		}
	}
	if !foundTestEnv {
		t.Errorf("expected inherited env var %s to be present in command environment", expectedTestEnv)
	}
}

func TestCreateTransport_HTTPUnaffected(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		URL:  "http://localhost:8080/mcp",
		Type: "http",
	}
	loggingAddress := "http://localhost:9090/log"

	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		t.Fatalf("CreateTransport failed: %v", err)
	}

	// HTTP transport should be StreamableClientTransport
	_, ok := transport.(*mcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.StreamableClientTransport, got %T", transport)
	}
	// HTTP transport doesn't spawn a child process, so loggingAddress has no effect
}

func TestCreateTransport_SSEUnaffected(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		URL:  "http://localhost:8080/sse",
		Type: "sse",
	}
	loggingAddress := "http://localhost:9090/log"

	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		t.Fatalf("CreateTransport failed: %v", err)
	}

	// SSE transport should be SSEClientTransport
	_, ok := transport.(*mcp.SSEClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.SSEClientTransport, got %T", transport)
	}
	// SSE transport doesn't spawn a child process, so loggingAddress has no effect
}

func TestCreateTransport_StdioWithConfigEnv(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		Command: "echo",
		Args:    []string{"hello"},
		Type:    "stdio",
		Env: map[string]string{
			"CUSTOM_VAR":  "custom_value",
			"ANOTHER_VAR": "another_value",
		},
	}
	loggingAddress := "http://127.0.0.1:8080"

	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		t.Fatalf("CreateTransport returned error: %v", err)
	}

	cmdTransport, ok := transport.(*mcp.CommandTransport)
	if !ok {
		t.Fatalf("expected *mcp.CommandTransport, got %T", transport)
	}

	// Verify custom env vars from config are set
	envMap := make(map[string]string)
	for _, env := range cmdTransport.Command.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["CUSTOM_VAR"] != "custom_value" {
		t.Errorf("expected CUSTOM_VAR=custom_value, got %q", envMap["CUSTOM_VAR"])
	}
	if envMap["ANOTHER_VAR"] != "another_value" {
		t.Errorf("expected ANOTHER_VAR=another_value, got %q", envMap["ANOTHER_VAR"])
	}
	if envMap[subagentLoggingAddressEnv] != loggingAddress {
		t.Errorf("expected %s=%s, got %q", subagentLoggingAddressEnv, loggingAddress, envMap[subagentLoggingAddressEnv])
	}
}

func TestCreateTransport_StdioConfigEnvWithoutLoggingAddress(t *testing.T) {
	ctx := context.Background()
	config := ServerConfig{
		Command: "echo",
		Args:    []string{"hello"},
		Type:    "stdio",
		Env: map[string]string{
			"MY_API_KEY": "secret123",
		},
	}

	transport, err := CreateTransport(ctx, config, "") // empty logging address
	if err != nil {
		t.Fatalf("CreateTransport returned error: %v", err)
	}

	cmdTransport, ok := transport.(*mcp.CommandTransport)
	if !ok {
		t.Fatalf("expected *mcp.CommandTransport, got %T", transport)
	}

	// Verify custom env var is set even without logging address
	envMap := make(map[string]string)
	for _, env := range cmdTransport.Command.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["MY_API_KEY"] != "secret123" {
		t.Errorf("expected MY_API_KEY=secret123, got %q", envMap["MY_API_KEY"])
	}
	
	// Verify logging address is NOT set
	if _, ok := envMap[subagentLoggingAddressEnv]; ok {
		t.Errorf("expected %s to NOT be set when loggingAddress is empty", subagentLoggingAddressEnv)
	}
}

