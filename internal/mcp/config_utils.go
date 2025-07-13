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
	// JSON example with tool filtering examples
	jsonExampleConfig := `{
  "mcpServers": {
    "filesystem": {
      "command": "pnpm",
      "args": ["dlx", "@modelcontextprotocol/server-filesystem", "."],
      "type": "stdio",
      "timeout": 30,
      "toolFilter": "whitelist",
      "enabledTools": [
        "read_file",
        "read_multiple_files",
        "write_file",
        "edit_file",
        "create_directory",
        "list_directory",
        "list_directory_with_sizes",
        "move_file",
        "search_files",
        "get_file_info"
      ]
    },
    "shell": {
      "command": "pnpm",
      "args": ["dlx", "mcp-shell"],
      "type": "stdio",
      "timeout": 60,
      "toolFilter": "blacklist",
      "disabledTools": [
        "system_restart",
        "system_shutdown",
        "rm_dangerous"
      ]
    }
  }
}`

	// Create JSON example
	if err := os.WriteFile(".cpemcp.json", []byte(jsonExampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to create JSON example: %w", err)
	}

	return nil
}
