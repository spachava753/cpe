# CPE (Chat-based Programming Editor)

CPE is a powerful command-line tool that enables developers to leverage AI for codebase analysis, modification, and
software development through natural language interactions in your terminal.

## Overview

CPE serves as an intelligent agent to assist with day-to-day software development tasks by connecting multiple AI
models (OpenAI, Anthropic, Google) to your local environment through a simple CLI interface. It helps you analyze
codebases, make code modifications, debug issues, and perform various programming tasks through natural language
conversations.

## Features

- **AI-powered code assistance**: Interact with advanced AI models through natural language to analyze and modify code
- **Codebase understanding**: Process and analyze codebases of any size
- **File & folder operations**: Create, view, edit, move, and delete files and folders with AI guidance
- **Shell command execution**: Run bash commands directly through the AI
- **Multiple AI model support**:
  - OpenAI models (GPT-4o, GPT-4o Mini, etc.)
  - Anthropic Claude models (Claude 3.5 Sonnet, Claude 3 Opus, etc.)
  - Google Gemini models (Gemini 1.5 Pro, Gemini 1.5 Flash, etc.)
- **Code Mode**: Enable LLMs to generate and execute Go code for complex tool compositions, control flow, and multi-step operations in a single turn
- **Conversation management**: Save, list, view, and continue previous conversations
- **Model Context Protocol (MCP)**: Connect to external MCP servers for enhanced functionality

## Installation

### Prerequisites

- Go 1.23+
- API key for at least one supported AI model provider:
  - OpenAI: https://platform.openai.com/
  - Anthropic: https://console.anthropic.com/
  - Google AI (Gemini): https://ai.google.dev/

### Install from source

```bash
go install github.com/spachava753/cpe@latest
```

### Environment Variables

Configure at least one of these API keys:

```bash
# Required (at least one)
export ANTHROPIC_API_KEY="your_anthropic_api_key"
export OPENAI_API_KEY="your_openai_api_key"
export GEMINI_API_KEY="your_gemini_api_key"

# Optional
export CPE_MODEL="claude-3-5-sonnet"  # Default model to use if not specified with --model
export CPE_CUSTOM_URL="https://your-custom-endpoint.com"  # For custom API endpoints
```

## Quick Start

### Configuration Setup

CPE requires a configuration file to define available models. Create a `cpe.yaml` file in your current directory or in the platform-specific user configuration directory (e.g., `~/.config/cpe/cpe.yaml` on Linux, `$HOME/Library/Application Support/cpe/cpe.yaml` on macOS, or `%AppData%\cpe\cpe.yaml` on Windows):

```yaml
version: "1.0"

models:
  - name: "sonnet"
    id: "claude-3-5-sonnet-20241022"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
    context_window: 200000
    max_output: 8192

defaults:
  model: "sonnet"
```

