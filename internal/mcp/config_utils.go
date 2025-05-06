package mcp

import (
	"os"
	"path/filepath"
)

// ConfigExists checks if an MCP configuration file exists
// It looks in the current directory and the user's home directory
func ConfigExists() bool {
	// Check current directory
	if _, err := os.Stat(".cpemcp.json"); err == nil {
		return true
	}
	
	// Check home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	
	configPath := filepath.Join(homeDir, ".cpemcp.json")
	if _, err := os.Stat(configPath); err == nil {
		return true
	}
	
	return false
}

// CreateExampleConfig creates an example MCP configuration file in the current directory
func CreateExampleConfig() error {
	exampleConfig := `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-filesystem",
        "/tmp"
      ],
      "timeout": 30,
      "env": {
        "NODE_ENV": "production"
      }
    },
    "sqlite": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-sqlite",
        "--db-path",
        "/tmp/example.db"
      ],
      "timeout": 60
    },
    "remote-api": {
      "type": "sse",
      "url": "https://example.com/mcp",
      "timeout": 120
    }
  }
}`

	return os.WriteFile(".cpemcp.json", []byte(exampleConfig), 0644)
}