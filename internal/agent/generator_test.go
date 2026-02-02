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
		mcpState       *mcp.MCPState
	}{
		{
			name:           "code mode disabled with no servers",
			codeModeConfig: nil,
			mcpState:       mcp.NewMCPState(),
		},
		{
			name:           "code mode enabled with no servers registers execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: true},
			mcpState:       mcp.NewMCPState(),
		},
		{
			name:           "code mode enabled with empty excluded tools registers execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: true, ExcludedTools: []string{}},
			mcpState:       mcp.NewMCPState(),
		},
		{
			name:           "code mode disabled with no servers does not register execute_go_code",
			codeModeConfig: &config.CodeModeConfig{Enabled: false},
			mcpState:       mcp.NewMCPState(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := computeToolsToRegister(tt.codeModeConfig, tt.mcpState)
			cupaloy.SnapshotT(t, tools)
		})
	}
}

// computeToolsToRegister simulates the tool registration logic from CreateToolCapableGenerator
// and returns the names of tools that would be registered.
func computeToolsToRegister(
	codeModeConfig *config.CodeModeConfig,
	mcpState *mcp.MCPState,
) []string {
	var registeredTools []string

	codeModeEnabled := codeModeConfig != nil && codeModeConfig.Enabled

	if codeModeEnabled {
		var excludedToolNames []string
		if codeModeConfig.ExcludedTools != nil {
			excludedToolNames = codeModeConfig.ExcludedTools
		}

		_, excludedByServer := codemode.PartitionTools(mcpState, excludedToolNames)

		// execute_go_code is always registered when code mode is enabled
		registeredTools = append(registeredTools, codemode.ExecuteGoCodeToolName)

		// Add excluded tools
		for _, tools := range excludedByServer {
			for _, tool := range tools {
				registeredTools = append(registeredTools, tool.Name)
			}
		}
	} else {
		// Code mode disabled: register all tools normally
		for _, conn := range mcpState.Connections {
			for _, tool := range conn.Tools {
				registeredTools = append(registeredTools, tool.Name)
			}
		}
	}

	return registeredTools
}
