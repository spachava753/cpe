package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

// Validate enforces structural schema and semantic invariants for RawConfig.
func (c *RawConfig) Validate() error {
	return c.ValidateWithConfigPath("")
}

// ValidateWithConfigPath enforces file-level validation that must hold for the
// whole config regardless of which model profile is selected. Runtime checks for
// selected-profile MCP transport constraints and codeMode filesystem paths are
// performed during resolution.
func (c *RawConfig) ValidateWithConfigPath(configFilePath string) error {
	_ = configFilePath

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("invalid configuration file: %w", err)
	}

	for _, m := range c.Models {
		if err := validateModelAuth(m); err != nil {
			return fmt.Errorf("model %q: %w", m.Ref, err)
		}
	}

	return nil
}

func validateSelectedProfile(model ModelConfig, configFilePath string) error {
	if err := validateMCPServerConfigs(model.MCPServers, "mcpServers"); err != nil {
		return err
	}
	if err := validateCodeModeConfig(model.CodeMode, configFilePath, "codeMode"); err != nil {
		return err
	}
	return nil
}

// validateCodeModeConfig validates normalized localModulePaths and enforces
// module directory invariants (existing directory with a go.mod file).
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

func validateMCPServerConfigs(servers map[string]mcpconfig.ServerConfig, fieldPrefix string) error {
	for name, server := range servers {
		field := fieldPrefix + "." + name
		switch server.Type {
		case "":
			if server.URL != "" {
				return fmt.Errorf("%s.type: required when url is set; use \"http\" or \"sse\"", field)
			}
			if server.Command == "" {
				return fmt.Errorf("%s.command: required for type \"stdio\"", field)
			}
			if len(server.Headers) > 0 {
				return fmt.Errorf("%s.headers: only supported for type \"http\" or \"sse\"", field)
			}
		case "stdio":
			if server.Command == "" {
				return fmt.Errorf("%s.command: required for type \"stdio\"", field)
			}
		case "http", "sse":
			if server.Command != "" {
				return fmt.Errorf("%s.command: only supported for type \"stdio\"", field)
			}
			if len(server.Args) > 0 {
				return fmt.Errorf("%s.args: only supported for type \"stdio\"", field)
			}
		case "builtin":
			if server.Command != "" {
				return fmt.Errorf("%s.command: not supported for type \"builtin\"", field)
			}
			if len(server.Args) > 0 {
				return fmt.Errorf("%s.args: not supported for type \"builtin\"", field)
			}
			if server.URL != "" {
				return fmt.Errorf("%s.url: not supported for type \"builtin\"", field)
			}
			if len(server.Headers) > 0 {
				return fmt.Errorf("%s.headers: not supported for type \"builtin\"", field)
			}
			if len(server.Env) > 0 {
				return fmt.Errorf("%s.env: not supported for type \"builtin\"", field)
			}
		}
	}
	return nil
}
