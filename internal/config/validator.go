package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
)

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
	if strings.ToLower(m.AuthMethod) == "oauth" && strings.ToLower(m.Type) != "anthropic" {
		return fmt.Errorf("auth_method 'oauth' is only supported for anthropic provider")
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
