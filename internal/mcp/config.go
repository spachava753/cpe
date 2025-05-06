package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFile represents the structure of the .cpemcp.json configuration file
type ConfigFile struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPServerConfig represents the configuration for a single MCP server
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Type    string            `json:"type,omitempty"` // Optional: "stdio" (default), "sse", or "http"
	URL     string            `json:"url,omitempty"`  // Required for "sse" and "http" types
	Timeout int               `json:"timeout,omitempty"` // Timeout in seconds (default: 60)
	Env     map[string]string `json:"env,omitempty"`    // Environment variables for stdio servers
}

// LoadConfig loads the MCP configuration from the .cpemcp.json file
func LoadConfig() (*ConfigFile, error) {
	// Look for config file in current directory
	configPath := ".cpemcp.json"
	
	// Check if the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Try in home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("could not determine home directory: %w", err)
		}
		
		configPath = filepath.Join(homeDir, ".cpemcp.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("MCP config file not found in current directory or home directory")
		}
	}
	
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}
	
	// Parse the JSON
	var config ConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}
	
	return &config, nil
}

// Validate checks if the configuration is valid
func (c *ConfigFile) Validate() error {
	if len(c.MCPServers) == 0 {
		return fmt.Errorf("no MCP servers defined in configuration")
	}
	
	for name, server := range c.MCPServers {
		// Validate type-specific requirements
		switch server.Type {
		case "", "stdio":
			// Command field is required for stdio servers
			if server.Command == "" {
				return fmt.Errorf("server %q with type 'stdio' requires a 'command' field", name)
			}
		case "sse", "http":
			if server.URL == "" {
				return fmt.Errorf("server %q with type %q requires a 'url' field", name, server.Type)
			}
		default:
			return fmt.Errorf("server %q has invalid type %q (must be 'stdio', 'sse', or 'http')", name, server.Type)
		}
		
		// Validate timeout if provided (must be positive)
		if server.Timeout < 0 {
			return fmt.Errorf("server %q has invalid timeout %d (must be a positive integer)", name, server.Timeout)
		}
		
		// Validate environment variables for stdio servers if provided
		if (server.Type == "" || server.Type == "stdio") && server.Env != nil {
			for k := range server.Env {
				if k == "" {
					return fmt.Errorf("server %q has an empty environment variable name", name)
				}
				// We don't validate the values, as empty values are allowed
			}
		} else if server.Type != "" && server.Type != "stdio" && server.Env != nil {
			return fmt.Errorf("server %q has type %q but specifies environment variables (only valid for stdio servers)", name, server.Type)
		}
	}
	
	return nil
}