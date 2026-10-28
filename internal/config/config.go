// Package config locates and loads runtime configuration.
package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

// Config represents the settings used for different behavior.
type Config struct {
	// MCPServers maps a server name to its configuration.
	MCPServers map[string]MCPServer `json:"mcpServers,omitempty"`
}

// MCPServer configures one MCP server. Its transport is stdio when Command is
// set and streamable HTTP when URL is set, unless Type says otherwise.
type MCPServer struct {
	// Enabled defaults to true; false removes the server from every session.
	Enabled *bool             `json:"enabled,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Type is "stdio", "http", or "sse"; empty infers it from Command or URL.
	Type    string `json:"type,omitempty"`
	Command string `json:"command,omitempty"`
	// Cwd is the absolute working directory of a stdio server.
	Cwd string `json:"cwd,omitempty"`
	URL string `json:"url,omitempty"`
	// Description overrides what the server says about itself in the system prompt.
	Description string   `json:"description,omitempty"`
	Args        []string `json:"args,omitempty"`
}

// IsEnabled reports whether the server takes part in sessions.
func (s MCPServer) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

const configPath = ".cpe"

// Path returns the directory where runtime configuration is stored
func Path() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(
		h,
		configPath,
	), nil
}

const settingsFile = "settings.json"

// LoadConfig reads settings.json relative to fsys. The caller selects the
// configuration directory, for example by passing os.DirFS after calling Path.
// Unknown fields are rejected so that a misspelled setting fails loudly.
func LoadConfig(fsys fs.FS) (Config, error) {
	f, err := fsys.Open(settingsFile)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var c Config
	if err := decoder.Decode(&c); err != nil {
		return Config{}, err
	}

	return c, nil
}
