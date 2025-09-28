# Product Requirements Document: Unified Configuration File

## Executive Summary

This PRD outlines the consolidation of multiple configuration sources (model catalog JSON, MCP config file, and CLI flags) into a single unified YAML configuration file. This simplification will reduce configuration complexity and improve user experience by providing a single source of truth for CPE configuration.

## Background and Problem Statement

### Current State

CPE currently requires users to manage configuration across multiple sources:

1. **Model Catalog** (`examples/models.json`): JSON file containing model definitions loaded via `--model-catalog` flag
   - Model properties: name, id, type, base_url, api_key_env, context_window, max_output, costs
   - Loaded through `internal/modelcatalog/` package

2. **MCP Config** (`.cpemcp.json`/`.cpemcp.yaml`): MCP server configurations loaded via `--mcp-config` flag or auto-detected
   - Server properties: command, args, type, url, timeout, env, tool filtering
   - Loaded through `internal/mcp/` package

3. **CLI Flags** (`cmd/root.go`): Various runtime flags
   - Generation parameters: temperature, top-p, top-k, max-tokens
   - System prompt: --system-prompt-file
   - Other flags: timeout, no-stream, model selection

### Pain Points

- **Configuration fragmentation**: Users must manage 2-3 separate configuration files plus CLI flags
- **Cognitive overhead**: Understanding which settings go where requires documentation lookup
- **Deployment complexity**: Distributing configurations requires multiple files
- **Limited defaults**: Cannot preset common generation parameters per model
- **Inconsistent formats**: JSON for models, JSON/YAML for MCP, CLI flags for runtime

## Goals and Outcomes

### Goals

1. Unify all configuration options into a single YAML file
2. Support hierarchical configuration with sensible defaults and overrides
3. Maintain backwards compatibility during transition (optional)
4. Enable per-model default generation parameters
5. Simplify configuration management and distribution

### Outcomes

- Users can configure CPE entirely through a single `cpe.yaml` file
- Reduced onboarding friction for new users
- Easier sharing of complete configurations
- Better support for team/organization standardization
- Cleaner separation between configuration and runtime parameters

## Requirements

### Functional Requirements

1. **Unified Config Structure**
   - Support YAML format (with JSON as optional format)
   - Include sections for: mcpServers, models, defaults
   - Allow field-level overrides via CLI flags

2. **Model Configuration**
   - All existing model fields from `modelcatalog.Model`
   - Additional per-model generation defaults (temperature, top-p, top-k, thinking-budget)
   - Support for custom headers/auth per model

3. **MCP Server Configuration**
   - All existing fields from `mcp.ServerConfig`
   - Maintain current functionality for tool filtering

4. **Default Settings**
   - System prompt path with template support
   - Default model selection
   - Global generation parameter defaults

5. **Configuration Loading**
   - Search order: explicit path → ./cpe.yaml → ~/.config/cpe/cpe.yaml
   - CLI flags override config file values
   - Environment variable expansion in config

6. **Flag Removal**
   - **BREAKING**: Remove `--model-catalog` flag entirely
   - **BREAKING**: Remove `--mcp-config` flag entirely
   - Models and MCP servers must be defined in unified config

7. **Validation**
   - Schema validation on load
   - Clear error messages for misconfigurations
   - Optional strict mode to fail on unknown fields

### Non-Functional Requirements

1. **Performance**: Config loading should not noticeably impact startup time (<100ms)
2. **Extensibility**: Design should accommodate future configuration needs
3. **Documentation**: Comprehensive examples and usage guide
4. **Testing**: Unit tests for all configuration scenarios

## Technical Design

### Config Structure

