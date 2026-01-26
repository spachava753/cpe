package agent

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
)

func TestCodeModeToolRegistration(t *testing.T) {
	tests := []struct {
		name           string
		codeModeConfig *config.CodeModeConfig
		toolsByServer  map[string][]mcp.ToolData
		mcpServers     map[string]mcp.ServerConfig
	}{
		{
			name:           "code mode disabled with no servers",
			codeModeConfig: nil,
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
		},
		{
			name:           "code mode enabled with no servers registers execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: true},
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
		},
		{
			name:           "code mode enabled with empty excluded tools registers execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: true, ExcludedTools: []string{}},
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
		},
		{
			name:           "code mode disabled with no servers does not register execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: false},
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := computeToolsToRegister(tt.codeModeConfig, tt.toolsByServer, tt.mcpServers)
			cupaloy.SnapshotT(t, tools)
		})
	}
}

// computeToolsToRegister simulates the tool registration logic from CreateToolCapableGenerator
// and returns the names of tools that would be registered.
func computeToolsToRegister(
	codeModeConfig *config.CodeModeConfig,
	toolsByServer map[string][]mcp.ToolData,
	mcpServers map[string]mcp.ServerConfig,
) []string {
	var registeredTools []string

	codeModeEnabled := codeModeConfig != nil && codeModeConfig.Enabled

	if codeModeEnabled {
		var excludedToolNames []string
		if codeModeConfig.ExcludedTools != nil {
			excludedToolNames = codeModeConfig.ExcludedTools
		}

		_, excludedTools := codemode.PartitionTools(toolsByServer, mcpServers, excludedToolNames)

		// execute_go_code is always registered when code mode is enabled
		registeredTools = append(registeredTools, codemode.ExecuteGoCodeToolName)

		// Add excluded tools
		for _, toolData := range excludedTools {
			registeredTools = append(registeredTools, toolData.Tool.Name)
		}
	} else {
		// Code mode disabled: register all tools normally
		for _, tools := range toolsByServer {
			for _, toolData := range tools {
				registeredTools = append(registeredTools, toolData.Tool.Name)
			}
		}
	}

	return registeredTools
}
