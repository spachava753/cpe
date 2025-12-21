package agent

import (
	"testing"

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
		wantToolCount  int
		wantTools      []string
	}{
		{
			name:           "code mode disabled with no servers",
			codeModeConfig: nil,
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
			wantToolCount:  0,
			wantTools:      []string{},
		},
		{
			name:           "code mode enabled with no servers registers execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: true},
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
			wantToolCount:  1,
			wantTools:      []string{codemode.ExecuteGoCodeToolName},
		},
		{
			name:           "code mode enabled with empty excluded tools registers execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: true, ExcludedTools: []string{}},
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
			wantToolCount:  1,
			wantTools:      []string{codemode.ExecuteGoCodeToolName},
		},
		{
			name:           "code mode disabled with no servers does not register execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: false},
			toolsByServer:  map[string][]mcp.ToolData{},
			mcpServers:     map[string]mcp.ServerConfig{},
			wantToolCount:  0,
			wantTools:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := computeToolsToRegister(tt.codeModeConfig, tt.toolsByServer, tt.mcpServers)

			if len(tools) != tt.wantToolCount {
				t.Errorf("got %d tools, want %d", len(tools), tt.wantToolCount)
			}

			for _, wantTool := range tt.wantTools {
				found := false
				for _, tool := range tools {
					if tool == wantTool {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected tool %q to be registered, but it wasn't. Got tools: %v", wantTool, tools)
				}
			}
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
