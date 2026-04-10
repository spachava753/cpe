package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

// Validate enforces schema and semantic invariants for RawConfig.
//
// It is equivalent to ValidateWithConfigPath(""): path-based fields are
// interpreted relative to the current working directory.
func (c *RawConfig) Validate() error {
	return c.ValidateWithConfigPath("")
}

// ValidateWithConfigPath enforces both structural validation tags and
// cross-field invariants that require lookup or filesystem checks.
//
// Invariants validated here include:
//   - defaults.model references an existing entry in models.
//   - auth_method provider constraints for each model.
//   - defaults.codeMode and model.codeMode path normalization + module checks.
//   - optional subagent.outputSchemaPath exists and contains valid JSON.
//   - MCP server transport defaults and transport-specific field constraints.
//
// When configFilePath is provided, relative codeMode paths and subagent schema
// paths are interpreted from that config file directory before existence checks.
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

	if err := validateMCPServerConfigs(c.MCPServers); err != nil {
		return err
	}

	// Validate subagent configuration if present
	if c.Subagent != nil {
		if err := c.validateSubagentConfig(configFilePath); err != nil {
			return err
		}
	}

	if err := validateCodeModeConfig(c.Defaults.CodeMode, configFilePath, "defaults.codeMode"); err != nil {
		return err
	}
	if err := validateCompactionConfig(c.Defaults.Compaction, "defaults.compaction"); err != nil {
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
		if err := validateCompactionConfig(m.Compaction, fmt.Sprintf("models[%d].compaction", i)); err != nil {
			return err
		}
	}

	return nil
}

// validateCodeModeConfig validates normalized localModulePaths and enforces
// module directory invariants (existing directory with a go.mod file).
// fieldPrefix is used to produce location-aware validation errors.
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

// validateModelAuth validates provider-specific auth_method constraints.
// Currently, auth_method=oauth is restricted to anthropic and responses ports.
func validateModelAuth(m ModelConfig) error {
	if strings.ToLower(m.AuthMethod) == "oauth" {
		modelType := strings.ToLower(m.Type)
		if modelType != "anthropic" && modelType != "responses" {
			return fmt.Errorf("auth_method 'oauth' is only supported for anthropic and responses providers")
		}
	}
	return nil
}

func validateMCPServerConfigs(servers map[string]mcpconfig.ServerConfig) error {
	for name, server := range servers {
		if server.Type == "" {
			if server.URL != "" {
				return fmt.Errorf("mcpServers.%s.type: required when url is set; use \"http\" or \"sse\"", name)
			}
			if len(server.Headers) > 0 {
				return fmt.Errorf("mcpServers.%s.headers: only supported for type \"http\" or \"sse\"", name)
			}
			continue
		}

		if server.Type == "http" || server.Type == "sse" {
			if server.Command != "" {
				return fmt.Errorf("mcpServers.%s.command: only supported for type \"stdio\"", name)
			}
			if len(server.Args) > 0 {
				return fmt.Errorf("mcpServers.%s.args: only supported for type \"stdio\"", name)
			}
		}
	}
	return nil
}

// validateSubagentConfig validates optional subagent output schema wiring.
// When outputSchemaPath is set, the target file must exist and parse as JSON.
func (c *RawConfig) validateSubagentConfig(configFilePath string) error {
	if c.Subagent.OutputSchemaPath == "" {
		return nil
	}

	schemaPath := c.Subagent.OutputSchemaPath
	if configFilePath != "" && !filepath.IsAbs(schemaPath) {
		schemaPath = filepath.Join(filepath.Dir(configFilePath), schemaPath)
	}

	data, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("subagent.outputSchemaPath: file does not exist: %s", schemaPath)
		}
		return fmt.Errorf("subagent.outputSchemaPath: error reading file: %w", err)
	}

	// Verify it's valid JSON. JSON Schema may be either an object or a boolean.
	var schema any
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("subagent.outputSchemaPath: invalid JSON schema: %w", err)
	}
	return nil
}
