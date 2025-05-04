package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spachava753/gai"
)

// MCPToolCallback implements the gai.ToolCallback interface for MCP tools
type MCPToolCallback struct {
	ClientManager *ClientManager
	ServerName    string
	ToolName      string
}

// Call implements the gai.ToolCallback interface
func (c *MCPToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Parse parameters
	var params map[string]interface{}
	if err := json.Unmarshal(parametersJSON, &params); err != nil {
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           toolCallID,
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(fmt.Sprintf("Error parsing parameters: %v", err)),
				},
			},
		}, nil
	}

	// Call the tool
	result, err := c.ClientManager.CallTool(ctx, c.ServerName, c.ToolName, params)
	if err != nil {
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           toolCallID,
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(fmt.Sprintf("Error calling MCP tool %s/%s: %v", c.ServerName, c.ToolName, err)),
				},
			},
		}, nil
	}

	// Process the content
	resultText := PrintContent(result.Content)
	return gai.Message{
		Role: gai.ToolResult,
		Blocks: []gai.Block{
			{
				ID:           toolCallID,
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(resultText),
			},
		},
	}, nil
}

// RegisterMCPServerTools registers all tools from all MCP servers with the tool registerer
func RegisterMCPServerTools(ctx context.Context, clientManager *ClientManager, toolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}) error {
	serverNames := clientManager.ListServerNames()
	registeredTools := make(map[string]string) // Map to track tool names for duplicate detection

	// For each server, initialize and register tools
	for _, serverName := range serverNames {
		// Initialize the client
		_, err := clientManager.InitializeClient(ctx, serverName)
		if err != nil {
			return fmt.Errorf("failed to initialize MCP client for server %s: %w", serverName, err)
		}

		// List tools
		toolsResult, err := clientManager.ListTools(ctx, serverName)
		if err != nil {
			return fmt.Errorf("failed to list MCP tools for server %s: %w", serverName, err)
		}

		// Register each tool
		for _, mcpTool := range toolsResult.Tools {
			// Check for duplicate tool names
			if existingServer, exists := registeredTools[mcpTool.Name]; exists {
				return fmt.Errorf("duplicate tool name '%s' found in servers '%s' and '%s'",
					mcpTool.Name, existingServer, serverName)
			}

			// Convert MCP tool to GAI tool (preserving original name)
			gaiTool, err := ConvertMCPToolToGAITool(mcpTool)
			if err != nil {
				return fmt.Errorf("failed to convert MCP tool '%s' from server '%s': %w",
					mcpTool.Name, serverName, err)
			}

			// Create a callback for this tool
			callback := &MCPToolCallback{
				ClientManager: clientManager,
				ServerName:    serverName,
				ToolName:      mcpTool.Name,
			}

			// Register the tool with the callback
			err = toolRegisterer.Register(gaiTool, callback)
			if err != nil {
				return fmt.Errorf("failed to register MCP tool '%s' from server '%s': %w",
					mcpTool.Name, serverName, err)
			}

			// Track this tool to detect duplicates
			registeredTools[mcpTool.Name] = serverName
		}
	}

	return nil
}
