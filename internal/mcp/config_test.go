package mcp

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "Valid stdio config",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Command: "npx",
						Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Valid sse config without command",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Type: "sse",
						URL:  "http://localhost:3000",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Valid with timeout and env",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Command: "npx",
						Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
						Timeout: 30,
						Env: map[string]string{
							"NODE_ENV": "production",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Missing URL for sse",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Type: "sse",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing command for stdio",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Type: "stdio",
						Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid type",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Command: "npx",
						Type:    "invalid",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Negative timeout",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Command: "npx",
						Timeout: -10,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Env vars on non-stdio server",
			config: Config{
				MCPServers: map[string]ServerConfig{
					"test": {
						Type: "sse",
						URL:  "http://localhost:3000",
						Env: map[string]string{
							"NODE_ENV": "production",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Empty servers",
			config: Config{
				MCPServers: map[string]ServerConfig{},
			},
			wantErr: false, // Empty servers should be valid
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}


func TestToolFilterValidation(t *testing.T) {
	tests := []struct {
		name     string
		config   ServerConfig
		wantErr  bool
		errorMsg string
	}{
		{
			name: "Valid whitelist config",
			config: ServerConfig{
				Command:      "test",
				ToolFilter:   "whitelist",
				EnabledTools: []string{"tool1", "tool2"},
			},
			wantErr: false,
		},
		{
			name: "Valid blacklist config",
			config: ServerConfig{
				Command:       "test",
				ToolFilter:    "blacklist",
				DisabledTools: []string{"tool1", "tool2"},
			},
			wantErr: false,
		},
		{
			name: "Valid all config (default)",
			config: ServerConfig{
				Command: "test",
				// ToolFilter omitted, defaults to 'all'
			},
			wantErr: false,
		},
		{
			name: "Invalid: whitelist without enabled tools",
			config: ServerConfig{
				Command:    "test",
				ToolFilter: "whitelist",
			},
			wantErr:  true,
			errorMsg: "no enabledTools specified",
		},
		{
			name: "Invalid: blacklist without disabled tools",
			config: ServerConfig{
				Command:    "test",
				ToolFilter: "blacklist",
			},
			wantErr:  true,
			errorMsg: "no disabledTools specified",
		},
		{
			name: "Invalid: whitelist with disabled tools",
			config: ServerConfig{
				Command:       "test",
				ToolFilter:    "whitelist",
				EnabledTools:  []string{"tool1"},
				DisabledTools: []string{"tool2"},
			},
			wantErr:  true,
			errorMsg: "also specifies disabledTools",
		},
		{
			name: "Invalid: blacklist with enabled tools",
			config: ServerConfig{
				Command:       "test",
				ToolFilter:    "blacklist",
				EnabledTools:  []string{"tool1"},
				DisabledTools: []string{"tool2"},
			},
			wantErr:  true,
			errorMsg: "also specifies enabledTools",
		},
		{
			name: "Invalid: all mode with enabled tools",
			config: ServerConfig{
				Command:      "test",
				ToolFilter:   "all",
				EnabledTools: []string{"tool1"},
			},
			wantErr:  true,
			errorMsg: "specifies enabledTools",
		},
		{
			name: "Invalid: all mode with disabled tools",
			config: ServerConfig{
				Command:       "test",
				ToolFilter:    "all",
				DisabledTools: []string{"tool1"},
			},
			wantErr:  true,
			errorMsg: "specifies disabledTools",
		},
		{
			name: "Invalid: unknown filter mode",
			config: ServerConfig{
				Command:    "test",
				ToolFilter: "invalid",
			},
			wantErr:  true,
			errorMsg: "invalid toolFilter",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{
				MCPServers: map[string]ServerConfig{
					"test": tc.config,
				},
			}

			err := config.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr && err != nil && !strings.Contains(err.Error(), tc.errorMsg) {
				t.Errorf("Validate() error = %v, expected to contain %q", err, tc.errorMsg)
			}
		})
	}
}

func TestFilterTools(t *testing.T) {
	// Mock tools for testing
	allTools := []*mcp.Tool{
		{Name: "read_file", Description: "Read a file"},
		{Name: "write_file", Description: "Write a file"},
		{Name: "list_directory", Description: "List directory contents"},
		{Name: "run_command", Description: "Run shell command"},
		{Name: "delete_file", Description: "Delete a file"},
	}

	tests := []struct {
		name          string
		config        ServerConfig
		expectedTools []string // Names of tools that should be included
		filteredOut   []string // Names of tools that should be filtered out
	}{
		{
			name: "All mode - no filtering",
			config: ServerConfig{
				ToolFilter: "all",
			},
			expectedTools: []string{"read_file", "write_file", "list_directory", "run_command", "delete_file"},
			filteredOut:   []string{},
		},
		{
			name:   "Default mode (empty filter) - no filtering",
			config: ServerConfig{
				// ToolFilter omitted
			},
			expectedTools: []string{"read_file", "write_file", "list_directory", "run_command", "delete_file"},
			filteredOut:   []string{},
		},
		{
			name: "Whitelist mode - only specific tools",
			config: ServerConfig{
				ToolFilter:   "whitelist",
				EnabledTools: []string{"read_file", "write_file", "list_directory"},
			},
			expectedTools: []string{"read_file", "write_file", "list_directory"},
			filteredOut:   []string{"run_command", "delete_file"},
		},
		{
			name: "Blacklist mode - exclude specific tools",
			config: ServerConfig{
				ToolFilter:    "blacklist",
				DisabledTools: []string{"run_command", "delete_file"},
			},
			expectedTools: []string{"read_file", "write_file", "list_directory"},
			filteredOut:   []string{"run_command", "delete_file"},
		},
		{
			name: "Whitelist mode - non-existent tool names are ignored",
			config: ServerConfig{
				ToolFilter:   "whitelist",
				EnabledTools: []string{"read_file", "non_existent_tool"},
			},
			expectedTools: []string{"read_file"},
			filteredOut:   []string{"write_file", "list_directory", "run_command", "delete_file"},
		},
		{
			name: "Blacklist mode - non-existent tool names are ignored",
			config: ServerConfig{
				ToolFilter:    "blacklist",
				DisabledTools: []string{"non_existent_tool"},
			},
			expectedTools: []string{"read_file", "write_file", "list_directory", "run_command", "delete_file"},
			filteredOut:   []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filteredTools, filteredOut := FilterMcpTools(allTools, tc.config)

			// Check that expected tools are present
			if len(filteredTools) != len(tc.expectedTools) {
				t.Errorf("Expected %d filtered tools, got %d", len(tc.expectedTools), len(filteredTools))
			}

			for _, expectedTool := range tc.expectedTools {
				found := false
				for _, tool := range filteredTools {
					if tool.Name == expectedTool {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected tool %q to be in filtered tools", expectedTool)
				}
			}

			// Check that filtered out tools are correct
			if len(filteredOut) != len(tc.filteredOut) {
				t.Errorf("Expected %d filtered out tools, got %d", len(tc.filteredOut), len(filteredOut))
			}

			for _, expectedFiltered := range tc.filteredOut {
				found := false
				for _, toolName := range filteredOut {
					if toolName == expectedFiltered {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected tool %q to be in filtered out tools", expectedFiltered)
				}
			}
		})
}
}
