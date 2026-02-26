# CPE – Chat-based Programming Editor

<p align="center">
  <strong>A powerful CLI that brings AI directly to your terminal for code analysis, editing, and automation.</strong>
</p>

<p align="center">
  <a href="#installation">Installation</a> •
  <a href="#quick-start">Quick Start</a> •
  <a href="#features">Features</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#troubleshooting">Troubleshooting</a>
</p>

---

CPE connects your local development workflow to multiple AI providers through a single, unified interface. Write natural language prompts, and CPE handles the rest—whether you're analyzing code, making edits, or automating complex tasks.

## ✨ Why CPE?

- **One tool, many models**: Switch between Claude, GPT, Gemini, and more with a simple flag
- **Tool integration**: Connect to MCP servers for file editing, shell commands, web search, and more
- **Conversation memory**: Resume previous conversations or branch off in new directions
- **Code Mode**: Let the AI write and execute Go code to accomplish complex multi-step tasks
- **Privacy-first**: Your data stays local; incognito mode for sensitive work

## 🚀 Installation

### Using Go (recommended)

```bash
go install github.com/spachava753/cpe@latest
```

### From source

```bash
git clone https://github.com/spachava753/cpe.git
cd cpe
go build -o cpe .
```

### Shell Completion

CPE supports shell autocompletion for faster command entry:

```bash
# Bash (add to ~/.bashrc)
source <(cpe completion bash)

# Zsh (add to ~/.zshrc)
source <(cpe completion zsh)

# Fish
cpe completion fish | source

# PowerShell
cpe completion powershell | Out-String | Invoke-Expression
```

## ⚡ Quick Start

> **Note**: CPE requires a configuration file to define which models and tools to use. There's no zero-config mode—you'll need to set up at least one model before getting started.

### 1. Create a configuration file

Create a `cpe.yaml` in your project directory or in your user config directory:
- **macOS**: `~/Library/Application Support/cpe/cpe.yaml`
- **Linux**: `~/.config/cpe/cpe.yaml`
- **Windows**: `%AppData%\cpe\cpe.yaml`

```yaml
version: "1.0"

models:
  - ref: sonnet
    display_name: "Claude Sonnet"
    id: claude-sonnet-4-5-20250929
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY  # You choose the env var name
    context_window: 200000
    max_output: 64000

defaults:
  model: sonnet
  timeout: 5m
```

