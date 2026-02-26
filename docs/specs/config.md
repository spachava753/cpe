# CPE Configuration System Specification

This document describes the CPE configuration system, covering configuration file format, loading and resolution, validation, and schema generation.

## Overview

CPE uses a YAML-based configuration file (`cpe.yaml`) to define AI models, MCP servers, default settings, and subagent configurations. The configuration system provides:

- **Unified configuration**: All settings in a single file (models, MCP servers, defaults)
- **Environment variable expansion**: `${VAR}` syntax for secrets and dynamic values
- **Layered defaults**: Global defaults with model-specific overrides
- **JSON Schema validation**: IDE autocomplete and validation support
- **Runtime resolution**: CLI flags override config file settings

## Configuration File

### File Locations

CPE searches for configuration in the following order:

1. **Explicit path** (via `--config` flag)
2. **Current directory**: `./cpe.yaml` or `./cpe.yml`
3. **User config directory**:
   - Linux: `~/.config/cpe/cpe.yaml`
   - macOS: `~/Library/Application Support/cpe/cpe.yaml`
   - Windows: `%AppData%\cpe\cpe.yaml`

### Basic Configuration Structure

```yaml
version: "1.0"

mcpServers:
  filesystem:
    type: stdio
    command: filesystem-mcp
    args: ["--root", "/home/user"]
    
  search:
    type: http
    url: https://api.example.com/mcp
    headers:
      Authorization: "Bearer ${API_KEY}"

models:
  - ref: claude
    display_name: "Claude Sonnet"
    id: claude-sonnet-4-5
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    context_window: 200000
    max_output: 64000
    input_cost_per_million: 3.0
    output_cost_per_million: 15.0
    generationDefaults:
      temperature: 1
      thinkingBudget: "45000"

  - ref: gpt4
    display_name: "GPT-4"
    id: gpt-4o
    type: openai
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1

defaults:
  model: claude
  systemPromptPath: ./agent_instructions.md
  timeout: 5m
  generationParams:
    temperature: 0.7
  codeMode:
    enabled: true
    maxTimeout: 3600
    localModulePaths:
      - ../my-go-helpers

subagent:
  name: code-reviewer
  description: "Reviews code changes and provides feedback"
  outputSchemaPath: ./review_schema.json
```

## Configuration Types

### Model Configuration

Models are defined as a list under the `models` key. Each model has a base definition and optional overrides.

#### Base Model Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | Yes | Unique reference name for the model |
| `display_name` | string | Yes | Human-readable name |
| `id` | string | Yes | Actual model identifier for the API |
| `type` | string | Yes | Provider type: `openai`, `anthropic`, `gemini`, `responses`, `groq`, `cerebras`, `openrouter`, `zai` |
| `base_url` | string | No | Custom API endpoint URL |
| `api_key_env` | string | Yes* | Environment variable containing API key (*not required for OAuth) |
| `auth_method` | string | No | Authentication method: `apikey` (default) or `oauth` |
| `context_window` | uint32 | No | Maximum context tokens |
| `max_output` | uint32 | No | Maximum output tokens |
| `input_cost_per_million` | float64 | No | Cost per 1M input tokens |
| `output_cost_per_million` | float64 | No | Cost per 1M output tokens |
| `patch_request` | object | No | HTTP request patching configuration |

#### Model-Specific Overrides

Each model can override global defaults:

| Field | Type | Description |
|-------|------|-------------|
| `systemPromptPath` | string | Model-specific system prompt template |
| `generationDefaults` | object | Model-specific generation parameters |
| `codeMode` | object | Model-specific code mode settings (full replacement, no merge) |

Example with overrides:

```yaml
models:
  - ref: claude-thinking
    display_name: "Claude (Extended Thinking)"
    id: claude-sonnet-4-5
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    systemPromptPath: ./reasoning_prompt.md
    generationDefaults:
      thinkingBudget: "64000"
      temperature: 1
    codeMode:
      enabled: true
      excludedTools: [shell, text_edit]
```

### MCP Server Configuration

