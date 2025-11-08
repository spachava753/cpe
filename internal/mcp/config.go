package mcp

import (
	"fmt"
)

// Config represents the structure of the MCP configuration file
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers" yaml:"mcpServers"`
}

// ServerConfig represents the configuration for a single MCP server
type ServerConfig struct {
	Command       string            `json:"command" yaml:"command"`
	Args          []string          `json:"args" yaml:"args"`
	Type          string            `json:"type,omitempty" yaml:"type,omitempty"`                   // Optional: "stdio" (default), "sse", or "http"
	URL           string            `json:"url,omitempty" yaml:"url,omitempty"`                     // Required for "sse" and "http" types
	Timeout       int               `json:"timeout,omitempty" yaml:"timeout,omitempty"`             // Timeout in seconds (default: 60)
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`                     // Environment variables for stdio servers
	EnabledTools  []string          `json:"enabledTools,omitempty" yaml:"enabledTools,omitempty"`   // Whitelist approach
	DisabledTools []string          `json:"disabledTools,omitempty" yaml:"disabledTools,omitempty"` // Blacklist approach
	ToolFilter    string            `json:"toolFilter,omitempty" yaml:"toolFilter,omitempty"`       // "whitelist", "blacklist", or "all" (default)
}


// Validate checks if the configuration is valid
func (c *Config) Validate() error {
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

		// Validate tool filtering configuration
		if err := validateToolFilter(server, name); err != nil {
			return err
		}
	}

	return nil
}

// validateToolFilter validates the tool filtering configuration
func validateToolFilter(server ServerConfig, serverName string) error {
	// Normalize tool filter value
	toolFilter := server.ToolFilter
	if toolFilter == "" {
		toolFilter = "all" // Default value
	}

	// Validate tool filter mode
	switch toolFilter {
	case "all":
		// No restrictions - both EnabledTools and DisabledTools should be empty or ignored
		if len(server.EnabledTools) > 0 {
			return fmt.Errorf("server %q has toolFilter 'all' but specifies enabledTools (use 'whitelist' mode for enabledTools)", serverName)
		}
		if len(server.DisabledTools) > 0 {
			return fmt.Errorf("server %q has toolFilter 'all' but specifies disabledTools (use 'blacklist' mode for disabledTools)", serverName)
		}
	case "whitelist":
		// Only enabled tools should be specified
		if len(server.EnabledTools) == 0 {
			return fmt.Errorf("server %q has toolFilter 'whitelist' but no enabledTools specified", serverName)
		}
		if len(server.DisabledTools) > 0 {
			return fmt.Errorf("server %q has toolFilter 'whitelist' but also specifies disabledTools (use only enabledTools for whitelist mode)", serverName)
		}
	case "blacklist":
		// Only disabled tools should be specified
		if len(server.DisabledTools) == 0 {
			return fmt.Errorf("server %q has toolFilter 'blacklist' but no disabledTools specified", serverName)
		}
		if len(server.EnabledTools) > 0 {
			return fmt.Errorf("server %q has toolFilter 'blacklist' but also specifies enabledTools (use only disabledTools for blacklist mode)", serverName)
		}
	default:
		return fmt.Errorf("server %q has invalid toolFilter %q (must be 'all', 'whitelist', or 'blacklist')", serverName, toolFilter)
	}

	return nil
}
