# Product Requirements Document: JSON Schema Generation and Config Lint Command

## Executive Summary

This PRD outlines the addition of JSON Schema generation for CPE's unified configuration structure and a new `cpe config lint` command. These features will improve the developer experience by enabling IDE autocompletion, validation, and early error detection for CPE configuration files.

Key additions:
- **New feature**: Generate JSON Schema from Go config structs using code generation
- **New feature**: Add `cpe config lint` command to validate configuration files
- **New feature**: Support JSON Schema for IDE/editor tooling integration

## Background and Problem Statement

### Current State

CPE uses a unified configuration file (`cpe.yaml` or `cpe.json`) defined by structs in `internal/config/config.go`:
- `Config`: Root configuration structure
- `ModelConfig`: Model definitions with generation defaults
- `GenerationParams`: Generation parameter settings
- `DefaultConfig`: Global defaults
- `PatchRequestConfig`: HTTP request patching configuration

Configuration validation logic exists in `internal/config/loader.go`:
- `Config.Validate()`: Validates the entire configuration
- `validateGenerationParams()`: Validates generation parameters
- Validates model types, refs, generation parameters, MCP server configs, etc.

Configuration loading happens in `cmd/root.go`, `cmd/model.go`, `cmd/tools.go`, `cmd/mcp.go`, and `cmd/system_prompt.go` via `config.LoadConfig(configPath)`, which automatically validates on load.

### Pain Points

From the perspective of a CPE user:

1. **No IDE autocompletion**: Users manually type YAML/JSON config without IDE hints, leading to typos and incorrect field names
2. **Late error detection**: Configuration errors are only discovered at runtime when executing CPE commands
3. **Poor discoverability**: Users must refer to documentation or examples to understand available fields and their types
4. **No schema validation in editors**: Modern editors like VS Code, IntelliJ, and Neovim support JSON Schema for autocompletion and inline validation, but CPE doesn't provide a schema
5. **Manual validation workflow**: Users must run `cpe` commands to discover config errors instead of catching them during editing

## Goals and Outcomes

### Goals

1. Generate a JSON Schema from CPE's config structs that accurately represents all configuration options
2. Provide IDE/editor integration through JSON Schema for YAML and JSON config files
3. Create a `cpe config lint` command that validates configuration files without executing other operations
4. Use Go code generation (`go generate`) to keep the schema in sync with struct definitions
5. Distribute the JSON Schema with CPE for easy consumption by developer tools

### Outcomes

After implementation, users will be able to:
- Receive autocompletion suggestions while editing `cpe.yaml` or `cpe.json` files in their IDE/editor
- See inline validation errors and warnings in their editor before running CPE
- Run `cpe config lint` to validate their configuration as part of CI/CD pipelines
- Discover available configuration options through IDE tooltips and documentation hints
- Catch configuration errors during development instead of at runtime

## Requirements

### Functional Requirements

1. **JSON Schema Generation**
    - Generate JSON Schema (Draft 2020-12 or compatible) from `internal/config/config.go` structs
    - Use `github.com/invopop/jsonschema` package (already in `go.mod`)
    - Generate schema via `go generate` command
    - Output schema to `schema/cpe-config-schema.json` at repository root
    - Include descriptions, field constraints, required fields, and type information
    - Support for both YAML and JSON validation (JSON Schema works for both formats)

2. **Code Generation Setup**
    - Create `internal/config/generate.go` with schema generation logic
    - Add `//go:generate` directive to trigger generation
    - Update root `gen.go` or add new generation entrypoint
    - Ensure generated schema is checked into version control

3. **Config Lint Command**
    - Add new command: `cpe config lint [config-file]`
    - Validate configuration using existing `config.LoadConfig()` and `Config.Validate()` logic
    - Accept optional config file path; default to standard search locations
    - Return exit code 0 for valid config, non-zero for invalid
    - Output clear, actionable error messages for validation failures
    - Support `--strict` flag for additional validations (future extensibility)

4. **Schema Integration**
    - Add schema reference to example `cpe.yaml` file
    - Document schema location in README.md
    - Provide instructions for IDE/editor integration (VS Code, IntelliJ, Neovim)

5. **Documentation Updates**
    - Add `cpe config lint` to command documentation
    - Create schema integration guide for popular editors
    - Update AGENTS.md with schema generation workflow

### Non-Functional Requirements

1. **Maintainability**
    - Schema generation must be automated and repeatable
    - Any changes to config structs should trigger schema regeneration
    - Generated schema must be human-readable and well-formatted

2. **Reliability**
    - Schema must accurately reflect all config struct fields and validation rules
    - `cpe config lint` must use the same validation logic as config loading
    - Error messages must be clear and actionable

3. **Developer Experience**
    - Schema generation should complete in under 1 second
    - `cpe config lint` should provide fast feedback (under 100ms for typical configs)
    - IDE integration setup should require minimal configuration

## Technical Design

