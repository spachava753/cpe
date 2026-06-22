package config

import (
	"fmt"
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
		if err := validateThinkingValues(m.ThinkingValues); err != nil {
			return fmt.Errorf("model %q: %w", m.Ref, err)
		}
	}

	return nil
}

func validateSelectedProfile(model ModelConfig) error {
	if err := validateMCPServerConfigs(model.MCPServers, "mcpServers"); err != nil {
		return err
	}
	return nil
}

// validateModelAuth validates provider-specific authentication constraints.
// Anthropic-on-Vertex uses Google ADC and IAM instead of CPE's API-key/OAuth paths.
func validateModelAuth(m ModelConfig) error {
	modelType := strings.ToLower(m.Type)
	authMethod := strings.ToLower(strings.TrimSpace(m.AuthMethod))

	if modelType == "anthropic_vertex" {
		return validateAnthropicVertexModel(m)
	}
	if m.Vertex != nil {
		return fmt.Errorf("vertex configuration is only supported for anthropic_vertex models")
	}
	if authMethod == "oauth" {
		if modelType != "anthropic" && modelType != "responses" {
			return fmt.Errorf("auth_method 'oauth' is only supported for anthropic and responses providers")
		}
		return nil
	}
	if strings.TrimSpace(m.ApiKeyEnv) == "" {
		return fmt.Errorf("api_key_env is required unless auth_method is oauth or type is anthropic_vertex")
	}
	if m.ApiKeyEnv != strings.TrimSpace(m.ApiKeyEnv) {
		return fmt.Errorf("api_key_env must not have leading or trailing whitespace")
	}
	return nil
}

func validateAnthropicVertexModel(m ModelConfig) error {
	if strings.TrimSpace(m.AuthMethod) != "" {
		return fmt.Errorf("auth_method is not supported for anthropic_vertex models; Google Application Default Credentials are used")
	}
	if strings.TrimSpace(m.ApiKeyEnv) != "" {
		return fmt.Errorf("api_key_env is not supported for anthropic_vertex models; Google Application Default Credentials are used")
	}
	if strings.TrimSpace(m.BaseUrl) != "" {
		return fmt.Errorf("base_url is not supported for anthropic_vertex models; configure vertex.region instead")
	}
	if m.Vertex == nil {
		return fmt.Errorf("vertex configuration is required for anthropic_vertex models")
	}
	if strings.TrimSpace(m.Vertex.ProjectID) == "" {
		return fmt.Errorf("vertex.project_id is required for anthropic_vertex models")
	}
	if m.Vertex.ProjectID != strings.TrimSpace(m.Vertex.ProjectID) {
		return fmt.Errorf("vertex.project_id must not have leading or trailing whitespace")
	}
	if strings.TrimSpace(m.Vertex.Region) == "" {
		return fmt.Errorf("vertex.region is required for anthropic_vertex models")
	}
	if m.Vertex.Region != strings.TrimSpace(m.Vertex.Region) {
		return fmt.Errorf("vertex.region must not have leading or trailing whitespace")
	}
	for i, scope := range m.Vertex.Scopes {
		if strings.TrimSpace(scope) == "" {
			return fmt.Errorf("vertex.scopes[%d] must not be empty", i)
		}
		if scope != strings.TrimSpace(scope) {
			return fmt.Errorf("vertex.scopes[%d] must not have leading or trailing whitespace", i)
		}
	}
	return nil
}

func validateThinkingValues(values []ThinkingValueConfig) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		trimmedValue := strings.TrimSpace(value.Value)
		if trimmedValue == "" {
			return fmt.Errorf("thinkingValues[%d].value must not be empty", i)
		}
		if value.Value != trimmedValue {
			return fmt.Errorf("thinkingValues[%d].value must not have leading or trailing whitespace", i)
		}
		if _, ok := seen[trimmedValue]; ok {
			return fmt.Errorf("thinkingValues contains duplicate value: %s", trimmedValue)
		}
		seen[trimmedValue] = struct{}{}
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
		}
	}
	return nil
}
