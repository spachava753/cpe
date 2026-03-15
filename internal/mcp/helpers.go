package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

// NewToolCallback binds an MCP session/server/tool triple into a reusable callback.
func NewToolCallback(session *mcpsdk.ClientSession, serverName, toolName string, serverConfig mcpconfig.ServerConfig) *ToolCallback {
	return &ToolCallback{
		ClientSession: session,
		ServerName:    serverName,
		ToolName:      toolName,
		ServerConfig:  serverConfig,
	}
}

// ToGaiTool adapts MCP tool metadata into gai.Tool format used by providers.
// InputSchema is normalized via JSON marshal/unmarshal into jsonschema.Schema.
func ToGaiTool(mcpTool *mcpsdk.Tool) (gai.Tool, error) {
	// Convert InputSchema from map[string]any to *jsonschema.Schema
	inputSchemaJSON, err := json.Marshal(mcpTool.InputSchema)
	if err != nil {
		return gai.Tool{}, fmt.Errorf("marshaling input schema: %w", err)
	}

	var inputSchema *jsonschema.Schema
	if err := json.Unmarshal(inputSchemaJSON, &inputSchema); err != nil {
		return gai.Tool{}, fmt.Errorf("unmarshaling input schema: %w", err)
	}

	return gai.Tool{
		Name:        mcpTool.Name,
		Description: mcpTool.Description,
		InputSchema: inputSchema,
	}, nil
}
