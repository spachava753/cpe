package mcp

import (
	"fmt"
	"os"
)

// ConfigExists checks if an MCP configuration file exists
// Only looks in the current directory (not home directory)
// Supports both JSON and YAML formats (.cpemcp.json, .cpemcp.yaml, or .cpemcp.yml)
func ConfigExists() bool {
	// Define possible config file names
	configFileNames := []string{".cpemcp.json", ".cpemcp.yaml", ".cpemcp.yml"}

	// Check only in the current directory
	for _, fileName := range configFileNames {
		if _, err := os.Stat(fileName); err == nil {
			return true
		}
	}

	return false
}

// CreateExampleConfig creates example MCP configuration files in the current directory
// Creates both JSON and YAML examples (both .yaml and .yml extensions)
func CreateExampleConfig() error {
	// JSON example
	jsonExampleConfig := `{
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

	// YAML example
	yamlExampleConfig := `# MCP Configuration in YAML format
mcpServers:
  filesystem:
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /tmp
    timeout: 30
    env:
      NODE_ENV: production
  
  sqlite:
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-sqlite"
      - --db-path
      - /tmp/example.db
    timeout: 60
  
  remote-api:
    type: sse
    url: https://example.com/mcp
    timeout: 120
`

	// Create JSON example
	if err := os.WriteFile(".cpemcp.json", []byte(jsonExampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to create JSON example: %w", err)
	}

	// Create YAML example (.yaml extension)
	if err := os.WriteFile(".cpemcp.yaml", []byte(yamlExampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to create YAML example (.yaml): %w", err)
	}

	return nil
}
