package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spachava753/gai"
	"os"
	"strings"
)

// ToolCallback implements the gai.ToolCallback interface for MCP tools
type ToolCallback struct {
	ClientManager *ClientManager
	ServerName    string
	ToolName      string
}

// Call implements the gai.ToolCallback interface
func (c *ToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
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

	// Update the result message with the correct tool call ID
	// Since CallTool now returns a gai.Message, we need to update the IDs
	for i := range result.Blocks {
		result.Blocks[i].ID = toolCallID
	}

	return result, nil
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

	// For each server, get tools and register
	for _, serverName := range serverNames {
		// List tools (client is already initialized in GetClient)
		toolsResult, err := clientManager.ListTools(ctx, serverName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to list MCP tools for server %s: %v", serverName, err))
			// Skip this server but continue with others
			continue
		}

		// Register each tool
		for _, gaiTool := range toolsResult {
			// Check for duplicate tool names
			if existingServer, exists := registeredTools[gaiTool.Name]; exists {
				warnings = append(warnings, fmt.Sprintf(
					"skipping duplicate tool name '%s' in server '%s' (already registered from server '%s')",
					gaiTool.Name, serverName, existingServer))
				continue
			}

			// Create a callback for this tool
			callback := &ToolCallback{
				ClientManager: clientManager,
				ServerName:    serverName,
				ToolName:      gaiTool.Name,
			}

			// Register the tool with the callback
			err = toolRegisterer.Register(gaiTool, callback)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf(
					"failed to register MCP tool '%s' from server '%s': %v",
					gaiTool.Name, serverName, err))
				// Skip this tool but continue with others
				continue
			}

			// Track this tool to detect duplicates
			registeredTools[gaiTool.Name] = serverName
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
