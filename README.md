# CPE - Chat-based Programming Editor

CPE is a local [Agent Client Protocol](https://agentclientprotocol.com/) (ACP) server for AI coding clients. Run it from an ACP-compatible editor such as [Zed](https://zed.dev/), and CPE provides model access, MCP tools, generated Go code execution, file editing, and local session persistence behind that editor UI.

## Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Features](#features)
- [Command Reference](#command-reference)
- [Troubleshooting](#troubleshooting)

## Installation

### Release binaries

```bash
curl -fsSL https://raw.githubusercontent.com/spachava753/cpe/main/install.sh | sh
```

The installer downloads the latest release for macOS or Linux and installs `cpe` to `~/.local/bin` by default. Set `CPE_INSTALL_DIR=/usr/local/bin` or `CPE_INSTALL_VERSION=vX.Y.Z` to customize the install.

### Go install

```bash
go install github.com/spachava753/cpe@latest
```

### From source

```bash
git clone https://github.com/spachava753/cpe.git
cd cpe
go build -o cpe .
```

### Shell completion

```bash
# Bash
source <(cpe completion bash)

# Zsh
source <(cpe completion zsh)

# Fish
cpe completion fish | source

# PowerShell
cpe completion powershell | Out-String | Invoke-Expression
```

## Quick Start

CPE requires a YAML configuration file with at least one model profile. There is no zero-config mode.

### 1. Create `cpe.yaml`

Create `cpe.yaml` in your project directory or user config directory:

- macOS: `~/Library/Application Support/cpe/cpe.yaml`
- Linux: `~/.config/cpe/cpe.yaml`
- Windows: `%AppData%\cpe\cpe.yaml`

```yaml
version: "1.0"

models:
  - ref: sonnet
    display_name: "Claude Sonnet"
    id: claude-sonnet-4-5-20250929
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY
    context_window: 200000
    max_output: 64000
    timeout: 5m
    generationParams:
      temperature: 0.2
```

### 2. Set provider credentials

```bash
export ANTHROPIC_API_KEY="your-api-key"
```

The environment variable name is controlled by `api_key_env` in the selected model profile.

### 3. Configure an ACP client

CPE communicates over stdio JSON-RPC. The client launches `cpe acp serve` and hosts the chat/thread UI.

Zed supports external ACP agents through `agent_servers`. See [Zed External Agents](https://zed.dev/docs/ai/external-agents) and the [Zed ACP page](https://zed.dev/acp/editor/zed).

A minimal Zed settings entry looks like this:

```json
{
  "agent_servers": {
    "CPE": {
      "type": "custom",
      "command": "cpe",
      "args": ["acp", "serve", "--config", "/absolute/path/to/cpe.yaml"],
      "env": {
        "ANTHROPIC_API_KEY": "your-api-key"
      }
    }
  }
}
```

If Zed cannot find `cpe` on its PATH, set `command` to an absolute path such as `/Users/me/.local/bin/cpe`.

### 4. Start an ACP thread

Open your ACP client's agent panel and start a CPE thread. In Zed, open the Agent Panel, create a new external-agent thread, and choose `CPE`. CPE exposes configured model profiles as ACP session configuration options, so model selection happens in the client UI.

## Configuration

### Config Discovery

CPE searches for configuration in this order:

1. `--config` explicit path
2. `./cpe.yaml` or `./cpe.yml`
3. User config directory, usually `~/.config/cpe/cpe.yaml` on Linux and `~/Library/Application Support/cpe/cpe.yaml` on macOS

### Model Profiles

Each `models` entry is a self-contained runtime profile. Use YAML anchors if you want to share repeated fields between profiles.

Supported model `type` values include:

| Provider | Type |
| --- | --- |
| Anthropic | `anthropic` |
| Anthropic on Google Vertex AI | `anthropic_vertex` |
| OpenAI Chat Completions | `openai` |
| OpenAI Responses API | `responses` |
| Google Gemini | `gemini` |
| Groq | `groq` |
| Cerebras | `cerebras` |
| OpenRouter | `openrouter` or `openai` with `base_url` |
| Z.AI | `zai` |

### Full Example

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/spachava753/cpe/refs/heads/main/schema/cpe-config-schema.json
version: "1.0"

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
    systemPromptPath: ./agent_instructions.md
    timeout: 5m
    thinkingValues:
      - name: "Fast"
        value: "1024"
        description: "Lower reasoning budget"
      - name: "Deep"
        value: "8192"
        description: "Higher reasoning budget"
    generationParams:
      temperature: 0.2
      maxGenerationTokens: 12000
    codeMode:
      enabled: true
      maxTimeout: 3600
      largeOutputCharLimit: 20000
    mcpServers:
      search:
        type: http
        url: https://search.example.com/mcp
        headers:
          Authorization: "Bearer ${SEARCH_API_KEY}"
    compaction:
      autoTriggerThreshold: 0.8
      maxAutoCompactionRestarts: 2
      toolDescription: "Compact the current session into a concise continuation summary."
      inputSchema:
        type: object
        properties:
          summary:
            type: string
        required: [summary]
      initialMessageTemplate: |
        Continue from this compacted CPE session.
        Previous leaf message: {{ .PreviousLeafID }}
        Compaction arguments: {{ .ToolArgumentsJSON }}

  - ref: gpt
    display_name: "GPT"
    id: gpt-5.1
    type: responses
    auth_method: oauth
    context_window: 400000
    max_output: 128000
```

### Anthropic on Google Vertex AI

Use `type: anthropic_vertex` for Claude models served through Vertex AI. These profiles use Google Application Default Credentials and IAM instead of Anthropic API keys, so do not set `api_key_env`, `auth_method`, or `base_url`. `patchRequest` remains available for custom headers or JSON request patches.

```yaml
models:
  - ref: vertex-sonnet
    display_name: "Claude Sonnet 4.6 on Vertex AI"
    id: claude-sonnet-4-6
    type: anthropic_vertex
    context_window: 200000
    max_output: 64000
    vertex:
      project_id: my-gcp-project
      region: global
```

Before using the profile, enable the Vertex AI API, enable/request the Claude model in Vertex AI Model Garden, configure Google credentials such as `gcloud auth application-default login` or `GOOGLE_APPLICATION_CREDENTIALS`, and grant an IAM role such as `roles/aiplatform.user` that includes `aiplatform.endpoints.predict`. Model availability varies by `global`, multi-region locations such as `us` and `eu`, and regional locations.

### Environment Variables

| Variable | Description |
| --- | --- |
| `CPE_MODEL` | Default model profile for `cpe model system-prompt` and `cpe mcp ...` when `--model` is omitted |
| `CPE_DB_PATH` | ACP session SQLite database path when `cpe acp serve --db-path` is not passed |

API key variables are configured per model profile through `api_key_env`. OAuth-backed profiles use `auth_method: oauth` and provider account commands where supported. Anthropic Vertex AI profiles use Google Application Default Credentials instead of `api_key_env`.

## Features

### ACP Server Runtime

`cpe acp serve` starts CPE's stdio ACP server. The ACP client owns the visible interaction loop; CPE owns model runtime assembly, tool execution, session persistence, and protocol updates.

CPE supports ACP session creation, loading, resumption, closing, deletion, and forking where the client exposes those capabilities. Session history is stored locally in SQLite, defaulting to `./.cpeconvo`.

### Model Selection

ACP sessions default to the first configured model profile. CPE exposes model profiles and configured thinking levels as ACP session configuration options, so compatible clients can switch models or reasoning levels from the UI.

ACP sessions choose the model through the client's session configuration. The CLI only needs a model ref for commands that inspect profile-specific data:

```bash
cpe model system-prompt --model sonnet
cpe mcp list-servers --model sonnet
```

Set `CPE_MODEL=sonnet` to omit `--model` for those commands.

### MCP Tool Integration

CPE is an MCP client inside each ACP session. Configure MCP servers on a model profile with `mcpServers`:

| Type | Description |
| --- | --- |
| `stdio` | Local process over stdin/stdout |
| `http` | HTTP endpoint |
| `sse` | Server-Sent Events endpoint |

CPE also accepts MCP servers forwarded by the ACP client and merges them with configured servers. Duplicate server names fail fast so tool behavior is not ambiguous.

The bundled `text_edit` file editing tool is registered directly by CPE and does not require MCP configuration. Set `disable_edit_tool: true` on a model profile to omit it.

Use the MCP inspection commands to inspect and test configured servers:

```bash
cpe mcp list-servers --model sonnet
cpe mcp list-tools search --model sonnet
cpe mcp list-tools search --show-all --model sonnet
cpe mcp info search --model sonnet
cpe mcp call-tool --server search --tool web_search --args '{"query":"golang"}' --model sonnet
```

### Code Mode

Code Mode registers an `execute_go_code` tool. The model writes a complete Go source file implementing `Run(ctx context.Context) ([]mcp.Content, error)`; CPE compiles and runs it in a temporary harness with timeout and output limits.

```yaml
models:
  - ref: sonnet
    # ...model provider fields...
    codeMode:
      enabled: true
      maxTimeout: 3600
      largeOutputCharLimit: 20000
```

Generated code can use the Go standard library, process local files, and return text, images, PDFs, or audio through MCP content blocks. MCP tools remain normal conversational tools; they are not exposed as generated Go bindings.

### System Prompts and Skills

Set `systemPromptPath` on a model profile to render a prompt template for that profile. CPE discovers skills from `./.agents/skills` and `~/.agents/skills` and exposes model-visible skills as `.Skills` template data when rendering system prompts. Skill frontmatter is available through `.Metadata`; skills with `disable-model-invocation: true` are omitted from `.Skills` but remain user-invocable through `/skill:<name>` slash commands.

```markdown
{{- range $skill := .Skills }}
- {{ $skill.Name }}: {{ $skill.Description }} ({{ $skill.Path }})
{{- end }}
```

When a user prompt references a known skill command such as `/skill:domain-modeling`, CPE expands that text to the skill path before generation and persistence. Unknown `/skill:<name>` references are left unchanged. ACP clients receive refreshed available skill command metadata before prompt turns and session config option updates.

Inspect the rendered prompt with:

```bash
cpe model --model sonnet system-prompt
```

### Account Authentication

CPE can store OAuth credentials for supported provider subscription flows.

```bash
cpe account login anthropic
cpe account login openai
cpe account usage openai
cpe account usage openai --watch
cpe account usage openai --raw
cpe account logout openai
```

For OpenAI Responses API profiles that use a ChatGPT subscription, configure `type: responses` and `auth_method: oauth`.

### Request Patching

Use `patchRequest` for advanced provider-specific headers or JSON Patch operations.

```yaml
models:
  - ref: qwen
    id: qwen/qwen3-max
    type: openai
    base_url: https://openrouter.ai/api/v1/
    api_key_env: OPENROUTER_API_KEY
    context_window: 262144
    max_output: 32768
    patchRequest:
      includeHeaders:
        HTTP-Referer: https://my-app.example.com
        X-Title: My AI App
```

## Command Reference

```text
cpe [command]

Root flags:
  --config string   Path to YAML configuration file
  -v, --version     Print the version number and exit

Commands:
  acp
    serve           Start the stdio ACP server
                    --db-path string  ACP session SQLite database path

  model, models     Inspect configured model profiles
    list, ls        List configured model refs
    info <ref>      Show model profile details
    system-prompt   Render the selected model profile's system prompt
                    -m, --model string  Model profile ref for profile-specific inspection

  mcp               Inspect MCP servers for a selected model profile
    list-servers, ls-servers
    list-tools, ls-tools <server>
    info <server>
    call-tool --server <server> --tool <tool> --args '{}'
    code-desc       Print the execute_go_code tool description
                    -m, --model string  Model profile ref whose MCP servers should be inspected

  account           Manage provider account credentials and usage
    login <provider>
    logout <provider>
    usage <provider> [--watch | --raw]

  completion        Generate shell completion scripts
```

## Troubleshooting

### `configuration file not found`

Create `cpe.yaml` in the current directory or user config directory, or pass `--config /path/to/cpe.yaml` from the ACP client command args.

### ACP client cannot start CPE

Use an absolute path for the `command` field if the editor process does not inherit your shell PATH. In Zed, inspect ACP traffic with `dev: open acp logs` from the Command Palette.

### API key missing

Ensure the environment variable named by `api_key_env` is visible to the ACP server process. For editor-launched processes, put required variables in the client's agent server `env` block or in the environment that launches the editor.

For `anthropic_vertex` profiles, CPE does not use `api_key_env`; configure Google Application Default Credentials or `GOOGLE_APPLICATION_CREDENTIALS` for the ACP server process instead.

### Model profile not found

Run `cpe model list` to inspect configured refs. ACP sessions can only select profiles present in the loaded config file.

### MCP server fails to start

Use the MCP inspection commands:

```bash
cpe mcp list-servers --model <model>
cpe mcp list-tools <server-name> --model <model>
cpe mcp info <server-name> --model <model>
```

For `stdio` servers, verify the command path, arguments, environment, and executable permissions. For `http` or `sse` servers, verify the URL and headers.

### Timeout errors

Increase the selected model profile's `timeout` or the MCP server's per-server `timeout`. Code Mode has its own `codeMode.maxTimeout` cap.

## Contributing

Contributions are welcome. See [AGENTS.md](./AGENTS.md) for development guidelines, code style, and test commands.

## License

This project is licensed under the [MIT License](LICENSE).
