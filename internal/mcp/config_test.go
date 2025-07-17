package mcp

import (
	"os"
	"strings"
	"testing"

	"github.com/spachava753/gai"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  ConfigFile
		wantErr bool
	}{
		{
			name: "Valid stdio config",
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
					"test": {
						Type: "sse",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing command for stdio",
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
			config: ConfigFile{
				MCPServers: map[string]MCPServerConfig{},
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

func TestConfigFileIO(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "mcp-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}
	defer os.Chdir(originalDir)

	// Create example config
	if err := CreateExampleConfig(); err != nil {
		t.Fatalf("CreateExampleConfig() error = %v", err)
	}

	// Verify config exists
	if !ConfigExists() {
		t.Errorf("ConfigExists() returned false, expected true")
	}

	// Load the config
	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Verify config content - updated to match new example config
	if len(config.MCPServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(config.MCPServers))
	}

	if _, ok := config.MCPServers["filesystem"]; !ok {
		t.Errorf("Expected 'filesystem' server to exist")
	}

	if _, ok := config.MCPServers["shell"]; !ok {
		t.Errorf("Expected 'shell' server to exist")
	}

	// Check filesystem server with whitelist filtering
	fsServer := config.MCPServers["filesystem"]
	if fsServer.Timeout != 30 {
		t.Errorf("Expected filesystem timeout to be 30, got %d", fsServer.Timeout)
	}

	if fsServer.ToolFilter != "whitelist" {
		t.Errorf("Expected filesystem toolFilter to be 'whitelist', got %s", fsServer.ToolFilter)
	}

	if len(fsServer.EnabledTools) == 0 {
		t.Errorf("Expected filesystem server to have enabled tools")
	}

	// Check shell server with blacklist filtering
	shellServer := config.MCPServers["shell"]
	if shellServer.ToolFilter != "blacklist" {
		t.Errorf("Expected shell toolFilter to be 'blacklist', got %s", shellServer.ToolFilter)
	}

	if len(shellServer.DisabledTools) == 0 {
		t.Errorf("Expected shell server to have disabled tools")
	}
}

func TestToolFilterValidation(t *testing.T) {
	tests := []struct {
		name     string
		config   MCPServerConfig
		wantErr  bool
		errorMsg string
	}{
		{
			name: "Valid whitelist config",
			config: MCPServerConfig{
				Command:      "test",
				ToolFilter:   "whitelist",
				EnabledTools: []string{"tool1", "tool2"},
			},
			wantErr: false,
		},
		{
			name: "Valid blacklist config",
			config: MCPServerConfig{
				Command:       "test",
				ToolFilter:    "blacklist",
				DisabledTools: []string{"tool1", "tool2"},
			},
			wantErr: false,
		},
		{
			name: "Valid all config (default)",
			config: MCPServerConfig{
				Command: "test",
				// ToolFilter omitted, defaults to 'all'
			},
			wantErr: false,
		},
		{
			name: "Invalid: whitelist without enabled tools",
			config: MCPServerConfig{
				Command:    "test",
				ToolFilter: "whitelist",
			},
			wantErr:  true,
			errorMsg: "no enabledTools specified",
		},
		{
			name: "Invalid: blacklist without disabled tools",
			config: MCPServerConfig{
				Command:    "test",
				ToolFilter: "blacklist",
			},
			wantErr:  true,
			errorMsg: "no disabledTools specified",
		},
		{
			name: "Invalid: whitelist with disabled tools",
			config: MCPServerConfig{
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
			config: MCPServerConfig{
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
			config: MCPServerConfig{
				Command:      "test",
				ToolFilter:   "all",
				EnabledTools: []string{"tool1"},
			},
			wantErr:  true,
			errorMsg: "specifies enabledTools",
		},
		{
			name: "Invalid: all mode with disabled tools",
			config: MCPServerConfig{
				Command:       "test",
				ToolFilter:    "all",
				DisabledTools: []string{"tool1"},
			},
			wantErr:  true,
			errorMsg: "specifies disabledTools",
		},
		{
			name: "Invalid: unknown filter mode",
			config: MCPServerConfig{
				Command:    "test",
				ToolFilter: "invalid",
			},
			wantErr:  true,
			errorMsg: "invalid toolFilter",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := ConfigFile{
				MCPServers: map[string]MCPServerConfig{
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
	allTools := []gai.Tool{
		{Name: "read_file", Description: "Read a file"},
		{Name: "write_file", Description: "Write a file"},
		{Name: "list_directory", Description: "List directory contents"},
		{Name: "run_command", Description: "Run shell command"},
		{Name: "delete_file", Description: "Delete a file"},
	}

	tests := []struct {
		name          string
		config        MCPServerConfig
		expectedTools []string // Names of tools that should be included
		filteredOut   []string // Names of tools that should be filtered out
	}{
		{
			name: "All mode - no filtering",
			config: MCPServerConfig{
				ToolFilter: "all",
			},
			expectedTools: []string{"read_file", "write_file", "list_directory", "run_command", "delete_file"},
			filteredOut:   []string{},
		},
		{
			name:   "Default mode (empty filter) - no filtering",
			config: MCPServerConfig{
				// ToolFilter omitted
			},
			expectedTools: []string{"read_file", "write_file", "list_directory", "run_command", "delete_file"},
			filteredOut:   []string{},
		},
		{
			name: "Whitelist mode - only specific tools",
			config: MCPServerConfig{
				ToolFilter:   "whitelist",
				EnabledTools: []string{"read_file", "write_file", "list_directory"},
			},
			expectedTools: []string{"read_file", "write_file", "list_directory"},
			filteredOut:   []string{"run_command", "delete_file"},
		},
		{
			name: "Blacklist mode - exclude specific tools",
			config: MCPServerConfig{
				ToolFilter:    "blacklist",
				DisabledTools: []string{"run_command", "delete_file"},
			},
			expectedTools: []string{"read_file", "write_file", "list_directory"},
			filteredOut:   []string{"run_command", "delete_file"},
		},
		{
			name: "Whitelist mode - non-existent tool names are ignored",
			config: MCPServerConfig{
				ToolFilter:   "whitelist",
				EnabledTools: []string{"read_file", "non_existent_tool"},
			},
			expectedTools: []string{"read_file"},
			filteredOut:   []string{"write_file", "list_directory", "run_command", "delete_file"},
		},
		{
			name: "Blacklist mode - non-existent tool names are ignored",
			config: MCPServerConfig{
				ToolFilter:    "blacklist",
				DisabledTools: []string{"non_existent_tool"},
			},
			expectedTools: []string{"read_file", "write_file", "list_directory", "run_command", "delete_file"},
			filteredOut:   []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filteredTools, filteredOut := FilterToolsPublic(allTools, tc.config, "test-server")

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

func TestLoadConfigWithCustomPath(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a custom config file
	customConfigPath := tmpDir + "/custom_mcp.json"
	customConfig := `{
  "mcpServers": {
    "custom_server": {
      "command": "echo",
      "args": ["custom test"],
      "type": "stdio",
      "timeout": 10
    }
  }
}`

	if err := os.WriteFile(customConfigPath, []byte(customConfig), 0644); err != nil {
		t.Fatalf("Failed to create custom config file: %v", err)
	}

	// Test loading from custom path
	config, err := LoadConfig(customConfigPath)
	if err != nil {
		t.Fatalf("LoadConfig() with custom path error = %v", err)
	}

	if len(config.MCPServers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(config.MCPServers))
	}

	if _, ok := config.MCPServers["custom_server"]; !ok {
		t.Errorf("Expected 'custom_server' to exist")
	}

	// Test loading from non-existent path
	_, err = LoadConfig("/nonexistent/path.json")
	if err == nil {
		t.Error("Expected error when loading non-existent config file")
	}
}

func TestLoadConfigEmptyPath(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}
	defer os.Chdir(originalDir)

	// Test loading with empty path (should return empty config)
	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() with empty path error = %v", err)
	}

	if len(config.MCPServers) != 0 {
		t.Errorf("Expected 0 servers, got %d", len(config.MCPServers))
	}
}