See the [Configuration](#configuration) section for a complete example with multiple models and advanced settings.

### Basic Usage

```bash
# Ask a simple question
cpe "What is a fibonacci sequence?"

# Start a coding task
cpe "Create a simple REST API server in Go with one endpoint to return the current time"

# Analyze a specific file
cpe "Analyze this code and suggest improvements" -i path/to/your/file.js

# Analyze multiple files (either approach works)
cpe "Check these files for bugs" -i file1.js -i file2.js
cpe "Check these files for bugs" -i file1.js,file2.js

# Start a new conversation (instead of continuing the last one)
cpe -n "Let's start a new project"

# Continue a specific conversation by ID
cpe -c abc123 "Could you explain more about the previous solution?"
```

### Working with AI tools

CPE provides a set of tools that the AI can use to help you:

- **Codebase analysis**: Understanding your codebase structure
- **File operations**: Creating, editing, viewing, and deleting files
- **Folder operations**: Creating, moving, and deleting directories
- **Shell integration**: Running commands directly in your environment
- **Multimedia input support**: Process images, audio, and video files with the `-i` flag

Just ask the AI naturally, and it will use the appropriate tools to help you:

```bash
cpe "Create a basic React component that fetches data from an API"
cpe "Fix the bug in my app.js file that's causing the navbar to disappear"
cpe "Write a unit test for the getUserData function in users.js"
cpe -i screenshot.png "What's wrong with this UI layout?"
cpe -i audio_recording.mp3 "Transcribe this meeting and summarize the key points"
```

### Combining Multiple Input Sources

CPE can accept input from multiple sources simultaneously:

```bash
# Combine stdin, file input, and command-line argument
cat error_log.txt | cpe -i screenshot.png "Debug this error and explain what's happening in the screenshot"

# Process multiple files of different types
cpe -i api_spec.yaml -i current_implementation.js "Update this code to match the API spec"

# Feed complex text with special characters via a file rather than command line
cpe -i complex_query.txt "Use this as a reference for the task"
```

## Conversation Management

One of CPE's most powerful features is its sophisticated conversation management system:

### Persistent Conversations

All conversations are automatically saved to a local SQLite database (`.cpeconvo`), allowing you to:

```bash
# Continue your most recent conversation without any special flags
cpe "Can you explain that last part in more detail?"

# Start a new conversation thread
cpe -n "I want to start working on a different project"

# Continue from a specific conversation by ID
cpe -c abc123 "Let's continue with the database schema design"

# View previous conversations
cpe conversation list

# See the full dialog from a specific conversation
cpe conversation print abc123
```

### Conversation Branching

You can create branches from any point in your conversation history:

```bash
# Start a new branch from an earlier conversation point
cpe -c abc123 "What if we used MongoDB instead of PostgreSQL?"

# This creates a new branch while preserving the original conversation path
```

### Interruption Recovery

If you interrupt the model during generation (Ctrl+C):

- The partial response and all actions performed up to that point are automatically saved
- You can continue from that interrupted state without losing context
- The AI will pick up where it left off

### Privacy Mode

For sensitive or temporary inquiries:

```bash
# Use incognito mode to prevent saving the conversation
cpe -G "How do I fix this security vulnerability?"
```

This powerful conversation system allows you to maintain context across multiple sessions, explore alternative solutions
through branching, and never lose your work even if interrupted.

## Command Reference

### Main command

```bash
cpe [flags] "Your prompt here"
```

#### Common Flags

| Flag                   | Short | Description                                                                                            |
|------------------------|-------|--------------------------------------------------------------------------------------------------------|
| `--config`             |       | Path to configuration file (default: ./cpe.yaml or platform-specific user config dir/cpe.yaml)          |
| `--model`,             | `-m`  | Specify which AI model to use                                                                          |
| `--temperature`        | `-t`  | Control randomness (0.0-1.0)                                                                           |
| `--max-tokens`         | `-x`  | Maximum tokens to generate                                                                             |
| `--input`              | `-i`  | Input file(s) of any type (text, images, audio, video) to process (can be repeated or comma-separated) |
| `--new`                | `-n`  | Start a new conversation                                                                               |
| `--continue`           | `-c`  | Continue from specific conversation ID                                                                 |
| `--incognito`          | `-G`  | Don't save conversation history                                                                        |
| `--timeout`            |       | Request timeout (default 5m)                                                                           |

#### Advanced Flags

| Flag                    | Description                                 |
|-------------------------|---------------------------------------------|
| `--top-p`               | Nucleus sampling parameter (0.0-1.0)        |
| `--top-k`               | Top-k sampling parameter                    |  
| `--frequency-penalty`   | Penalize repeated tokens (-2.0-2.0)         |
| `--presence-penalty`    | Penalize tokens already present (-2.0-2.0)  |
| `--number-of-responses` | Number of alternative responses to generate |
| `--thinking-budget`     | Budget for reasoning/thinking capabilities  |

### Model Commands

```bash
# List all configured models
cpe model list   # or cpe models list

# View model details
cpe model info sonnet

# Use custom config file
cpe --config ./my-config.yaml model list
```

### Conversation Management

```bash
# List all conversations
cpe conversation list

# View a specific conversation
cpe conversation print <id>

# Delete a specific conversation
cpe conversation delete <id>

# Delete with all child messages
cpe conversation delete <id> --cascade
```

Note: Conversations are stored in a local SQLite database file named `.cpeconvo` in your current working directory. You
can back up this file or remove it to clear all stored conversations. See
the [Conversation Management](#conversation-management) section for more details.

### Debug Tools

```bash
# Get an overview of files in a directory
cpe tools overview [path]

# Find files related to specific input files
cpe tools related-files file1.go,file2.go

# Count tokens in code files
cpe tools token-count [path]

# List all text files in directory
cpe tools list-files
```

### MCP Tools

```bash
# Initialize a new MCP configuration
cpe mcp init

# List configured MCP servers
cpe mcp list-servers

# Get information about a specific MCP server
cpe mcp info <server_name>

# List tools available from an MCP server
cpe mcp list-tools server_name

# Directly call an MCP tool
cpe mcp call-tool --server server_name --tool tool_name --args '{"param": "value"}'
```

## Configuration

CPE uses a unified YAML configuration file that defines models, MCP servers, and default settings. The configuration file is automatically detected from:

1. Path specified with `--config` flag
2. `./cpe.yaml` or `./cpe.yml` (current directory)
3. Platform-specific user config directory (e.g., `~/.config/cpe/cpe.yaml` on Linux, `$HOME/Library/Application Support/cpe/cpe.yaml` on macOS, or `%AppData%\\cpe\\cpe.yaml` on Windows)

### Configuration Validation

You can validate your configuration file using the `config lint` command:

```bash
# Validate default configuration location
cpe config lint

# Validate specific configuration file
cpe config lint ./path/to/cpe.yaml
```

This command checks for configuration errors without executing other operations, making it useful for CI/CD pipelines and development workflows.

### IDE Integration with JSON Schema

CPE provides a JSON Schema (`schema/cpe-config-schema.json`) for IDE autocompletion and validation. To enable schema support in your editor:

**VS Code**: Create `.vscode/settings.json` in your project:
```json
{
  "yaml.schemas": {
    "https://raw.githubusercontent.com/spachava753/cpe/refs/heads/main/schema/cpe-config-schema.json": ["cpe.yaml", "cpe.yml", "**/cpe.yaml", "**/cpe.yml"]
  }
}
```

**Add to YAML file**: Add this comment at the top of your `cpe.yaml`:
```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/spachava753/cpe/refs/heads/main/schema/cpe-config-schema.json
version: "1.0"
```

**Neovim** (with yaml-language-server): The schema is automatically detected when the file is named `cpe.yaml` or `cpe.yml`.

**IntelliJ/GoLand**: The schema is automatically used when editing `cpe.yaml` files with the YAML plugin enabled.

### Configuration File Format

Create a `cpe.yaml` file with the following structure:

```yaml
version: "1.0"

# Model definitions
models:
  - name: "gpt4"
    id: "gpt-4"
    type: "openai"
    api_key_env: "OPENAI_API_KEY"
    context_window: 128000
    max_output: 4096
    input_cost_per_million: 30
    output_cost_per_million: 60
    generationDefaults:
      temperature: 0.7
      maxTokens: 2048

  - name: "sonnet"
    id: "claude-3-5-sonnet-20241022"
    type: "anthropic"
    api_key_env: "ANTHROPIC_API_KEY"
    context_window: 200000
    max_output: 8192
    input_cost_per_million: 3
    output_cost_per_million: 15
    generationDefaults:
      temperature: 0.5
      maxTokens: 8192

# MCP server definitions (optional)
mcpServers:
  editor:
    command: "mcp-server-editor"
    type: stdio
    timeout: 60
    toolFilter: whitelist
    enabledTools:
      - text_edit
      - shell

# Global defaults (optional)
defaults:
  model: "sonnet"
  systemPromptPath: "./custom-prompt.txt"
  timeout: "5m"
  codeMode:
    enabled: true
    excludedTools:
      - multimedia_tool
  generationParams:
    temperature: 0.7
    maxTokens: 4096
```

### Model Configuration

Each model requires:
- `name`: Unique identifier used with `-m` flag
- `id`: Model ID used by the provider
- `type`: Provider type (`openai`, `anthropic`, `gemini`, `groq`, `cerebras`, `responses`)
- `api_key_env`: Environment variable containing the API key
- `context_window`: Maximum context size
- `max_output`: Maximum output tokens

Optional per-model settings:
- `generationDefaults`: Default generation parameters for this model
  - `temperature`, `topP`, `topK`, `maxTokens`, etc.
- `patchRequest`: Modify HTTP requests sent to model providers
  - `jsonPatch`: Array of JSON Patch operations to apply to request bodies
  - `includeHeaders`: Map of additional HTTP headers to include

### Request Patching

CPE supports patching HTTP requests sent to model providers, allowing you to customize API calls for providers with specific format requirements or to add custom fields. This is useful when working with proxy services like OpenRouter or custom model endpoints.

#### Configuration

Add `patchRequest` to any model definition:

```yaml
models:
  - name: custom-model
    id: provider/model-id
    type: openai
    base_url: https://openrouter.ai/api/v1/
    api_key_env: OPENROUTER_API_KEY
    context_window: 200000
    max_output: 16384
    patchRequest:
      # JSON Patch operations (RFC 6902)
      jsonPatch:
        - op: add
          path: /custom_field
          value: custom_value
        - op: replace
          path: /max_tokens
          value: 8192
      # Additional HTTP headers
      includeHeaders:
        HTTP-Referer: https://my-app.example.com
        X-Title: My AI App
```

#### Supported JSON Patch Operations

- `add`: Add a field to the request body
- `remove`: Remove a field from the request body
- `replace`: Replace an existing field value
- `move`: Move a value from one location to another
- `copy`: Copy a value from one location to another
- `test`: Test that a value matches (validation)

#### Use Cases

1. **Custom provider parameters:**
   ```yaml
   patchRequest:
     jsonPatch:
       - op: add
         path: /provider_specific_param
         value: some_value
   ```

2. **Provider identification headers (e.g., OpenRouter):**
   ```yaml
   patchRequest:
     includeHeaders:
       HTTP-Referer: https://myapp.com
       X-Title: My Application
   ```

3. **Override default values:**
   ```yaml
   patchRequest:
     jsonPatch:
       - op: replace
         path: /temperature
         value: 0.9
   ```

### Parameter Precedence

Generation parameters are merged with this priority (highest to lowest):
1. CLI flags (e.g., `--temperature 0.9`)
2. Model-specific defaults (`generationDefaults` in config)
3. Global defaults (`defaults.generationParams` in config)
4. Built-in defaults

### Example Configuration

See `examples/cpe.yaml` for a complete example with all supported models and MCP servers.

### Using the Configuration

```bash
# Use default config location (./cpe.yaml or ~/.config/cpe/cpe.yaml)
cpe -m sonnet "Your prompt"

# Specify explicit config file
cpe --config ./my-config.yaml -m gpt4 "Your prompt"

# List models from config
cpe model list

# View model details
cpe model info sonnet
```

## Customization

### Customizing CPE

#### .cpeignore

Create a `.cpeignore` file to exclude certain paths from code analysis. It supports all standard Git-ignore syntax
including globs, negation with `!`, and comments:

```
# Ignore build artifacts
node_modules/
*.log
build/
dist/

# But don't ignore specific files
!build/important.js

# Ignore big data files
**/*.csv
**/*.json
```

#### Custom System Prompt

You can customize the AI's system instructions with a template file. This is a Go template that will be filled with data
from the environment where CPE is executed:

```bash
cpe -s path/to/custom_system_prompt.txt "Your prompt"
```

You can set a global `systemPromptPath` in `defaults` and optionally override it per model (
`models[n].systemPromptPath`). The path resolution order is:

1. `--system-prompt-file` CLI flag
2. Model-level `systemPromptPath`
3. Global `defaults.systemPromptPath`

Each prompt file supports Go template syntax, allowing you to include dynamic information. For example:

```
You are an AI assistant helping with a codebase.
Current working directory: {{.WorkingDirectory}}
Git branch: {{.GitBranch}}
User: {{.Username}}
Operating System: {{.OperatingSystem}}
```

This allows you to create contextual system prompts that adapt to the current environment.

### MCP Servers

Model Context Protocol (MCP) servers are configured in the unified configuration file under the `mcpServers` section. See the [Configuration](#configuration) section above for details on configuring MCP servers in your `cpe.yaml` file.

### Code Mode

Code Mode is an advanced feature that allows LLMs to generate and execute Go code to interact with MCP tools. Instead of making discrete tool calls, the LLM writes complete Go programs that can:

- **Compose multiple tools** in a single execution without round-trips
- **Use control flow** like loops and conditionals for complex logic
- **Process data** using Go's standard library (file I/O, JSON, strings, etc.)
- **Handle errors** with proper Go error handling patterns

#### Enabling Code Mode

Add code mode configuration to your `cpe.yaml`:

```yaml
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - multimedia_tool  # Exclude tools returning images/videos
      - stateful_tool    # Exclude tools that maintain state

models:
  - ref: sonnet
    # Inherits defaults.codeMode
  
  - ref: small-model
    # Override for this model only
    codeMode:
      enabled: true
      excludedTools:
        - expensive_tool
```

#### How It Works

When code mode is enabled, CPE exposes a special `execute_go_code` tool that:
1. Accepts complete Go source code from the LLM
2. Compiles it with MCP tools exposed as strongly-typed functions
3. Executes it in a temporary sandbox with configurable timeout
4. Returns the output (stdout/stderr) to the LLM

Example LLM-generated code:
```go
package main

import (
    "context"
    "fmt"
)

func Run(ctx context.Context) error {
    weather, err := GetWeather(ctx, GetWeatherInput{
        City: "Seattle",
        Unit: "fahrenheit",
    })
    if err != nil {
        return err
    }
    
    fmt.Printf("Temperature in Seattle: %.0f°F\n", weather.Temperature)
    return nil
}
```

#### When to Use Code Mode

**Enable code mode when:**
- You have multiple related tools that need to be composed
- Your tasks involve loops, conditionals, or complex data processing
- You want to reduce latency from multiple LLM round-trips
- You need file I/O or standard library functionality

**Exclude tools from code mode when:**
- They return multimedia content (images, video, audio)
- They maintain state across calls (session-based tools)
- They're built-in tools that models are specifically trained to use

#### Security Considerations

Generated code runs with the same permissions as the CPE process. For production use, consider:
- Running CPE in a containerized or sandboxed environment
- Using restricted file permissions
- Setting conservative execution timeouts
- Carefully configuring which tools are exposed

## Examples

### Code Creation

```bash
cpe "Create a Python script that reads a CSV file, calculates statistics, and generates a report"
```

### Code Improvement

```bash
cpe -i path/to/slow_function.js "This function is slow. Can you optimize it?"
```

### Project Setup

```bash
cpe "Set up a new TypeScript project with Express and MongoDB integration"
```

### Debugging

```bash
cpe "I'm getting this error when running my app: [error message]. What might be causing it?"
```

## Known Limitations

- Very large codebases might exceed token limits
- Some complex refactoring operations may require multiple steps
- File overview tool may omit some code details to stay within token limits
- Code analysis primarily supports common languages (Go, JavaScript/TypeScript, Python, Java) using Tree-sitter parsers
- Specialized or less common languages may have limited analysis capabilities
- Performance varies based on the selected AI model

## License

MIT