MCP servers extend CPE's capabilities with external tools. See `docs/specs/mcp_handling.md` for detailed MCP documentation.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Transport type: `stdio`, `http`, or `sse` |
| `command` | string | Yes (stdio) | Executable command |
| `args` | []string | No | Command arguments |
| `url` | string | Yes (http/sse) | Endpoint URL |
| `timeout` | int | No | Connection timeout in seconds (default: 60) |
| `env` | map[string]string | No (stdio only) | Additional environment variables |
| `headers` | map[string]string | No (http/sse only) | Custom HTTP headers |
| `enabledTools` | []string | No | Whitelist of tool names |
| `disabledTools` | []string | No | Blacklist of tool names |

### Generation Parameters

Generation parameters control LLM behavior. They can be specified globally in `defaults.generationParams` or per-model in `models[].generationDefaults`.

| Field | Type | Validation | Description |
|-------|------|------------|-------------|
| `temperature` | float64 | 0-2 | Sampling temperature |
| `topP` | float64 | 0-1 | Nucleus sampling |
| `topK` | uint | >=0 | Top-k sampling |
| `frequencyPenalty` | float64 | -2 to 2 | Frequency penalty |
| `presencePenalty` | float64 | -2 to 2 | Presence penalty |
| `n` | uint | 0-2 | Number of completions |
| `maxGenerationTokens` | int | >=0 | Maximum tokens to generate |
| `toolChoice` | string | - | Tool selection strategy |
| `stopSequences` | []string | - | Sequences that stop generation |
| `thinkingBudget` | string | - | Thinking budget: `minimal`, `low`, `medium`, `high`, or token count |

### Code Mode Configuration

Code mode enables LLMs to execute Go code for composable tool operations.

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Enable code mode |
| `excludedTools` | []string | Tools to exclude from code mode (called normally) |
| `localModulePaths` | []string | Local module directories to add to the execution workspace (`go.mod` required in each directory) |
| `maxTimeout` | int | Maximum execution timeout in seconds |
| `largeOutputCharLimit` | int | Max characters before tool output is spilled to disk preview |

Path semantics:
- `localModulePaths` may be absolute or relative.
- Relative paths resolve against the config file directory.
- Paths are normalized to absolute paths in effective runtime config.

**Important**: Code mode configuration does NOT merge between defaults and model-specific settings. Model `codeMode` completely replaces `defaults.codeMode`.

```yaml
defaults:
  codeMode:
    enabled: true
    excludedTools: [slow_tool]

models:
  - ref: fast-model
    # ...
    codeMode:
      enabled: true
      # excludedTools is NOT inherited from defaults!
      # Must specify complete configuration
```

### Subagent Configuration

Subagent configuration is used when running CPE in MCP server mode (`cpe mcp serve`). See `docs/specs/mcp_server_mode.md` for details.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Tool name exposed to parent |
| `description` | string | Yes | Tool description for LLM |
| `outputSchemaPath` | string | No | Path to JSON schema file for structured output |

## Configuration Resolution

Configuration is resolved from `RawConfig` (file structure) to `Config` (runtime effective) with the following precedence:

### Resolution Precedence

| Setting | Highest Priority -> Lowest Priority |
|---------|-----------------------------------|
| **Model selection** | CLI `--model` -> `defaults.model` |
| **System prompt** | Model-specific -> Global `defaults.systemPromptPath` |
| **Generation params** | CLI flags -> Model `generationDefaults` -> Global `defaults.generationParams` |
| **Timeout** | CLI `--timeout` -> `defaults.timeout` -> 5 minutes (default) |
| **Code mode** | Model `codeMode` (full override) -> `defaults.codeMode` |

### Resolution Process

1. **Load**: Parse YAML/JSON file into `RawConfig`
2. **Expand**: Replace `${VAR}` with environment variable values
3. **Validate**: Validate struct tags and custom rules
4. **Resolve**: Apply precedence rules to create `Config`

Generation parameters are merged using pointer-aware field-level overrides. A non-nil pointer in a higher-precedence source overrides the destination, while nil (unset) leaves it unchanged:

```go
// CLI flags override model-specific, which override global defaults
mergedParams := defaults.generationParams
mergeGenOpts(&mergedParams, model.generationDefaults)
// Then apply CLI flag overrides
```

## Environment Variable Expansion

Environment variable expansion is supported **everywhere** in the configuration file using `$VAR` or `${VAR}` syntax. The entire config file content is processed through `os.ExpandEnv()` before YAML/JSON parsing, enabling maximum flexibility.