```go
// internal/config/config.go
package config

import (
    "github.com/spachava753/cpe/internal/modelcatalog"
    "github.com/spachava753/cpe/internal/mcp"
)

// Config represents the unified configuration structure
type Config struct {
    // MCP server configurations
    MCPServers map[string]mcp.ServerConfig `yaml:"mcpServers,omitempty"`
    
    // Model definitions
    Models []ModelConfig `yaml:"models"`
    
    // Default settings
    Defaults DefaultConfig `yaml:"defaults,omitempty"`
    
    // Version for future compatibility
    Version string `yaml:"version,omitempty"`
}

// ModelConfig extends the base model with generation defaults
type ModelConfig struct {
    modelcatalog.Model `yaml:",inline"`
    
    // Generation parameter defaults for this model
    GenerationDefaults *GenerationParams `yaml:"generationDefaults,omitempty"`
}

// GenerationParams holds generation parameters
type GenerationParams struct {
    Temperature      *float64 `yaml:"temperature,omitempty"`
    TopP            *float64 `yaml:"topP,omitempty"`
    TopK            *int     `yaml:"topK,omitempty"`
    MaxTokens       *int     `yaml:"maxTokens,omitempty"`
    ThinkingBudget  *string  `yaml:"thinkingBudget,omitempty"`
    FrequencyPenalty *float64 `yaml:"frequencyPenalty,omitempty"`
    PresencePenalty  *float64 `yaml:"presencePenalty,omitempty"`
}

// DefaultConfig holds global defaults
type DefaultConfig struct {
    // Path to system prompt template file
    SystemPromptPath string `yaml:"systemPromptPath,omitempty"`
    
    // Default model to use if not specified
    Model string `yaml:"model,omitempty"`
    
    // Global generation parameter defaults
    GenerationParams *GenerationParams `yaml:"generationParams,omitempty"`
    
    // Request timeout
    Timeout string `yaml:"timeout,omitempty"`
    
    // Disable streaming globally
    NoStream bool `yaml:"noStream,omitempty"`
}
```

### Configuration Loading Logic

```go
// internal/config/loader.go
package config

// LoadConfig loads configuration with the following precedence:
// 1. Explicit config path (--config flag)
// 2. ./cpe.yaml or ./cpe.yml
// 3. ~/.config/cpe/cpe.yaml or ~/.config/cpe/cpe.yml
func LoadConfig(explicitPath string) (*Config, error) {
    // Implementation details...
}

// MergeWithFlags overlays CLI flag values onto config
func (c *Config) MergeWithFlags(flags Flags) *Config {
    // CLI flags take precedence over config file
}
```

### Example Configuration File

```yaml
# cpe.yaml - Unified CPE Configuration
version: "1.0"

# MCP server definitions
mcpServers:
  editor:
    command: "mcp-server-editor"
    type: stdio
    timeout: 60
    toolFilter: whitelist
    enabledTools:
      - text_edit
      - shell
    env:
      EDITOR: "vim"

  filesystem:
    command: "mcp-server-filesystem"
    args: ["/home/user/projects"]
    toolFilter: all

# Model definitions with optional generation defaults
models:
  - name: "gpt5"
    id: "gpt-5"
    type: "openai"
    base_url: "https://api.openai.com/v1/"
    api_key_env: "OPENAI_API_KEY"
    context_window: 400000
    max_output: 128000
    input_cost_per_million: 1.25
    output_cost_per_million: 10
    generationDefaults:
      temperature: 0.7
      topP: 0.9

  - name: "sonnet"
    id: "claude-3-5-sonnet-20241022"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
    context_window: 200000
    max_output: 64000
    input_cost_per_million: 3
    output_cost_per_million: 15
    generationDefaults:
      temperature: 0.5
      maxTokens: 8192

  - name: "qwen-coder"
    id: "qwen-3-coder-32b"
    type: "cerebras"
    base_url: "https://api.cerebras.ai/v1/"
    api_key_env: "CEREBRAS_API_KEY"
    context_window: 128000
    max_output: 16384
    input_cost_per_million: 0.2
    output_cost_per_million: 0.2
    generationDefaults:
      temperature: 1.0
      topP: 0.8
      thinkingBudget: "10000"

# Global defaults
defaults:
  systemPromptPath: "./prompts/agent.prompt"
  model: "sonnet"
  timeout: "5m"
  noStream: false
  generationParams:
    temperature: 0.7
    maxTokens: 4096
```

### CLI Integration

```go
// cmd/root.go modifications
var (
    configPath string  // New unified config flag
    // Remove these legacy flags:
    // modelCatalogPath string - REMOVED
    // mcpConfigPath string    - REMOVED
)

func init() {
    rootCmd.PersistentFlags().StringVar(&configPath, "config", "", 
        "Path to unified configuration file (default: ./cpe.yaml, ~/.config/cpe/cpe.yaml)")
    
    // Remove these flag definitions:
    // rootCmd.PersistentFlags().StringVar(&modelCatalogPath, "model-catalog", "", ...)  // REMOVED
    // rootCmd.PersistentFlags().StringVar(&mcpConfigPath, "mcp-config", "", ...)        // REMOVED
    
    // Generation parameter flags remain for runtime overrides
}
```

