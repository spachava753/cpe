package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile represents the structure of the MCP configuration file
type ConfigFile struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers" yaml:"mcpServers"`
}

// MCPServerConfig represents the configuration for a single MCP server
type MCPServerConfig struct {
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args" yaml:"args"`
	Type    string            `json:"type,omitempty" yaml:"type,omitempty"`       // Optional: "stdio" (default), "sse", or "http"
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`         // Required for "sse" and "http" types
	Timeout int               `json:"timeout,omitempty" yaml:"timeout,omitempty"` // Timeout in seconds (default: 60)
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`         // Environment variables for stdio servers
}

// LoadConfig loads the MCP configuration from either .cpemcp.json or .cpemcp.yaml/.cpemcp.yml file
// If no config file is found, it returns an empty configuration instead of an error
// If a config file exists but has reading or parsing errors, it returns an error
func LoadConfig() (*ConfigFile, error) {
	// Define possible config file names in order of precedence
	configFileNames := []string{".cpemcp.json", ".cpemcp.yaml", ".cpemcp.yml"}
	configPath := ""

	// Look only in the current directory
	for _, fileName := range configFileNames {
		if _, err := os.Stat(fileName); err == nil {
			configPath = fileName
			break
		}
	}

	// If no config file found, return default config with CPE as a stdio server
	if configPath == "" {
		// Get the current executable path instead of hardcoding "cpe"
		execPath, err := os.Executable()
		if err != nil {
			// Fallback to "cpe" if we can't determine the executable path
			execPath = "cpe"
		}

		defaultConfig := &ConfigFile{MCPServers: make(map[string]MCPServerConfig)}
		// Add CPE's own serve command as an MCP server by default
		defaultConfig.MCPServers["cpe_native_tools"] = MCPServerConfig{
			Command: execPath,
			Args:    []string{"mcp", "serve"},
			Type:    "stdio",
			Timeout: 60,
		}
		return defaultConfig, nil
	}

	// Read the config file, return error if reading fails
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file %s: %w", configPath, err)
	}

	// Parse the file based on its extension
	var config ConfigFile
	var parseErr error

	if strings.HasSuffix(configPath, ".json") {
		// Parse JSON
		parseErr = json.Unmarshal(data, &config)
	} else if strings.HasSuffix(configPath, ".yaml") || strings.HasSuffix(configPath, ".yml") {
		// Parse YAML
		parseErr = yaml.Unmarshal(data, &config)
	} else {
		// Try JSON first, then YAML as a fallback
		jsonErr := json.Unmarshal(data, &config)
		if jsonErr != nil {
			yamlErr := yaml.Unmarshal(data, &config)
			if yamlErr != nil {
				// Both parsing attempts failed
				return nil, fmt.Errorf("failed to parse %s: JSON error: %v, YAML error: %v",
					configPath, jsonErr, yamlErr)
			}
		}
	}

	// If parsing failed, return error
	if parseErr != nil {
		return nil, fmt.Errorf("error parsing config file %s: %w", configPath, parseErr)
	}

	// Initialize the map if it's nil
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]MCPServerConfig)
	}

	return &config, nil
}

// Validate checks if the configuration is valid
func (c *ConfigFile) Validate() error {
	// Check each server configuration if any exist
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
