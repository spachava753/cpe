package config

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration with the following precedence:
// 1. Explicit config path (--config flag)
// 2. ./cpe.yaml or ./cpe.yml
// 3. ~/.config/cpe/cpe.yaml or ~/.config/cpe/cpe.yml
func LoadConfig(explicitPath string) (Config, error) {
	var configPath string
	var err error

	if explicitPath != "" {
		// Use explicit path
		configPath = explicitPath
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return Config{}, fmt.Errorf("specified config file does not exist: %s", configPath)
		}
	} else {
		// Search for config file
		configPath, err = findConfigFile()
		if err != nil {
			return Config{}, fmt.Errorf("no configuration file found: %w", err)
		}
	}

	// Open and read config file
	file, err := os.Open(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("failed to open config file %s: %w", configPath, err)
	}
	defer file.Close()

	// Parse config file
	config, err := loadConfigFromFile(file)
	if err != nil {
		return Config{}, fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	// Expand environment variables
	if err := config.expandEnvironmentVariables(); err != nil {
		return Config{}, fmt.Errorf("failed to expand environment variables: %w", err)
	}

	// Validate the configuration
	validate := validator.New(validator.WithRequiredStructEnabled())

	return config, validate.Struct(config)
}

// findConfigFile searches for configuration files in the expected locations
func findConfigFile() (string, error) {
	// Define possible config file names in order of precedence
	configNames := []string{"cpe.yaml", "cpe.yml"}

	// Check current directory first
	for _, name := range configNames {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
	}

	// Check user config directory
	userConfigDir, err := os.UserConfigDir()
	if err == nil {
		cpeConfigDir := filepath.Join(userConfigDir, "cpe")
		for _, name := range configNames {
			path := filepath.Join(cpeConfigDir, name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf(`configuration file not found. Create one of:
  - ./cpe.yaml (current directory)
  - ~/.config/cpe/cpe.yaml (user config directory)`)
}

// loadConfigFromFile reads and parses a configuration file from any fs.File source.
// This design allows for flexible testing with in-memory files, embedded configs,
// network sources, or any other io.Reader implementation.
// The file extension is automatically detected from the file's stat info.
func loadConfigFromFile(file fs.File) (Config, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return Config{}, fmt.Errorf("error reading config file: %w", err)
	}

	// Get filename from file info for format detection
	stat, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("error getting file info: %w", err)
	}
	filename := stat.Name()

	var config Config

	// Determine format based on filename extension
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &config); err != nil {
			return Config{}, fmt.Errorf("error parsing JSON config: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			return Config{}, fmt.Errorf("error parsing YAML config: %w", err)
		}
	default:
		// Try YAML first, then JSON as fallback
		yamlErr := yaml.Unmarshal(data, &config)
		if yamlErr != nil {
			jsonErr := json.Unmarshal(data, &config)
			if jsonErr != nil {
				return Config{}, fmt.Errorf("failed to parse config file: YAML error: %v, JSON error: %v", yamlErr, jsonErr)
			}
		}
	}

	return config, nil
}

// expandEnvironmentVariables expands environment variables in configuration values
func (c *Config) expandEnvironmentVariables() error {
	// Expand in model configurations
	for i := range c.Models {
		model := &c.Models[i]
		model.BaseUrl = os.ExpandEnv(model.BaseUrl)
		model.ApiKeyEnv = os.ExpandEnv(model.ApiKeyEnv)
		model.SystemPromptPath = os.ExpandEnv(model.SystemPromptPath)
		if model.PatchRequest != nil && model.PatchRequest.IncludeHeaders != nil {
			expandedHeaders := make(map[string]string)
			for h, v := range model.PatchRequest.IncludeHeaders {
				expandedHeaders[os.ExpandEnv(h)] = os.ExpandEnv(v)
			}
			model.PatchRequest.IncludeHeaders = expandedHeaders
		}
	}

	// Expand in MCP server configurations
	for name, server := range c.MCPServers {
		server.Command = os.ExpandEnv(server.Command)
		if server.Args != nil {
			expandedArgs := make([]string, len(server.Args))
			for i := range server.Args {
				expandedArgs[i] = os.ExpandEnv(server.Args[i])
			}
			server.Args = expandedArgs
		}
		server.URL = os.ExpandEnv(server.URL)

		// Expand environment variables for the server
		if server.Env != nil {
			expandedEnv := make(map[string]string)
			for k, v := range server.Env {
				expandedEnv[os.ExpandEnv(k)] = os.ExpandEnv(v)
			}
			server.Env = expandedEnv
		}

		// Expand environment variables in custom headers
		if server.Headers != nil {
			expandedHeaders := make(map[string]string)
			for k, v := range server.Headers {
				expandedHeaders[os.ExpandEnv(k)] = os.ExpandEnv(v)
			}
			server.Headers = expandedHeaders
		}

		// Expand command arguments
		for i, arg := range server.Args {
			server.Args[i] = os.ExpandEnv(arg)
		}

		c.MCPServers[name] = server
	}

	// Expand in defaults
	c.Defaults.SystemPromptPath = os.ExpandEnv(c.Defaults.SystemPromptPath)

	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("invalid configuration file: %w", err)
	}

	// Validate default model if specified
	if c.Defaults.Model != "" {
		if _, found := c.FindModel(c.Defaults.Model); !found {
			return fmt.Errorf("defaults.model '%s' not found in models list", c.Defaults.Model)
		}
	}

	return nil
}
