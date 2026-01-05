package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// ServerOptions contains options for creating an MCP server
type ServerOptions struct {
	// Currently empty, reserved for future options like custom logging
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
	mcpServer    *mcp.Server
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
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
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
	tool := &mcp.Tool{
		Name:        s.config.Subagent.Name,
		Description: s.config.Subagent.Description,
	}

	// Set output schema if configured
	if s.outputSchema != nil {
		tool.OutputSchema = s.outputSchema
	}

	// Register the tool with a typed handler
	// The handler returns an error for now - actual execution will be implemented in Task 5
	mcp.AddTool(s.mcpServer, tool, s.handleToolCall)

	return nil
}

// handleToolCall handles calls to the subagent tool
// This is a placeholder that will be fully implemented in Task 5
func (s *Server) handleToolCall(ctx context.Context, req *mcp.CallToolRequest, input SubagentToolInput) (*mcp.CallToolResult, any, error) {
	// Task 5 will implement actual subagent execution
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Subagent execution not yet implemented. Received prompt: %s", input.Prompt),
		}},
		IsError: true,
	}, nil, nil
}

// Serve starts the MCP server and blocks until the context is cancelled
// or the connection is closed. The server communicates via stdio.
func (s *Server) Serve(ctx context.Context) error {
	// Run the server on stdio transport
	// The MCP SDK handles graceful shutdown when context is cancelled
	transport := &mcp.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}