## Implementation Plan

1. **Phase 1: Core Config Structure**
   - Create `internal/config` package
   - Define unified config structures
   - Implement YAML/JSON parsing and validation

2. **Phase 2: Config Loading**
   - Implement config search and loading logic
   - Add environment variable expansion
   - Create config validation and error handling

3. **Phase 3: Remove Legacy Flags**
   - **BREAKING**: Remove `--model-catalog` flag from `cmd/root.go`
   - **BREAKING**: Remove `--mcp-config` flag from `cmd/root.go`
   - Remove associated variables and validation logic
   - Update error messages to reference unified config

4. **Phase 4: Integration**
   - Modify `cmd/root.go` to use unified config
   - Update `agent.CreateToolCapableGenerator` to accept unified config
   - Implement flag override mechanism for generation parameters
   - Replace `modelcatalog.Load()` calls with unified config loading

5. **Phase 5: Testing**
   - Unit tests for config loading and validation
   - Integration tests for flag overrides
   - End-to-end tests with example configurations
   - Test error cases for missing config file

6. **Phase 6: Documentation**
   - Update README.md with new configuration approach
   - Add comprehensive config examples
   - Update AGENT.md with unified config details
   - Remove references to legacy model catalog and MCP config approaches

## Risks and Mitigations

### Risk 1: Breaking Changes
**Risk**: Existing users' workflows break due to removed flags
**Mitigation**: 
- Clear error messages explaining the new config requirement
- Provide comprehensive migration examples
- Document the breaking change prominently in release notes

### Risk 2: Config Complexity
**Risk**: Unified config becomes too complex/overwhelming
**Mitigation**:
- Provide minimal starter templates
- Clear documentation with common patterns
- Config validation with helpful error messages

### Risk 3: Missing Configuration
**Risk**: Users forget to create config file and get cryptic errors
**Mitigation**:
- Detect missing config and provide helpful error with example
- Consider `cpe init` command to generate starter config
- Clear documentation on config file locations

## Documentation

### Required Documentation Updates

1. **README.md**
   - **BREAKING**: Update all examples to use `--config` instead of `--model-catalog`
   - Replace multi-file config examples with unified approach
   - Add configuration precedence explanation
   - Update quick start to show config file creation
   - Comprehensive config guide

2. **AGENT.md**
   - Update configuration section with unified config
   - Remove references to separate model catalog and MCP config files
   - Update build/run commands to use unified config
   - Update examples in build commands section

3. **New Documentation**
   - `examples/cpe.yaml`: Well-commented example config showing all options
   - Update existing examples to remove model catalog references

## Appendix

### Example Usage After Changes

```bash
# Before (legacy, will be removed):
./cpe --model-catalog ./examples/models.json --mcp-config .cpemcp.json -m sonnet "Your prompt"

# After (unified config):
./cpe --config ./cpe.yaml -m sonnet "Your prompt"
# OR (auto-detected config):
./cpe -m sonnet "Your prompt"  # Uses ./cpe.yaml automatically

# Error example when config missing:
./cpe -m sonnet "test"
# Error: No configuration file found. Create cpe.yaml or use --config flag.
# See https://github.com/spachava753/cpe#configuration for examples.
```

### Config Precedence Order

1. CLI flags (highest priority)
2. Environment variables
3. Unified config file
4. Built-in defaults (lowest priority)

### Breaking Changes Summary

1. **Removed Flags**:
   - `--model-catalog` flag removed entirely
   - `--mcp-config` flag removed entirely

2. **New Requirements**:
   - Users MUST provide a unified config file
   - Config file MUST contain models section (no longer optional)
   - MCP servers MUST be defined in config file if needed

3. **Migration Path**:
   - Users need to combine their `models.json` + `.cpemcp.json` into `cpe.yaml`
   - All command examples in docs need updating
   - Scripts using old flags will break

### Future Considerations

- Plugin system configuration
- Remote config loading (URLs, S3, etc.)
- `cpe init` command to generate starter configs