package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Validate checks if the configuration is valid.
// Relative paths in codeMode are validated relative to the current working directory.
func (c *RawConfig) Validate() error {
	return c.ValidateWithConfigPath("")
}

// ValidateWithConfigPath checks if the configuration is valid.
// Relative codeMode paths are validated relative to configFilePath when provided.
func (c *RawConfig) ValidateWithConfigPath(configFilePath string) error {
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

	if err := validateCodeModeConfig(c.Defaults.CodeMode, configFilePath, "defaults.codeMode"); err != nil {
		return err
	}

	// Validate auth_method and api_key_env for each model and model-level codeMode.
	for i, m := range c.Models {
		if err := validateModelAuth(m); err != nil {
			return fmt.Errorf("model '%s': %w", m.Ref, err)
		}

		if err := validateCodeModeConfig(m.CodeMode, configFilePath, fmt.Sprintf("models[%d].codeMode", i)); err != nil {
			return err
		}
	}

	return nil
}

func validateCodeModeConfig(codeMode *CodeModeConfig, configFilePath, fieldPrefix string) error {
	normalized, err := normalizeCodeModeConfigPaths(codeMode, configFilePath)
	if err != nil {
		return fmt.Errorf("%s: %w", fieldPrefix, err)
	}
	if normalized == nil {
		return nil
	}

	for i, modulePath := range normalized.LocalModulePaths {
		field := fmt.Sprintf("%s.localModulePaths[%d]", fieldPrefix, i)
		moduleStat, err := os.Stat(modulePath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%s: directory does not exist: %s", field, modulePath)
			}
			return fmt.Errorf("%s: %w", field, err)
		}
		if !moduleStat.IsDir() {
			return fmt.Errorf("%s: expected a directory, got file: %s", field, modulePath)
		}

		goModPath := filepath.Join(modulePath, "go.mod")
		goModStat, err := os.Stat(goModPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%s: missing go.mod in module directory: %s", field, goModPath)
			}
			return fmt.Errorf("%s: %w", field, err)
		}
		if goModStat.IsDir() {
			return fmt.Errorf("%s: expected go.mod file, got directory: %s", field, goModPath)
		}
	}

	return nil
}

// validateModelAuth validates auth_method constraints
func validateModelAuth(m ModelConfig) error {
	if strings.ToLower(m.AuthMethod) == "oauth" {
		modelType := strings.ToLower(m.Type)
		if modelType != "anthropic" && modelType != "responses" {
			return fmt.Errorf("auth_method 'oauth' is only supported for anthropic and responses providers")
		}
	}
	return nil
}

// validateSubagentConfig validates the subagent configuration
func (c *RawConfig) validateSubagentConfig() error {
	if c.Subagent.OutputSchemaPath == "" {
		return nil
	}

	// Read the file directly - os.ReadFile returns a clear error if file doesn't exist
	data, err := os.ReadFile(c.Subagent.OutputSchemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("subagent.outputSchemaPath: file does not exist: %s", c.Subagent.OutputSchemaPath)
		}
		return fmt.Errorf("subagent.outputSchemaPath: error reading file: %w", err)
	}

	// Verify it's valid JSON
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("subagent.outputSchemaPath: invalid JSON schema: %w", err)
	}
	return nil
}