### Architecture Overview

The implementation follows this flow:

1. **Schema Generation** (build-time):
   ```
   go generate → internal/config/generate.go → schema/cpe-config-schema.json
   ```

2. **Config Validation** (runtime):
   ```
   cpe config lint → config.LoadConfig() → Config.Validate() → validation errors
   ```

3. **IDE Integration** (editor-time):
   ```
   Editor reads cpe.yaml → Matches $schema or file pattern → Loads schema/cpe-config-schema.json → Provides autocompletion/validation
   ```

### Code Structure

#### Schema Generation (`internal/config/generate.go`)

```go
package config

//go:generate go run generate.go

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	
	"github.com/invopop/jsonschema"
)

// Generate JSON Schema for CPE configuration
func generateSchema() error {
	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties: false,
		RequiredFromJSONSchemaTags: true,
	}
	
	schema := reflector.Reflect(&Config{})
	schema.Title = "CPE Configuration Schema"
	schema.Description = "JSON Schema for CPE (Chat-based Programming Editor) configuration files"
	schema.Version = "https://json-schema.org/draft/2020-12/schema"
	
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}
	
	schemaPath := filepath.Join("schema", "cpe-config-schema.json")
	if err := os.MkdirAll(filepath.Dir(schemaPath), 0755); err != nil {
		return fmt.Errorf("failed to create schema directory: %w", err)
	}
	
	if err := os.WriteFile(schemaPath, schemaJSON, 0644); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}
	
	fmt.Printf("Generated schema: %s\n", schemaPath)
	return nil
}

func main() {
	if err := generateSchema(); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating schema: %v\n", err)
		os.Exit(1)
	}
}
```

#### Config Lint Command (`cmd/config.go`)

```go
package cmd

import (
	"fmt"
	
	"github.com/spachava753/cpe/internal/config"
	"github.com/spf13/cobra"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	Long:  `Manage and validate CPE configuration files.`,
}

// configLintCmd validates configuration files
var configLintCmd = &cobra.Command{
	Use:   "lint [config-file]",
	Short: "Validate CPE configuration file",
	Long: `Validate a CPE configuration file for correctness.

If no config file is specified, searches for configuration in the default locations:
  - ./cpe.yaml or ./cpe.yml (current directory)
  - ~/.config/cpe/cpe.yaml or ~/.config/cpe/cpe.yml (user config directory)

Exit codes:
  0 - Configuration is valid
  1 - Configuration has errors`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var configPath string
		if len(args) > 0 {
			configPath = args[0]
		}
		
		// Load and validate config (automatically validates)
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}
		
		fmt.Printf("✓ Configuration is valid\n")
		fmt.Printf("  Models: %d\n", len(cfg.Models))
		if len(cfg.MCPServers) > 0 {
			fmt.Printf("  MCP Servers: %d\n", len(cfg.MCPServers))
		}
		if cfg.GetDefaultModel() != "" {
			fmt.Printf("  Default Model: %s\n", cfg.GetDefaultModel())
		}
		
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configLintCmd)
}
```

### Configuration Changes

Update `examples/cpe.yaml` to include schema reference:

```yaml
# yaml-language-server: $schema=../schema/cpe-config-schema.json
version: "1.0"

# MCP server definitions
mcpServers:
  editor:
    command: "editor-mcp"
    type: stdio
    timeout: 60
    toolFilter: whitelist
    enabledTools:
      - text_edit
      - shell

# ... rest of config
```

### Integration Points

1. **Config Package** (`internal/config/`)
   - Add `generate.go`: Schema generation logic
   - Existing `config.go`: Source structs for schema
   - Existing `loader.go`: Validation logic reused by lint command

2. **Root Gen File** (`gen.go`)
   - Add or reference config generation directive

3. **Command Package** (`cmd/`)
   - New `config.go`: Config management commands
   - Add `configCmd` to `rootCmd` in `init()`

4. **Schema Directory** (`schema/`)
   - New directory at repository root
   - Contains `cpe-config-schema.json` (generated)
   - Include in `.gitignore` or commit generated schema (prefer committing)

5. **Examples** (`examples/`)
   - Update `cpe.yaml` with schema reference

## Implementation Plan

1. **Phase 1: Schema Generation**
    - Create `internal/config/generate.go` with schema generation logic
    - Add `//go:generate` directive
    - Create `schema/` directory
    - Run `go generate` and verify output
    - Add struct field tags (if needed) for better schema descriptions
    - Test that generated schema is valid JSON Schema

2. **Phase 2: Config Lint Command**
    - Create `cmd/config.go` with `configCmd` and `configLintCmd`
    - Wire up to `rootCmd` in init
    - Implement validation logic using existing `config.LoadConfig()`
    - Add helpful success/error output formatting
    - Test with valid and invalid config files

