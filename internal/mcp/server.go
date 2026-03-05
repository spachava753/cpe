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

// SubagentDef describes the single tool exposed by CPE MCP server mode.
type SubagentDef struct {
	// Name is the MCP tool name advertised to parent clients (required).
	Name string
	// Description is the MCP tool description shown to parent clients (required).
	Description string
	// OutputSchemaPath optionally points to a JSON schema file. When provided,
	// the schema is loaded at startup and published as the MCP tool output schema.
	OutputSchemaPath string
}

// MCPServerConfig contains configuration required to expose one subagent tool.
type MCPServerConfig struct {
	// Subagent defines the tool identity and optional output schema (required).
	Subagent SubagentDef

	// MCPServers holds downstream MCP server definitions available during subagent
	// execution. It is passed through to executor-level setup.
	MCPServers map[string]ServerConfig
}

// SubagentInput is the normalized tool input passed to SubagentExecutor.
type SubagentInput struct {
	// Prompt is the task text to execute.
	Prompt string
	// Inputs is an optional list of file paths to provide as additional context.
	Inputs []string
	// RunID correlates lifecycle/tool events for this invocation.
	RunID string
}

// SubagentExecutor executes one subagent invocation and returns terminal text.
//
// MCP server mode calls the executor once per tool invocation and does not retry
// on failure; errors are surfaced directly to the MCP client as tool errors.
type SubagentExecutor func(ctx context.Context, input SubagentInput) (string, error)

// ServerOptions provides required runtime dependencies for MCP Server.
type ServerOptions struct {
	// Executor runs the underlying subagent for each MCP tool call (required).
	Executor SubagentExecutor
}

// SubagentToolInput is the JSON schema exposed to MCP clients for the single
// subagent tool.
type SubagentToolInput struct {
	// Prompt is required and must be non-empty after trimming whitespace.
	Prompt string `json:"prompt" jsonschema:"The task or instruction for the subagent to execute"`
	// Inputs is an optional list of context file paths.
	Inputs []string `json:"inputs,omitempty" jsonschema:"Optional list of file paths to include as context for the subagent"`
	// RunID is required for event correlation across start/tool/result/end events.
	RunID string `json:"runId" jsonschema:"required,A unique identifier for correlating events across the subagent execution"`
}

// Server wraps the MCP SDK server and exposes exactly one subagent tool.
// Transport is stdio-only; stdout is reserved for MCP protocol frames.
type Server struct {
	config       MCPServerConfig
	opts         ServerOptions
	mcpServer    *mcpsdk.Server
	outputSchema json.RawMessage
}

// NewServer validates configuration, preloads optional output schema JSON, and
// registers the configured subagent tool on a new MCP SDK server.
//
// Startup fails fast when required subagent metadata is missing, executor is nil,
// or the output schema file cannot be read/parsed as valid JSON.
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

// registerSubagentTool installs the single subagent tool and typed input handler.
// OutputSchema is attached when configured so callers can rely on structured
// output expectations at the protocol level.
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

// handleToolCall validates request input, executes the subagent once, and maps
// outcomes to MCP CallToolResult values.
//
// Failure semantics:
//   - Panics are recovered and converted to IsError=true tool results.
//   - Validation/context/executor failures return IsError=true tool results.
//   - Method-level error return is reserved for transport/framework failures, so
//     normal execution failures are represented in result content instead.
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

	if strings.TrimSpace(input.RunID) == "" {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{
				Text: "Subagent execution failed: runId is required and cannot be empty",
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

// Serve runs the MCP server on stdio transport until ctx is cancelled or the
// stdio connection closes.
//
// Stdio discipline: stdout is protocol-only in server mode. Human-readable logs
// must use stderr to avoid corrupting MCP framing.
func (s *Server) Serve(ctx context.Context) error {
	// Run the server on stdio transport
	// The MCP SDK handles graceful shutdown when context is cancelled
	transport := &mcpsdk.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}
