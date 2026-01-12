package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/version"
)

// SubagentDef defines a subagent for MCP server mode.
type SubagentDef struct {
	// Name is the tool name exposed via MCP (required)
	Name string
	// Description is the tool description exposed via MCP (required)
	Description string
	// OutputSchemaPath is an optional path to a JSON schema file for structured output
	OutputSchemaPath string
}

// MCPServerConfig holds the configuration needed to run an MCP server.
type MCPServerConfig struct {
	// Subagent is the subagent configuration (required)
	Subagent SubagentDef

	// MCPServers are the MCP server configurations available to the subagent
	MCPServers map[string]ServerConfig
}

// SubagentInput represents the input passed to the subagent executor
type SubagentInput struct {
	// Prompt is the task to execute
	Prompt string
	// Inputs is a list of file paths to include as context
	Inputs []string
}

// SubagentExecutor is a function that executes a subagent with the given input.
// It returns the result text or an error.
type SubagentExecutor func(ctx context.Context, input SubagentInput) (string, error)

// ServerOptions contains options for creating an MCP server
type ServerOptions struct {
	// Executor is the function that executes the subagent (required)
	Executor SubagentExecutor
}

// SubagentToolInput defines the input schema for the subagent tool
type SubagentToolInput struct {
	// Prompt is the task to execute (required)
	Prompt string `json:"prompt" jsonschema:"The task or instruction for the subagent to execute"`
	// Inputs is an optional list of file paths to include as context
	Inputs []string `json:"inputs,omitempty" jsonschema:"Optional list of file paths to include as context for the subagent"`
}

// Server wraps an MCP server that exposes a subagent as a tool
type Server struct {
	config       MCPServerConfig
	opts         ServerOptions
	mcpServer    *mcpsdk.Server
	outputSchema json.RawMessage
}

// NewServer creates a new MCP server from the given configuration.
// The config must have a valid Subagent configuration.
func NewServer(cfg MCPServerConfig, opts ServerOptions) (*Server, error) {
	if cfg.Subagent.Name == "" {
		return nil, fmt.Errorf("subagent name is required")
	}
	if cfg.Subagent.Description == "" {
		return nil, fmt.Errorf("subagent description is required")
	}
	if opts.Executor == nil {
		return nil, fmt.Errorf("executor is required")
	}

	// Load and validate output schema if configured
	var outputSchema json.RawMessage
	if cfg.Subagent.OutputSchemaPath != "" {
		schemaBytes, err := os.ReadFile(cfg.Subagent.OutputSchemaPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read output schema file %q: %w", cfg.Subagent.OutputSchemaPath, err)
		}
		// Validate it's valid JSON
		if !json.Valid(schemaBytes) {
			return nil, fmt.Errorf("output schema file %q contains invalid JSON", cfg.Subagent.OutputSchemaPath)
		}
		outputSchema = schemaBytes
	}

	// Create the underlying MCP server
	mcpServer := mcpsdk.NewServer(
		&mcpsdk.Implementation{
			Name:    "cpe",
			Title:   "CPE MCP Server",
			Version: version.Get(),
		},
		nil,
	)

	server := &Server{
		config:       cfg,
		opts:         opts,
		mcpServer:    mcpServer,
		outputSchema: outputSchema,
	}

	// Register the subagent as a tool
	if err := server.registerSubagentTool(); err != nil {
		return nil, fmt.Errorf("failed to register subagent tool: %w", err)
	}

	return server, nil
}

// registerSubagentTool registers the subagent as an MCP tool
func (s *Server) registerSubagentTool() error {
	tool := &mcpsdk.Tool{
		Name:        s.config.Subagent.Name,
		Description: s.config.Subagent.Description,
	}

	// Set output schema if configured
	if s.outputSchema != nil {
		tool.OutputSchema = s.outputSchema
	}

	// Register the tool with a typed handler
	mcpsdk.AddTool(s.mcpServer, tool, s.handleToolCall)

	return nil
}

// handleToolCall handles calls to the subagent tool.
// It includes panic recovery to ensure panics are returned as structured errors
// rather than crashing the server process.
func (s *Server) handleToolCall(ctx context.Context, req *mcpsdk.CallToolRequest, input SubagentToolInput) (result *mcpsdk.CallToolResult, metadata any, err error) {
	// Recover from panics and convert to error response
	defer func() {
		if r := recover(); r != nil {
			result = &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{
					Text: fmt.Sprintf("Subagent execution panicked: %v", r),
				}},
				IsError: true,
			}
			metadata = nil
			err = nil
		}
	}()

	// Validate input
	if strings.TrimSpace(input.Prompt) == "" {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{
				Text: "Subagent execution failed: prompt is required and cannot be empty",
			}},
			IsError: true,
		}, nil, nil
	}

	// Check context before starting execution
	if err := ctx.Err(); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{
				Text: fmt.Sprintf("Subagent execution failed: %v", err),
			}},
			IsError: true,
		}, nil, nil
	}

	execResult, execErr := s.opts.Executor(ctx, SubagentInput(input))
	if execErr != nil {
		// Provide actionable error messages based on error type
		errMsg := execErr.Error()
		if ctx.Err() != nil {
			errMsg = fmt.Sprintf("execution cancelled or timed out: %v", execErr)
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{
				Text: fmt.Sprintf("Subagent execution failed: %s", errMsg),
			}},
			IsError: true,
		}, nil, nil
	}

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{
			Text: execResult,
		}},
	}, nil, nil
}

// Serve starts the MCP server and blocks until the context is cancelled
// or the connection is closed. The server communicates via stdio.
func (s *Server) Serve(ctx context.Context) error {
	// Run the server on stdio transport
	// The MCP SDK handles graceful shutdown when context is cancelled
	transport := &mcpsdk.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}
