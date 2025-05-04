package mcp

import (
	"os"
	"testing"
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
			wantErr: true,
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
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Verify config content
	if len(config.MCPServers) != 3 {
		t.Errorf("Expected 3 servers, got %d", len(config.MCPServers))
	}
	
	if _, ok := config.MCPServers["filesystem"]; !ok {
		t.Errorf("Expected 'filesystem' server to exist")
	}
	
	if _, ok := config.MCPServers["sqlite"]; !ok {
		t.Errorf("Expected 'sqlite' server to exist")
	}
	
	if _, ok := config.MCPServers["remote-api"]; !ok {
		t.Errorf("Expected 'remote-api' server to exist")
	}
	
	// Check new fields and omitted command for remote-api
	fsServer := config.MCPServers["filesystem"]
	if fsServer.Timeout != 30 {
		t.Errorf("Expected filesystem timeout to be 30, got %d", fsServer.Timeout)
	}
	
	if fsServer.Env == nil || fsServer.Env["NODE_ENV"] != "production" {
		t.Errorf("Expected filesystem server to have NODE_ENV set to production")
	}
	
	remoteServer := config.MCPServers["remote-api"]
	if remoteServer.Command != "" {
		t.Errorf("Expected remote-api server to have empty command, got %s", remoteServer.Command)
	}
}