> **Tip**: You can quickly add models from the [models.dev](https://models.dev) registry:
> ```bash
> # Add a model from the registry
> cpe config add anthropic/claude-sonnet-4-20250514 --ref sonnet
> ```

### 2. Set your API key

```bash
# Use whatever env var name you specified in api_key_env
export ANTHROPIC_API_KEY="your-api-key"
```

### 3. Start chatting

```bash
# Ask a question
cpe "Explain what this project does"

# Analyze specific files
cpe -i main.go -i README.md "What are the main entry points?"

# Use a different model
cpe -m gpt4 "Help me refactor this function"
```

## 🎯 Features

### Multi-Model Support

CPE works with all major AI providers:

| Provider | Type | Example Models |
|----------|------|----------------|
| Anthropic | `anthropic` | Claude Opus, Sonnet, Haiku |
| OpenAI | `openai`, `responses` | GPT-4o, GPT-5, o1 |
| Google | `gemini` | Gemini Pro, Flash |
| Groq | `groq` | Llama, Mixtral (fast inference) |
| Z.AI (ZhipuAI) | `zai` | GLM-4 |
| Cerebras | `cerebras` | Llama (fast inference) |
| OpenRouter | `openrouter` or `openai` (with base_url) | Any OpenRouter model |

Switch models on the fly:
```bash
cpe -m sonnet "Write a test for this function"
cpe -m flash "Quick question about Go syntax"
```

### Conversation Persistence

CPE remembers your conversations, stored locally in `.cpeconvo`:

```bash
# Continue your last conversation
cpe "Now add error handling to that code"

# Start fresh
cpe -n "Let's work on a new feature"

# Continue from a specific point
cpe -c abc123 "Actually, let's try a different approach"

# View conversation history (aliases: convo, conv)
cpe conversation list       # alias: ls

# Print a specific conversation (aliases: show, view)
cpe conversation print abc123

# Delete a conversation (aliases: rm, remove)
cpe conversation delete abc123

# Delete with cascade (removes children too)
cpe conversation delete abc123 --cascade
```

### Input Files & URLs

Feed context directly to CPE:

```bash
# Include files
cpe -i src/main.go -i src/utils.go "Find any bugs"

# Include URLs
cpe -i https://example.com/docs.md "Summarize this documentation"

# Mix files and stdin
echo "Additional context" | cpe -i config.yaml "Help me configure this"
```

### MCP Tool Integration

Connect external tools via the [Model Context Protocol](https://modelcontextprotocol.io/). CPE supports three transport types:

| Type | Description | Use Case |
|------|-------------|----------|
| `stdio` | Local process via stdin/stdout | Local tools, CLIs |
| `http` | HTTP/HTTPS endpoint | Remote APIs, cloud services |
| `sse` | Server-Sent Events | Streaming, real-time tools |

```yaml
# cpe.yaml
mcpServers:
  # Local tool via stdio
  editor:
    command: "editor-mcp"
    type: stdio
    timeout: 60
    enabledTools:
      - text_edit
      - shell

  # Remote tool via HTTP
  search:
    url: "https://search.example.com/mcp"
    type: http
    headers:
      Authorization: "Bearer ${API_KEY}"

  # SSE-based server
  streaming:
    url: "https://streaming.example.com/sse"
    type: sse
    headers:
      X-API-Key: "${MCP_API_KEY}"
```

**Tool Filtering**: Control which tools are exposed to the AI:
- `enabledTools`: Whitelist—only these tools are available
- `disabledTools`: Blacklist—all tools except these are available

Now the AI can edit files, run commands, and search the web!

#### Debugging MCP Integrations

CPE provides commands to help debug MCP server connections:

```bash
# List all configured MCP servers (alias: ls-servers)
cpe mcp list-servers

# List tools available from a specific server (alias: ls-tools)
cpe mcp list-tools editor

# Show all tools including filtered ones
cpe mcp list-tools editor --show-all

# Show only filtered-out tools
cpe mcp list-tools editor --show-filtered

# Get detailed info about a server
cpe mcp info editor

# Call a tool directly for testing
cpe mcp call-tool --server editor --tool text_edit --args '{"path": "test.txt", "text": "hello"}'

# View the execute_go_code tool description (for code mode)
cpe mcp code-desc
```

### Code Mode

Code Mode lets the AI write and execute Go code to accomplish complex tasks in a single step:

```yaml
defaults:
  codeMode:
    enabled: true
    localModulePaths:
      - ../my-go-helpers
      - /Users/me/dev/shared-go-utils
```

With Code Mode, the AI can:
- Chain multiple tool calls together
- Use loops and conditionals
- Process data in parallel
- Access Go's standard library

When `localModulePaths` is configured, generated code runs inside an ephemeral Go workspace. This lets `go mod tidy`, `go build`, and import auto-correction resolve local modules without per-run manual setup.

Example: "Find all TODO comments, group them by file, and create a summary report" becomes a single Go program that the AI writes and CPE executes.

### OAuth Authentication

Use your Claude Pro/Max subscription directly:

```bash
# Authenticate with Anthropic
cpe auth login anthropic

# Check status
cpe auth status

# Refresh OAuth tokens
cpe auth refresh anthropic

# Logout
cpe auth logout anthropic
```

### Request Patching

For advanced use cases, you can patch API requests with custom headers or JSON modifications. This is useful for:
- OpenRouter's required `HTTP-Referer` header
- Custom authentication schemes
- Adding provider-specific metadata

```yaml
models:
  - ref: qwen
    id: qwen/qwen3-max
    type: openai
    base_url: https://openrouter.ai/api/v1/
    api_key_env: OPENROUTER_API_KEY
    patchRequest:
      includeHeaders:
        HTTP-Referer: https://my-app.example.com
        X-Title: My AI App
      # Optional: JSON Patch operations
      # jsonPatch:
      #   - op: add
      #     path: /custom_field
      #     value: custom_value
```

## ⚙️ Configuration

### Configuration File Locations

CPE searches for configuration in this order:
1. `--config` flag (explicit path)
2. `./cpe.yaml` (current directory)
3. User config directory:
   - **macOS**: `~/Library/Application Support/cpe/cpe.yaml`
   - **Linux**: `~/.config/cpe/cpe.yaml`
   - **Windows**: `%AppData%\cpe\cpe.yaml`

### Full Configuration Example

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/spachava753/cpe/refs/heads/main/schema/cpe-config-schema.json
version: "1.0"

# MCP servers for tool access
mcpServers:
  editor:
    command: "editor-mcp"
    type: stdio
    timeout: 60
    enabledTools:
      - text_edit
      - shell

# Define your models
models:
  - ref: sonnet
    display_name: "Claude Sonnet"
    id: claude-sonnet-4-5-20250929
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    context_window: 200000
    max_output: 64000
    input_cost_per_million: 3
    output_cost_per_million: 15

  - ref: flash
    display_name: "Gemini Flash"
    id: gemini-flash-latest
    type: gemini
    api_key_env: GEMINI_API_KEY
    context_window: 1048576
    max_output: 65536

  - ref: gpt4
    display_name: "GPT-4o"
    id: gpt-4o
    type: openai
    api_key_env: OPENAI_API_KEY

  - ref: glm
    display_name: "Z.AI GLM-4"
    id: glm-4
    type: zai
    api_key_env: Z_API_KEY
    context_window: 128000
    max_output: 4096

# Global defaults
defaults:
  model: sonnet
  systemPromptPath: "./prompts/agent.md"
  timeout: 5m
  codeMode:
    enabled: true
    maxTimeout: 3600
    localModulePaths:
      - ../my-go-helpers
  # Generation parameters control LLM behavior
  generationParams:
    temperature: 0.7      # Controls randomness (0.0 = deterministic, 1.0 = creative)
    # topP: 0.9           # Nucleus sampling threshold
    # topK: 40            # Top-k sampling parameter
    # frequencyPenalty: 0 # Penalize repeated tokens (-2.0 to 2.0)
    # presencePenalty: 0  # Penalize tokens already in context (-2.0 to 2.0)
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `CPE_MODEL` | Default model to use (overridden by `-m` flag) |
| `CPE_VERBOSE_SUBAGENT` | Show detailed subagent output |

> **Note**: API keys are configured per-model via the `api_key_env` field. You choose the environment variable name—there are no hardcoded defaults. For example, you could use `MY_ANTHROPIC_KEY`, `OPENAI_API_KEY`, or any name you prefer.

### Model Management

```bash
# List configured models (aliases: models, ls)
cpe model list

# Show model details
cpe model info sonnet

# View the rendered system prompt for a model (uses -m flag)
cpe model system-prompt -m sonnet

# Add a model from models.dev registry
cpe config add anthropic/claude-sonnet-4-20250514

# Add with custom ref
cpe config add anthropic/claude-sonnet-4-20250514 --ref claude

# Remove a model
cpe config remove claude
```

## 📚 Examples

### Code Review

```bash
cpe -i main.go -i utils.go "Review this code for potential bugs and suggest improvements"
```

### Refactoring

```bash
cpe -i legacy_module.py "Refactor this to use modern Python patterns. Update the file directly."
```

### Documentation

```bash
cpe -i api.go -i handlers.go "Generate comprehensive documentation for all public functions"
```

### Debugging

```bash
cpe -i error.log -i src/handler.go "Why is this error happening? Suggest a fix."
```

### Quick Tasks

```bash
# Generate a .gitignore
cpe "Create a .gitignore for a Go project"

# Explain code
cpe -i complex_algorithm.go "Explain what this does step by step"

# Convert formats
echo '{"name": "test"}' | cpe "Convert this JSON to YAML"
```

## 🧩 Skills System

CPE can be extended with **skills**—reusable, composable capabilities that provide specialized knowledge and workflows.

### What Are Skills?

A skill is a directory containing a `SKILL.md` file with YAML frontmatter (name and description) followed by instructions, examples, or reference material. Skills are discovered and rendered into the system prompt.

### Configuring Skills

Skills locations are **user-defined** in your system prompt template using the `{{ skills }}` function. There are no hardcoded defaults—you specify exactly which directories to scan:

```markdown
<!-- In your agent_instructions.md template -->
{{- $skills := skills "./skills" "~/my-custom-skills" "/shared/team-skills" -}}
{{- if $skills }}
<skills>
{{- range $skill := $skills }}
  <skill name={{ printf "%q" $skill.Name }}>
    <description>{{ $skill.Description }}</description>
    <path>{{ $skill.Path }}</path>
  </skill>
{{- end }}
</skills>
{{- end }}
```

The `skills` function:
- Accepts any number of directory paths
- Scans each for subdirectories containing `SKILL.md`
- Returns a list of skill objects (`name`, `description`, `path`) so your template controls the output format (XML, JSON, CSV, etc.)

### Example Skill Structure

```
skills/
└── github-issue/
    └── SKILL.md
```

```markdown
---
name: github-issue
description: Create and manage GitHub issues with proper templates
---

# GitHub Issue Skill

Instructions for creating well-formatted GitHub issues...
```

### Creating Your Own Skills

Create a directory with a `SKILL.md` file anywhere you like, then reference that path in your system prompt template:

```markdown
{{- $skills := skills "./my-project-skills" "~/my-global-skills" -}}
{{/* format $skills however you want */}}
```

For examples of well-structured skills, see the `skills/` directory in the CPE repository—these are skills used for CPE's own development but serve as good templates for creating your own.

## 🔧 CLI Reference

```
cpe [flags] [prompt]

Core Flags:
  -m, --model string           Specify the model to use
  -i, --input strings          Input files or URLs to process
  -n, --new                    Start a new conversation
  -c, --continue string        Continue from a specific conversation ID
  -G, --incognito              Don't save conversation to storage
      --config string          Path to configuration file
      --skip-stdin             Skip reading from stdin
  -v, --version                Print version and exit

Generation Parameters:
  -t, --temperature float      Sampling temperature (0.0 - 1.0)
  -x, --max-tokens int         Maximum tokens to generate
  -b, --thinking-budget string Budget for reasoning capabilities
      --top-p float            Nucleus sampling parameter (0.0 - 1.0)
      --top-k uint             Top-k sampling parameter
      --frequency-penalty float Frequency penalty (-2.0 - 2.0)
      --presence-penalty float  Presence penalty (-2.0 - 2.0)
      --number-of-responses uint Number of responses to generate
      --timeout string         Request timeout (e.g., '5m', '30s')

Advanced:
      --custom-url string      Custom base URL for the model provider API
      --verbose-subagent       Show verbose subagent output including full tool payloads

Commands:
  auth          Manage OAuth authentication (login, logout, status, refresh)
  config        Manage configuration (add, remove models)
  conversation  Manage conversation history [aliases: convo, conv]
                ├─ list    List conversations [alias: ls]
                ├─ print   Print a conversation [aliases: show, view]
                └─ delete  Delete conversations [aliases: rm, remove]
  model         List and inspect models [alias: models]
                ├─ list          List models [alias: ls]
                ├─ info          Show model details
                └─ system-prompt Show rendered system prompt
  mcp           MCP tools
                ├─ serve        Run CPE as MCP server
                ├─ list-servers List servers [alias: ls-servers]
                ├─ list-tools   List tools [alias: ls-tools]
                ├─ call-tool    Call a tool
                ├─ info         Server info
                └─ code-desc    Show code mode description
  completion    Generate shell autocompletion scripts (bash, zsh, fish, powershell)
```

## 🤖 Subagent Mode

CPE can run as an MCP server, enabling powerful agent composition patterns:

```yaml
# subagent.yaml
version: "1.0"

subagent:
  name: "code_reviewer"
  description: "Reviews code and provides detailed feedback"

models:
  - ref: sonnet
    id: claude-sonnet-4-5-20250929
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY

defaults:
  model: sonnet
  systemPromptPath: ./reviewer_prompt.md
```

Run the subagent:
```bash
cpe mcp serve --config ./subagent.yaml
```

Use it from a parent CPE instance:
```yaml
mcpServers:
  code_reviewer:
    command: cpe
    args: ["mcp", "serve", "--config", "./subagent.yaml"]
    type: stdio
```

## 🔧 Troubleshooting

### Common Issues

**"API key missing: ANTHROPIC_API_KEY not set"**
- Ensure the environment variable specified in `api_key_env` is exported in your shell
- Check for typos in the variable name in both your shell and `cpe.yaml`
- For OAuth authentication, run `cpe auth login anthropic` instead of using API keys

**"no configuration file found"**
- Create a `cpe.yaml` in the current directory or your user config directory (see [Configuration File Locations](#configuration-file-locations))
- Use `--config /path/to/cpe.yaml` to specify an explicit path

**"model not found: xyz"**
- Run `cpe model list` to see available models
- Check that the `ref` in your `cpe.yaml` matches what you're requesting with `-m`

**MCP server fails to start**
- Check that the command exists and is executable
- For stdio servers, ensure the `command` path is correct
- Use `cpe mcp list-tools <server>` to debug tool availability
- Check server logs with `cpe mcp info <server>`

**"context length exceeded"**
- Use fewer input files or truncate large files
- Check `context_window` in your model config matches the provider's limits
- Start a new conversation with `-n` to clear history

**Timeout errors**
- Increase `timeout` in `defaults` or use `--timeout 10m`
- For MCP servers, adjust the per-server `timeout` value

### Debug Tips

```bash
# Check which model is being used
cpe model info <ref>

# View the system prompt being sent (uses -m flag for model selection)
cpe model system-prompt -m <ref>

# Test MCP server connectivity
cpe mcp list-tools <server-name>

# Enable verbose output for subagents
cpe --verbose-subagent "your prompt"
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

See [AGENTS.md](./AGENTS.md) for detailed development guidelines, code style, and testing practices.

## 📄 License

This project is licensed under the [MIT License](LICENSE).
