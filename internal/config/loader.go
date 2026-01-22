package config

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dario.cat/mergo"
	"github.com/go-playground/validator/v10"
	"github.com/spachava753/gai"
	"gopkg.in/yaml.v3"
)

// LoadRawConfig loads raw config for commands that need to list/inspect all models
func LoadRawConfig(explicitPath string) (*RawConfig, error) {
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
	config, err := loadRawConfigFromFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	// Expand environment variables
	if err := config.expandEnvironmentVariables(); err != nil {
		return nil, fmt.Errorf("failed to expand environment variables: %w", err)
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
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

	userConfigPath := "~/.config/cpe/cpe.yaml"
	if userConfigDir != "" {
		userConfigPath = filepath.Join(userConfigDir, "cpe", "cpe.yaml")
	}
	return "", fmt.Errorf(`configuration file not found. Create one of:
  - ./cpe.yaml (current directory)
  - %s (user config directory)`, userConfigPath)
}

// loadRawConfigFromFile reads and parses a configuration file from any fs.File source.
// This design allows for flexible testing with in-memory files, embedded configs,
// network sources, or any other io.Reader implementation.
// The file extension is automatically detected from the file's stat info.
func loadRawConfigFromFile(file fs.File) (*RawConfig, error) {
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

	var config RawConfig

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
func (c *RawConfig) expandEnvironmentVariables() error {
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
		if model.CodeMode != nil && model.CodeMode.ExcludedTools != nil {
			expandedTools := make([]string, len(model.CodeMode.ExcludedTools))
			for j := range model.CodeMode.ExcludedTools {
				expandedTools[j] = os.ExpandEnv(model.CodeMode.ExcludedTools[j])
			}
			model.CodeMode.ExcludedTools = expandedTools
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

	// Expand in subagent configuration
	if c.Subagent != nil {
		c.Subagent.OutputSchemaPath = os.ExpandEnv(c.Subagent.OutputSchemaPath)
	}

	// Expand in defaults
	c.Defaults.SystemPromptPath = os.ExpandEnv(c.Defaults.SystemPromptPath)
	if c.Defaults.CodeMode != nil && c.Defaults.CodeMode.ExcludedTools != nil {
		expandedTools := make([]string, len(c.Defaults.CodeMode.ExcludedTools))
		for i := range c.Defaults.CodeMode.ExcludedTools {
			expandedTools[i] = os.ExpandEnv(c.Defaults.CodeMode.ExcludedTools[i])
		}
		c.Defaults.CodeMode.ExcludedTools = expandedTools
	}

	return nil
}

// Validate checks if the configuration is valid
func (c *RawConfig) Validate() error {
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

	// Validate subagent configuration if present
	if c.Subagent != nil {
		if err := c.validateSubagentConfig(); err != nil {
			return err
		}
	}

	// Validate auth_method and api_key_env for each model
	for _, m := range c.Models {
		if err := validateModelAuth(m); err != nil {
			return fmt.Errorf("model '%s': %w", m.Ref, err)
		}
	}

	return nil
}

// validateModelAuth validates auth_method constraints
func validateModelAuth(m ModelConfig) error {
	// oauth is only valid for anthropic
	if strings.ToLower(m.AuthMethod) == "oauth" && strings.ToLower(m.Type) != "anthropic" {
		return fmt.Errorf("auth_method 'oauth' is only supported for anthropic provider")
	}
	return nil
}

// validateSubagentConfig validates the subagent configuration
func (c *RawConfig) validateSubagentConfig() error {
	// If outputSchemaPath is set, verify the file exists and is valid JSON
	if c.Subagent.OutputSchemaPath != "" {
		if _, err := os.Stat(c.Subagent.OutputSchemaPath); os.IsNotExist(err) {
			return fmt.Errorf("subagent.outputSchemaPath: file does not exist: %s", c.Subagent.OutputSchemaPath)
		} else if err != nil {
			return fmt.Errorf("subagent.outputSchemaPath: error checking file: %w", err)
		}

		// Verify it's valid JSON
		data, err := os.ReadFile(c.Subagent.OutputSchemaPath)
		if err != nil {
			return fmt.Errorf("subagent.outputSchemaPath: error reading file: %w", err)
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			return fmt.Errorf("subagent.outputSchemaPath: invalid JSON schema: %w", err)
		}
	}
	return nil
}

// ResolveConfig loads the config file and resolves effective runtime configuration
// for the specified model with runtime options applied
func ResolveConfig(configPath string, opts RuntimeOptions) (*Config, error) {
	// Load raw config from file
	rawCfg, err := LoadRawConfig(configPath)
	if err != nil {
		return nil, err
	}

	// Determine which model to use
	modelRef := opts.ModelRef
	if modelRef == "" {
		if rawCfg.Defaults.Model != "" {
			modelRef = rawCfg.Defaults.Model
		} else {
			return nil, fmt.Errorf("no model specified. Set CPE_MODEL environment variable, use --model flag, or set defaults.model in configuration")
		}
	}

	// Find the model in configuration
	selectedModel, found := rawCfg.FindModel(modelRef)
	if !found {
		return nil, fmt.Errorf("model %q not found in configuration", modelRef)
	}

	// Resolve system prompt path with precedence: model-specific > global defaults
	systemPromptPath := selectedModel.SystemPromptPath
	if systemPromptPath == "" {
		systemPromptPath = rawCfg.Defaults.SystemPromptPath
	}

	// Merge generation parameters with precedence: CLI flags > Model-specific > Global defaults
	genParams := &gai.GenOpts{}

	// Start with global defaults
	if rawCfg.Defaults.GenerationParams != nil {
		globalGenOpts := rawCfg.Defaults.GenerationParams.ToGenOpts()
		if err := mergo.Merge(genParams, globalGenOpts, mergo.WithOverride); err != nil {
			return nil, err
		}
	}

	// Apply model-specific defaults
	if selectedModel.GenerationDefaults != nil {
		modelGenOpts := selectedModel.GenerationDefaults.ToGenOpts()
		if err := mergo.Merge(genParams, modelGenOpts, mergo.WithOverride); err != nil {
			return nil, err
		}
	}

	// Apply CLI overrides
	if opts.GenParams != nil {
		if err := mergo.Merge(genParams, opts.GenParams, mergo.WithOverride); err != nil {
			return nil, err
		}
	}

	// Resolve timeout
	timeout := 5 * time.Minute // default timeout
	if opts.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(opts.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout value %q: %w", opts.Timeout, err)
		}
		timeout = parsedTimeout
	} else if rawCfg.Defaults.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(rawCfg.Defaults.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid default timeout value %q: %w", rawCfg.Defaults.Timeout, err)
		}
		timeout = parsedTimeout
	}

	// Resolve code mode configuration with override behavior (not merge)
	// Model-level completely replaces defaults
	var codeMode *CodeModeConfig
	if selectedModel.CodeMode != nil {
		codeMode = selectedModel.CodeMode
	} else if rawCfg.Defaults.CodeMode != nil {
		codeMode = rawCfg.Defaults.CodeMode
	}

	return &Config{
		MCPServers:         rawCfg.MCPServers,
		Model:              selectedModel.Model,
		SystemPromptPath:   systemPromptPath,
		GenerationDefaults: genParams,
		Timeout:            timeout,
		CodeMode:           codeMode,
	}, nil
}
