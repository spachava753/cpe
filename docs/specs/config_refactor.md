# Config Refactor Specification

## Problem Statement

The current config loading logic has two major issues:

### 1. Effective Parameters Not Computed

Config parameters are computed at the site of usage, leading to repetitive code throughout the codebase. Example from `cmd/root.go`:

```go
// Determine which model to use
modelName := model
if modelName == "" {
    if cfg.Defaults.Model != "" {
        modelName = cfg.Defaults.Model
    } else if DefaultModel != "" {
        modelName = DefaultModel
    } else {
        return errors.New("no model specified...")
    }
}

// Determine system prompt path
spPath := selectedModel.SystemPromptPath
if spPath == "" {
    spPath = cfg.Defaults.SystemPromptPath
}
```

This pattern increases maintenance burden and creates opportunities for bugs.

### 2. Config Type Bloat

The current `Config` type contains unnecessary fields. At runtime, we only care about:
- The selected model and its configuration
- MCP servers
- System prompt (loaded as `fs.FS`)
- Effective generation parameters

## Solution Design

### Two-Tier Configuration Architecture

#### RawConfig (File Schema)

Represents the configuration file structure with all models and defaults:

```go
// RawConfig represents the configuration file structure
type RawConfig struct {
    MCPServers map[string]mcp.ServerConfig `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty" validate:"dive"`
    Models     []ModelConfig                `yaml:"models" json:"models" validate:"gt=0,unique=Ref,dive"`
    Defaults   Defaults                     `yaml:"defaults,omitempty" json:"defaults,omitempty"`
    Version    string                       `yaml:"version,omitempty" json:"version,omitempty"`
}
```

#### Config (Runtime Effective Config)

The resolved, runtime-ready configuration for a specific model:

```go
// Config represents the effective runtime configuration for a single model
type Config struct {
    // MCP server configurations
    MCPServers map[string]mcp.ServerConfig

    // Selected model configuration
    Model Model

    // Loaded system prompt filesystem
    SystemPrompt fs.FS

    // Effective generation parameters after merging all sources
    GenerationDefaults *gai.GenOpts

    // Effective timeout
    Timeout time.Duration

    // Whether streaming is disabled
    NoStream bool
}
```

#### RuntimeOptions (CLI Flags & Environment)

Captures runtime overrides from CLI flags and environment variables:

```go
// RuntimeOptions captures runtime overrides from CLI flags and environment
type RuntimeOptions struct {
    // Model ref to use (from --model or CPE_MODEL)
    ModelRef string

    // Generation parameter overrides (from flags)
    GenParams *gai.GenOpts

    // Timeout override (from --timeout)
    Timeout string

    // Streaming override (from --no-stream)
    NoStream *bool
}
```

### Parameter Merge Precedence

Resolution follows this priority order (highest to lowest):

1. **CLI flags** (`RuntimeOptions`)
2. **Model-specific config** (`ModelConfig.GenerationDefaults`, `ModelConfig.SystemPromptPath`)
3. **Global defaults** (`Defaults` section in config file)

If a required parameter is not provided through any of these sources, config resolution fails with a clear error message.

### Core Functions

#### Loading Raw Config

```go
// loadRawConfig loads and validates the raw configuration from file
// This is internal and not exported
func loadRawConfig(explicitPath string) (*RawConfig, error)
```

#### Resolving Effective Config

```go
// ResolveConfig loads the config file and resolves effective runtime configuration
// for the specified model with runtime options applied
func ResolveConfig(configPath string, opts RuntimeOptions) (*Config, error)
```

This function:
1. Loads and validates `RawConfig` from file
2. Validates `RuntimeOptions`
3. Determines which model to use (opts.ModelRef → defaults.Model, error if neither)
4. Finds the model in the config (error if not found)
5. Merges generation parameters following precedence rules
6. Resolves and loads system prompt into `fs.FS` (error if required but not found)
7. Resolves timeout and streaming settings
8. Returns effective `Config` or error if any required parameter is missing

### Command-Specific Handling

#### `model list` Command

Needs access to all models. Will directly parse the config file:

```go
// In cmd/model.go
cfg, err := config.LoadRawConfigForListing(configPath)
// Access cfg.Models, cfg.Defaults.Model for listing
```

Add new exported function:

```go
// LoadRawConfigForListing loads raw config for commands that need to list/inspect all models
func LoadRawConfigForListing(configPath string) (*RawConfig, error)
```

#### `model info` Command

Shows details for a specific model. Uses `ResolveConfig`:

```go
// In cmd/model.go
cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{
    ModelRef: modelName,
})
// Access cfg.Model for detailed info
```

#### `config lint` Command

**Remove this command.** Config validation errors will surface when users invoke commands.

#### `root` Command

Uses `ResolveConfig` with full `RuntimeOptions` from flags:

```go
cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{
    ModelRef:         model,
    SystemPromptPath: systemPromptPath,
    GenParams:        genParamsFromFlags,
    Timeout:          timeout,
    NoStream:         &noStream,
})
```

### System Prompt Handling

Current code passes around system prompt paths. Refactor to:

1. **Resolution phase**: Load the system prompt file into `fs.FS` during `ResolveConfig`
2. **Usage phase**: All code consuming system prompts accepts `fs.FS` instead of paths

Example refactor in `internal/agent`:

```go
// Before
func NewAgent(systemPromptPath string, ...) (*Agent, error) {
    // Load file here...
}

// After
func NewAgent(systemPromptFS fs.FS, ...) (*Agent, error) {
    // Use fs.FS directly
}
```

### Validation Strategy

- **RawConfig**: Full validation using struct tags and custom validators
- **RuntimeOptions**: Validate non-empty required fields, valid formats
- **Config**: No validation needed (derived from validated inputs)

## Implementation Checklist

- [ ] Rename existing `Config` to `RawConfig`
- [ ] Define new `Config` struct with effective fields
- [ ] Define `RuntimeOptions` struct
- [ ] Implement `loadRawConfig()` (internal)
- [ ] Implement `ResolveConfig()` with merge logic
- [ ] Implement `LoadRawConfigForListing()` for model commands
- [ ] Update `cmd/root.go` to use `ResolveConfig()`
- [ ] Update `cmd/model.go list` to use `LoadRawConfigForListing()`
- [ ] Update `cmd/model.go info` to use `ResolveConfig()`
- [ ] Remove `config lint` command
- [ ] Refactor system prompt consumers to accept `fs.FS`
- [ ] Update tests for two-tier config
- [ ] Update documentation and examples

## Migration Notes

- Existing config files remain compatible (no schema changes)
- Internal API changes only; no user-facing breaking changes
- Commands automatically get effective config without manual merging
- Error messages should indicate which config sources were checked when resolution fails