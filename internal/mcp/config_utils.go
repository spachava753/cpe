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
	// Get the current executable path instead of hardcoding "cpe"
	execPath, err := os.Executable()
	if err != nil {
		// Fallback to "cpe" if we can't determine the executable path
		execPath = "cpe"
	}

	// JSON example
	jsonExampleConfig := fmt.Sprintf(`{
  "mcpServers": {
    "cpe_native_tools": {
      "command": "%s",
      "args": ["mcp", "serve"],
      "type": "stdio",
      "timeout": 60
    }
  }
}`, execPath)

	// YAML example
	yamlExampleConfig := fmt.Sprintf(`# MCP Configuration in YAML format
mcpServers:
  # Native CPE tools available as an MCP server
  cpe_native_tools:
    command: %s
    args:
      - mcp
      - serve
    type: stdio
    timeout: 60
  
  # Example of an external MCP server (commented out)
  # filesystem:
  #   command: npx
  #   args:
  #     - -y
  #     - "@modelcontextprotocol/server-filesystem"
  #     - /tmp
  #   timeout: 30
  #   env:
  #     NODE_ENV: production
  
  # Example of an SSE MCP server (commented out)
  # remote-api:
  #   type: sse
  #   url: https://example.com/mcp
  #   timeout: 120
`, execPath)

	// Create JSON example
	if err := os.WriteFile(".cpemcp.json", []byte(jsonExampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to create JSON example: %w", err)
	}

	// Create YAML example (.yaml extension)
	if err := os.WriteFile(".cpemcp.yaml", []byte(yamlExampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to create YAML example (.yaml): %w", err)
	}

	// Create YAML example (.yml extension) - same content as .yaml
	if err := os.WriteFile(".cpemcp.yml", []byte(yamlExampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to create YAML example (.yml): %w", err)
	}

	return nil
}
