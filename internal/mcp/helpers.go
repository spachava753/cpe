package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"
)

// NewToolCallback creates a ToolCallback for an MCP tool
func NewToolCallback(session *mcpsdk.ClientSession, serverName, toolName string) *ToolCallback {
	return &ToolCallback{
		ClientSession: session,
		ServerName:    serverName,
		ToolName:      toolName,
	}
}

// ToGaiTool converts an MCP tool to a gai.Tool
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