3. **Phase 3: Integration and Documentation**
    - Update `examples/cpe.yaml` with schema reference
    - Document schema generation in AGENTS.md
    - Add editor integration instructions to README.md
    - Create examples for VS Code (`.vscode/settings.json`), IntelliJ, and Neovim
    - Update build documentation to mention schema generation

4. **Phase 4: CI/CD Integration**
    - Add `go generate` step to CI workflow (if applicable)
    - Verify generated schema is up-to-date in CI
    - Add `cpe config lint examples/cpe.yaml` to CI checks

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Generated schema doesn't accurately represent config structs | Validate schema against example configs in automated tests; add integration tests that verify schema matches actual config loading behavior |
| Schema becomes out of sync with code changes | Make schema generation part of CI/CD; fail builds if generated schema is outdated; document schema regeneration in AGENTS.md |
| IDE integration doesn't work across all editors | Provide clear setup instructions for popular editors; test with VS Code, IntelliJ, and Neovim; document known limitations |
| `jsonschema` library limitations | Use struct tags and custom reflector settings to fine-tune schema output; fallback to manual schema edits if needed (document deviations) |
| Breaking changes to schema format | Version the schema file (e.g., `cpe-config-schema-v1.json`); use semantic versioning for config schema |

## Documentation

### Required Documentation Updates

1. **README.md**
   - Add "Configuration Validation" section
   - Document `cpe config lint` command
   - Add "IDE Integration" section with setup instructions for VS Code, IntelliJ, Neovim
   - Link to generated schema file

2. **AGENTS.md**
   - Update "Build, test, and development commands" section:
     ```bash
     # Generate JSON Schema for config
     go generate ./internal/config/
     
     # Validate configuration
     ./cpe config lint ./examples/cpe.yaml
     ```
   - Document schema generation as part of development workflow

3. **New Documentation**: `docs/ide-integration.md`
   - VS Code setup (workspace settings, YAML plugin config)
   - IntelliJ/GoLand setup (YAML plugin, schema mapping)
   - Neovim setup (yaml-language-server config)
   - Example configurations for each editor

4. **Command Help Text**
   - Ensure `cpe config --help` displays clear usage
   - Ensure `cpe config lint --help` shows examples

## Appendix

### Example Usage

#### Schema Generation
```bash
# Generate schema from config structs
go generate ./internal/config/

# Output: schema/cpe-config-schema.json created
```

#### Config Validation
```bash
# Lint default config location
cpe config lint

# Lint specific config file
cpe config lint ./my-config.yaml

# Example output (valid):
✓ Configuration is valid
  Models: 10
  MCP Servers: 1
  Default Model: sonnet

# Example output (invalid):
Error: configuration validation failed: model "invalid-model": invalid type 'unknown', must be one of: openai, responses, anthropic, gemini, groq, cerebras
```

#### IDE Integration Example (VS Code)

Create `.vscode/settings.json`:
```json
{
  "yaml.schemas": {
    "./schema/cpe-config-schema.json": ["cpe.yaml", "cpe.yml", "**/cpe.yaml", "**/cpe.yml"]
  }
}
```

Or add to individual YAML file:
```yaml
# yaml-language-server: $schema=./schema/cpe-config-schema.json
version: "1.0"
models:
  - ref: sonnet  # IDE autocompletes: ref, display_name, id, type, etc.
```

### Reference Information

- **JSON Schema Specification**: https://json-schema.org/draft/2020-12/json-schema-core.html
- **invopop/jsonschema Library**: https://github.com/invopop/jsonschema
- **VS Code YAML Extension**: https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml
- **yaml-language-server**: https://github.com/redhat-developer/yaml-language-server

### Testing Strategy

#### Unit Tests
- Test schema generation produces valid JSON
- Test schema includes all config struct fields
- Test schema enforces required fields correctly
- Test `config lint` command with valid configs
- Test `config lint` command with invalid configs (various error types)

#### Integration Tests
- Validate generated schema against `examples/cpe.yaml`
- Test that schema validation matches `Config.Validate()` behavior
- Test config loading with schema-validated files produces no additional errors

#### Manual Testing
- Test IDE autocompletion in VS Code with YAML extension
- Test inline validation errors appear for invalid configs
- Test schema updates after struct changes

### Future Considerations

1. **Strict Mode**: Add `--strict` flag to `cpe config lint` for additional checks beyond current validation
2. **Schema Versioning**: Version schemas as config format evolves; support multiple schema versions
3. **Config Initialization**: Add `cpe config init` command to generate starter config from template
4. **Online Schema Hosting**: Host schema on GitHub Pages or CDN for easier IDE consumption without local files
5. **Enhanced Validation**: Add custom validators for complex constraints (e.g., mutually exclusive fields)
6. **Config Format Conversion**: Add `cpe config convert` to migrate between JSON/YAML formats
7. **JSON Schema Tags**: Add custom struct tags to provide richer descriptions and examples in schema

### Breaking Changes Summary

No breaking changes. This is a purely additive feature that doesn't modify existing behavior.