### Supported Syntax

| Syntax | Description |
|--------|-------------|
| `$VAR` | Simple variable reference |
| `${VAR}` | Braced variable reference (useful when followed by other characters) |

### Examples

```yaml
models:
  - ref: claude
    type: anthropic
    api_key_env: $API_KEY_VAR_NAME  # Expands to the variable name stored in API_KEY_VAR_NAME
    base_url: https://${API_DOMAIN}/v1  # Mixed with other text
    display_name: $MODEL_NAME

mcpServers:
  api:
    type: http
    url: $MCP_SERVER_URL
    headers:
      Authorization: "Bearer ${API_TOKEN}"
      $HEADER_NAME: $HEADER_VALUE  # Both key and value are expanded
    env:
      $ENV_KEY: $ENV_VALUE  # Map keys and values are expanded

defaults:
  systemPromptPath: $HOME/.config/cpe/prompt.md
  model: $DEFAULT_MODEL
  codeMode:
    excludedTools:
      - $EXCLUDED_TOOL_1
      - ${EXCLUDED_TOOL_2}
```

### Behavior Notes

1. **Undefined variables**: Expand to empty strings (standard `os.ExpandEnv` behavior)
2. **Map keys**: Both keys and values in maps are expanded
3. **Arrays**: All elements in arrays are expanded
4. **Nested values**: All nested string values are expanded
5. **Non-string values**: Numbers, booleans, etc. are not affected by expansion (they're parsed after expansion)

### Caveats

1. **Special characters in values**: If an environment variable contains YAML special characters (`:`, `#`, `[`, `]`, `{`, `}`) or JSON special characters (`"`, `\`), the config may fail to parse. Use quoted values to mitigate:
   ```yaml
   # Safer - quotes protect against special characters
   password: "$DB_PASSWORD"
   
   # Risky - if DB_PASSWORD contains ":" it will break YAML parsing
   password: $DB_PASSWORD
   ```

2. **No escape syntax**: There is no way to include a literal `$` followed by a variable-like name. The `$$` sequence does not escape to a single `$` (it expands `$` as a variable name).

3. **Empty values on undefined**: Undefined environment variables silently become empty strings, which may cause confusing validation errors. Always ensure required environment variables are set before running CPE.

## Validation

### Struct Validation

Configuration is validated using `go-playground/validator/v10` with struct tags:

| Field | Validation |
|-------|------------|
| `Model.Ref` | `required` |
| `Model.Type` | `required,oneof=openai anthropic gemini responses groq cerebras openrouter zai` |
| `Model.BaseUrl` | `omitempty,https_url\|http_url` |
| `ServerConfig.Type` | `required,oneof=stdio sse http` |
| `ServerConfig.Timeout` | `gte=0` |
| `Models` | `gt=0,unique=Ref` (at least one model, unique refs) |

### Custom Validation

Additional validations performed by `RawConfig.Validate()`:

1. **Default model exists**: `defaults.model` must reference a model in the `models` list
2. **OAuth constraints**: `auth_method: oauth` is only allowed for `type: anthropic`
3. **Subagent schema**: If `subagent.outputSchemaPath` is specified, the file must exist and contain valid JSON

### Validation Errors

Validation errors include the full path to help users locate issues:

```
invalid configuration file: Key: 'RawConfig.Models[0].Type' Error:Field validation for 'Type' failed on the 'oneof' tag
```

## JSON Schema Generation

CPE generates a JSON Schema from Go struct definitions for IDE support.

### Generation Process

1. **Source**: `internal/config/config.go` and `internal/mcp/client.go` define the structs
2. **Generator**: `scripts/gen_schema_task.go` uses `invopop/jsonschema`
3. **Output**: `schema/cpe-config-schema.json`

### Schema Features

- Strict mode: `additionalProperties: false` (no extra fields allowed)
- Required fields inferred from JSON tags
- All config types defined as `$defs`

### Regenerating Schema

```bash
# Via go generate
go generate ./...

# Via goyek task
go run ./scripts gen-schema
```

## Architecture

### Key Files

| File | Purpose |
|------|---------|
| `internal/config/config.go` | Type definitions (`RawConfig`, `Config`, `Model`, `ModelConfig`, etc.) |
| `internal/config/loader.go` | Config file discovery, parsing, and environment expansion |
| `internal/config/resolver.go` | Resolution from `RawConfig` to runtime `Config` |
| `internal/config/validator.go` | Validation logic using `go-playground/validator` |
| `internal/config/writer.go` | Config file writing utilities |
| `internal/config/registry.go` | models.dev integration for importing model catalogs |
| `internal/mcp/client.go` | `ServerConfig` type definition |
| `scripts/gen_schema_task.go` | JSON Schema generation task |
| `schema/cpe-config-schema.json` | Generated schema output |

### Type Hierarchy

```
RawConfig (YAML file structure)
├── MCPServers: map[string]ServerConfig
├── Models: []ModelConfig
│   ├── Model (embedded base model)
│   ├── SystemPromptPath
│   ├── GenerationDefaults
│   └── CodeMode
├── Defaults
│   ├── SystemPromptPath
│   ├── Model (default ref)
│   ├── GenerationParams
│   ├── Timeout
│   └── CodeMode
├── Subagent
└── Version

        ↓ ResolveConfig()

Config (runtime effective)
├── MCPServers
├── Model (selected model)
├── SystemPromptPath (resolved)
├── GenerationDefaults (merged)
├── Timeout (resolved)
└── CodeMode (resolved)
```

### Configuration Loading Flow

```
LoadRawConfig(path)
    ├── Find config file (search order)
    ├── Parse YAML/JSON
    ├── Expand environment variables
    └── Validate (RawConfig.Validate())
            ├── Struct validation (validator.v10)
            ├── Default model exists check
            ├── Auth method constraints
            └── Subagent schema validation

ResolveConfig(configPath, runtimeOpts)
    ├── LoadRawConfig()
    ├── Select model (CLI > defaults)
    ├── Merge generation params
    ├── Resolve system prompt path
    ├── Resolve timeout
    └── Resolve code mode
```

## CLI Integration

### Config Commands

```bash
# Use specific config file
cpe --config ./custom.yaml "prompt"

# Override model
cpe --model gpt4 "prompt"

# Override timeout
cpe --timeout 10m "prompt"

# List available models
cpe models list

# Show model details
cpe models info claude
```

### Runtime Options

CLI flags create `RuntimeOptions` that override config file settings:

| Flag | Environment Variable | Description |
|------|---------------------|-------------|
| `--model` | `CPE_MODEL` | Override default model |
| `--timeout` | - | Override timeout (e.g., `5m`, `30s`) |
| `--config` | - | Explicit config file path |

## Error Handling

### Loading Errors

| Error | Cause |
|-------|-------|
| `config file not found` | No config file in search paths |
| `failed to parse config` | Invalid YAML/JSON syntax |
| `invalid configuration file` | Validation failure (struct tags or custom rules) |

### Resolution Errors

| Error | Cause |
|-------|-------|
| `default model %q not found` | `defaults.model` references non-existent model |
| `model %q not found` | CLI `--model` references non-existent model |
| `failed to parse timeout` | Invalid duration string |

## Best Practices

### Security

- Store API keys in environment variables, not config files
- Use `api_key_env` to reference environment variables
- Use `${VAR}` syntax for headers and URLs containing secrets

### Organization

- Use descriptive `ref` names (e.g., `claude-sonnet`, `gpt4-turbo`)
- Group related MCP servers together
- Comment complex configurations

### Version Control

- Commit `cpe.yaml` without secrets (use `${VAR}`)
- Use `examples/` directory for template configurations
- Document model-specific overrides

### IDE Support

Configure your IDE to use the JSON Schema for autocomplete:

**VS Code** (`settings.json`):
```json
{
  "yaml.schemas": {
    "schema/cpe-config-schema.json": "cpe.yaml"
  }
}
```

## Related Specifications

- `docs/specs/mcp_handling.md` - MCP server configuration details
- `docs/specs/mcp_server_mode.md` - Subagent configuration
- `docs/specs/code_mode.md` - Code mode configuration
- `docs/prds/unified_config.md` - Original unified config PRD
- `docs/prds/config_schema_and_lint.md` - Schema generation PRD
