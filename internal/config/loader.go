package config

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration with the following precedence:
// 1. Explicit config path (--config flag)
// 2. ./cpe.yaml or ./cpe.yml
// 3. ~/.config/cpe/cpe.yaml or ~/.config/cpe/cpe.yml
func LoadConfig(explicitPath string) (*Config, error) {
	var configPath string
	var err error

	if explicitPath != "" {
		// Use explicit path
		configPath = explicitPath
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("specified config file does not exist: %s", configPath)
		}
	} else {
		// Search for config file
		configPath, err = findConfigFile()
		if err != nil {
			return nil, fmt.Errorf("no configuration file found: %w", err)
		}
	}

	// Open and read config file
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file %s: %w", configPath, err)
	}
	defer file.Close()

	// Parse config file
	config, err := loadConfigFromFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Expand environment variables
	if err := config.expandEnvironmentVariables(); err != nil {
		return nil, fmt.Errorf("failed to expand environment variables: %w", err)
	}

	return config, nil
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
func loadConfigFromFile(file fs.File) (*Config, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// Get filename from file info for format detection
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("error getting file info: %w", err)
	}
	filename := stat.Name()

	var config Config

	// Determine format based on filename extension
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("error parsing JSON config: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("error parsing YAML config: %w", err)
		}
	default:
		// Try YAML first, then JSON as fallback
		yamlErr := yaml.Unmarshal(data, &config)
		if yamlErr != nil {
			jsonErr := json.Unmarshal(data, &config)
			if jsonErr != nil {
				return nil, fmt.Errorf("failed to parse config file: YAML error: %v, JSON error: %v", yamlErr, jsonErr)
			}
		}
	}

	return &config, nil
}

// expandEnvironmentVariables expands environment variables in configuration values
func (c *Config) expandEnvironmentVariables() error {
	// Expand in model configurations
	for i := range c.Models {
		model := &c.Models[i]
		model.BaseUrl = os.ExpandEnv(model.BaseUrl)
		model.ApiKeyEnv = os.ExpandEnv(model.ApiKeyEnv)
	}

	// Expand in MCP server configurations
	for name, server := range c.MCPServers {
		server.Command = os.ExpandEnv(server.Command)
		server.URL = os.ExpandEnv(server.URL)

		// Expand environment variables for the server
		if server.Env != nil {
			expandedEnv := make(map[string]string)
			for k, v := range server.Env {
				expandedEnv[os.ExpandEnv(k)] = os.ExpandEnv(v)
			}
			server.Env = expandedEnv
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
	// Check that we have at least one model
	if len(c.Models) == 0 {
		return fmt.Errorf("configuration must contain at least one model")
	}

	// Validate each model
	modelNames := make(map[string]bool)
	for _, model := range c.Models {
		// Check for duplicate model names
		if modelNames[model.Name] {
			return fmt.Errorf("duplicate model name: %s", model.Name)
		}
		modelNames[model.Name] = true

		// Validate model fields
		if model.Name == "" {
			return fmt.Errorf("model name cannot be empty")
		}
		if model.ID == "" {
			return fmt.Errorf("model %s: id cannot be empty", model.Name)
		}
		if model.Type == "" {
			return fmt.Errorf("model %s: type cannot be empty", model.Name)
		}

		// Validate model type
		validTypes := map[string]bool{
			"openai": true, "anthropic": true, "gemini": true, "responses": true,
			"groq": true, "cerebras": true,
		}
		if !validTypes[model.Type] {
			return fmt.Errorf("model %s: invalid type '%s', must be one of: openai, responses, anthropic, gemini, groq, cerebras", model.Name, model.Type)
		}

		// Validate reasoning configuration consistency
		if !model.SupportsReasoning && model.DefaultReasoningEffort != "" {
			return fmt.Errorf("model %s: has default_reasoning_effort but supports_reasoning is false", model.Name)
		}

		// Validate generation defaults if present
		if model.GenerationDefaults != nil {
			if err := validateGenerationParams(model.GenerationDefaults, fmt.Sprintf("model %s generation defaults", model.Name)); err != nil {
				return err
			}
		}
	}

	// Validate default model if specified
	if c.Defaults.Model != "" {
		if !modelNames[c.Defaults.Model] {
			return fmt.Errorf("defaults.model '%s' not found in models list", c.Defaults.Model)
		}
	}

	// Validate global generation defaults
	if c.Defaults.GenerationParams != nil {
		if err := validateGenerationParams(c.Defaults.GenerationParams, "global defaults"); err != nil {
			return err
		}
	}

	// Validate MCP servers
	if c.MCPServers != nil {
		for name, server := range c.MCPServers {
			// Use existing MCP validation logic
			tempConfig := struct {
				MCPServers map[string]interface{} `json:"mcpServers"`
			}{
				MCPServers: map[string]interface{}{name: server},
			}

			// Convert back to mcp.Config for validation
			tempData, err := json.Marshal(tempConfig)
			if err != nil {
				return fmt.Errorf("error validating MCP server %s: %w", name, err)
			}

			var mcpConfig struct {
				MCPServers map[string]interface{} `json:"mcpServers"`
			}
			if err := json.Unmarshal(tempData, &mcpConfig); err != nil {
				return fmt.Errorf("error validating MCP server %s: %w", name, err)
			}

			// Basic validation for MCP server
			if server.Type == "" || server.Type == "stdio" {
				if server.Command == "" {
					return fmt.Errorf("MCP server %s: command is required for stdio type", name)
				}
			} else if server.Type == "sse" || server.Type == "http" {
				if server.URL == "" {
					return fmt.Errorf("MCP server %s: url is required for %s type", name, server.Type)
				}
			}
		}
	}

	return nil
}

// validateGenerationParams validates generation parameters
func validateGenerationParams(params *GenerationParams, context string) error {
	if params.Temperature != nil {
		if *params.Temperature < 0 || *params.Temperature > 2 {
			return fmt.Errorf("%s: temperature must be between 0 and 2", context)
		}
	}

	if params.TopP != nil {
		if *params.TopP < 0 || *params.TopP > 1 {
			return fmt.Errorf("%s: topP must be between 0 and 1", context)
		}
	}

	if params.TopK != nil {
		if *params.TopK < 0 {
			return fmt.Errorf("%s: topK must be non-negative", context)
		}
	}

	if params.MaxTokens != nil {
		if *params.MaxTokens < 1 {
			return fmt.Errorf("%s: maxTokens must be positive", context)
		}
	}

	if params.FrequencyPenalty != nil {
		if *params.FrequencyPenalty < -2 || *params.FrequencyPenalty > 2 {
			return fmt.Errorf("%s: frequencyPenalty must be between -2 and 2", context)
		}
	}

	if params.PresencePenalty != nil {
		if *params.PresencePenalty < -2 || *params.PresencePenalty > 2 {
			return fmt.Errorf("%s: presencePenalty must be between -2 and 2", context)
		}
	}

	if params.NumberOfResponses != nil {
		if *params.NumberOfResponses < 1 {
			return fmt.Errorf("%s: numberOfResponses must be positive", context)
		}
	}

	return nil
}
