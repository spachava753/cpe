package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spachava753/gai"
	"os"
	"strings"
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
// It continues registering tools even if some fail, collecting warnings along the way
func RegisterMCPServerTools(ctx context.Context, clientManager *ClientManager, toolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}) error {
	serverNames := clientManager.ListServerNames()

	// If no servers are configured, return early without an error
	if len(serverNames) == 0 {
		return nil
	}

	registeredTools := make(map[string]string) // Map to track tool names for duplicate detection
	var warnings []string                      // To collect all warnings
	registeredCount := 0                       // Count successful registrations

	// For each server, initialize and register tools
	for _, serverName := range serverNames {
		// Initialize the client
		_, err := clientManager.InitializeClient(ctx, serverName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to initialize MCP client for server %s: %v", serverName, err))
			// Skip this server but continue with others
			continue
		}

		// List tools
		toolsResult, err := clientManager.ListTools(ctx, serverName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to list MCP tools for server %s: %v", serverName, err))
			// Skip this server but continue with others
			continue
		}

		// Register each tool
		for _, mcpTool := range toolsResult.Tools {
			// Check for duplicate tool names
			if existingServer, exists := registeredTools[mcpTool.Name]; exists {
				warnings = append(warnings, fmt.Sprintf(
					"skipping duplicate tool name '%s' in server '%s' (already registered from server '%s')",
					mcpTool.Name, serverName, existingServer))
				continue
			}

			// Convert MCP tool to GAI tool (preserving original name)
			gaiTool, err := ConvertMCPToolToGAITool(mcpTool)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf(
					"failed to convert MCP tool '%s' from server '%s': %v",
					mcpTool.Name, serverName, err))
				// Skip this tool but continue with others
				continue
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
				warnings = append(warnings, fmt.Sprintf(
					"failed to register MCP tool '%s' from server '%s': %v",
					mcpTool.Name, serverName, err))
				// Skip this tool but continue with others
				continue
			}

			// Track this tool to detect duplicates
			registeredTools[mcpTool.Name] = serverName
			registeredCount++
		}
	}

	// If we have warnings but also registered at least one tool, print warnings only
	if len(warnings) > 0 {
		if registeredCount > 0 {
			// Print warnings to stderr for visibility
			fmt.Fprintf(os.Stderr, "Registered %d MCP tools with %d warnings:\n", registeredCount, len(warnings))
			for _, warning := range warnings {
				fmt.Fprintf(os.Stderr, "  - %s\n", warning)
			}
			return nil
		}
		// If no tools were registered at all, return an error with the warnings
		return fmt.Errorf("failed to register any MCP tools: %s", strings.Join(warnings, "; "))
	}

	return nil
}
