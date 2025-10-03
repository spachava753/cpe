# CPE AGENT Guide

This document equips agents and new contributors to work effectively in this repository.

@README.md and @ROADMAP.md provide additional context. See examples/agent_instructions.prompt for the system prompt
used by CPE itself.

## Project Overview

CPE (Chat-based Programming Editor) is a CLI that connects local developer workflows to multiple AI model providers. It
analyzes, edits, and creates code via natural-language prompts, with optional MCP tool integration and persistent
conversation storage.

Key capabilities:

- Multi-model generation via unified YAML/JSON configuration
- Tool use (file ops, shell execution, MCP servers)
- Streaming or non-streaming output with “thinking” filtering
- Conversation persistence and branching

BREAKING: Running cpe now requires a unified configuration file with at least one model defined.

## Project structure and organization

- cmd/
    - root.go: main CLI entry and flow (flags, IO processing, dialog, execution)
    - model.go: model list/info from the JSON catalog
    - tools.go: token counting utilities
    - system_prompt.go: system prompt template handling
    - mcp.go, conversation.go, etc.: supporting subcommands
- internal/
    - agent/: generator adapters, streaming/printing, thinking filter, sysinfo
    - config/: Unified configuration loading, validation, parameter merging, and Model struct
    - mcp/: MCP config validation and client
    - storage/: SQLite-backed conversation storage (.cpeconvo)
    - token/: token counting builder and directory tree utilities
    - urlhandler/: URL detection and safe downloading
    - version/: version reporting
- scripts/: CI helper utilities (e.g., process PR comments)
- main.go: invokes cmd.Execute()
- gen.go: code generation hooks (if any)
- .github/workflows/: CI (PR comment processing, issue-to-PR)
- examples/cpe.yaml: example unified configuration (YAML)
- (No ignore patterns - all files are analyzed)

## Build, test, and development commands

Build/install:

```bash
# Build local binary
go build -o cpe .

# Install to GOPATH/bin
go install ./...
```

Run (typical dev):

```bash
# Required: provide a unified configuration with models defined
./cpe --config ./examples/cpe.yaml -m sonnet "Your prompt"

# With inputs
./cpe --config ./examples/cpe.yaml -m sonnet -i path/to/file "Task"

# Auto-detect config from current directory or user config
./cpe -m sonnet "Your prompt"  # Uses ./cpe.yaml or ~/.config/cpe/cpe.yaml

# New conversation or continue specific one
./cpe --config ./examples/cpe.yaml -m sonnet -n "Start fresh"
./cpe --config ./examples/cpe.yaml -m sonnet -c <message_id> "Continue"

# Streaming off, custom system prompt
./cpe --config ./examples/cpe.yaml -m sonnet --no-stream -s prompt.txt "..."
```

Model utilities:

```bash
./cpe model list --config ./examples/cpe.yaml
./cpe model info sonnet --config ./examples/cpe.yaml
```

Token tools:

```bash
./cpe tools token-count . --config ./examples/cpe.yaml -m sonnet
```

Formatting, vetting, testing:

```bash
go fmt ./...
go vet ./...
go test ./...
```

## Code style and conventions

- Language: Go 1.24.0
- Formatting: go fmt; keep imports tidy; idiomatic Go naming
- Errors: wrap with fmt.Errorf("...: %w", err); prefer contextual errors
- Context: pass context.Context where IO, network, or cancellation applies
- CLI: Cobra for flags/commands; minimize side effects in init(); validate flags early
- Packages: keep public surface minimal; prefer internal/ for non-exported APIs
- Concurrency: prefer small, bounded goroutine pools; configurable limits in token tools
- I/O: guard large inputs; processUserInput caps at 50MB per input; detect MIME when needed
- **String literals**: Use raw strings (``) for multi-line strings instead of multiple fmt.Println calls

## Commit message conventions

- Use Conventional Commits: type(scope)!: short summary
- Common types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
- Imperative, present tense; no trailing period; scope optional; add ! for breaking changes
- Include body/footer when helpful; use BREAKING CHANGE: and issue refs (e.g., Closes #123)

## Architecture and design patterns

- CLI layer (cmd/*) orchestrates request:
    1) Parse flags and inputs (stdin/files/URLs) into gai.Blocks
    2) Load unified config (required) with models and MCP servers
    3) Select model and merge generation parameters (model defaults + global defaults + CLI overrides)
    4) Create generator with tool capabilities
    5) Generate, then persist dialog to SQLite (.cpeconvo) unless incognito
- Config layer (internal/config):
    - Unified YAML/JSON configuration loading with search paths
    - Parameter precedence: CLI flags > model defaults > global defaults
    - Environment variable expansion in config values
- Agent layer (internal/agent):
    - NewBlockWhitelistFilter: filters blocks based on a whitelist of allowed block types
    - Streaming vs response printing adapters
- MCP (internal/mcp): server validation; client registers tools for use by the generator
- Storage (internal/storage): threadlike message DAG with parent IDs

## Testing guidelines

- Use go test ./...; write table-driven unit tests
- Prefer httptest for HTTP; avoid real network calls
- Keep tests deterministic; use short timeouts; avoid sleeping where possible
- Isolate filesystem effects; clean up temp files; do not depend on developer-local state
- For dialog storage, prefer temp DB paths when adding tests
- Name tests with _test.go; keep per-package tests close to implementation

## Security considerations

- Secrets: models read API keys from env per catalog entry api_key_env (e.g., OPENAI_API_KEY, ANTHROPIC_API_KEY,
  GROQ_API_KEY, CEREBRAS_API_KEY). Do not log keys.
- Conversation data: .cpeconvo contains prompts, outputs, and file references. Use --incognito (-G) for sensitive
  sessions.
- Network IO: URL inputs are downloaded over HTTP(S) with size limits; validate sources before use.
- Model catalog: only trust catalogs from trusted sources; base_url redirects requests; respect provider ToS.
- MCP servers: stdio/http/sse tools can execute external code; review .cpemcp.json, limit tools with enabled/disabled
  lists, and pin commands/versions.
- CI: GitHub Actions use repository secrets; avoid echoing secrets in logs.

## Testing

- Primary: go test ./...
- Example integration: see cmd/integration_test.go for URL handling tests
- Token counting: test with representative trees; guard MaxFileSize and concurrency flags

## Build & Commands

Common flags:

- --config, -m/--model, -t/--temperature, -x/--max-tokens, -i/--input
- -n/--new, -c/--continue, -G/--incognito, -s/--system-prompt-file, --no-stream, --timeout

Examples:

```bash
cpe --config ./examples/cpe.yaml -m gpt5 "Explain the code in cmd/root.go"
cpe --config ./examples/cpe.yaml -m sonnet -i README.md "Summarize"
# or with auto-detected config:
cpe -m sonnet -i README.md "Summarize"
```

## Configuration

Environment variables:

- Provider keys: OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, GROQ_API_KEY, CEREBRAS_API_KEY (as required by
  config entries via api_key_env)
- CPE_MODEL: optional default model name (still must exist in the configuration)
- CPE_CUSTOM_URL: optional global base URL override

Files:

- examples/cpe.yaml: unified configuration example (YAML)
- examples/agent_instructions.prompt: system instructions template

Configuration search paths:

1. Explicit --config path
2. ./cpe.yaml or ./cpe.yml (current directory)
3. ~/.config/cpe/cpe.yaml or ~/.config/cpe/cpe.yml (user config directory)

## System Prompt Template Functions

The system prompt templates support custom functions for dynamic content:

### Available Functions

- `fileExists(path)` - Returns true if file exists, false otherwise
- `includeFile(path)` - Returns file contents as string, empty string if file doesn't exist
- `exec(command)` - Executes bash command and returns stdout (trimmed), empty string on error
- All functions from `github.com/Masterminds/sprig/v3` (text/template): e.g., `upper`, `lower`, `default`, `toJson`, and more. See https://masterminds.github.io/sprig/ for full list.

### Example Usage

Create a custom system prompt template (`custom_prompt.txt`):

```
You are an AI assistant working on: {{.WorkingDir}}

{{if fileExists "README.md"}}
Project documentation:
{{includeFile "README.md"}}
{{end}}

Current git status:
{{exec "git status --short"}}

Python version: {{exec "python --version 2>/dev/null || echo 'Not installed'"}}
```

Use it with:
```bash
./cpe --model-catalog ./examples/models.json -m sonnet -s custom_prompt.txt "your query"
```

### Example Templates

See `examples/` for ready-to-use templates:
- `project_aware.prompt` - Includes project documentation and git status
- `development_env.prompt` - Shows installed tools and system resources
- `agent_instructions.prompt` - System instructions template

## Testing frameworks, conventions, and execution

- Framework: standard library testing package
- Conventions: table-driven tests; use t.Helper() where useful; keep functions pure for testability
- Execution: go test ./...

## Additional resources

- GitHub: https://github.com/spachava753/cpe
- CI workflows: @.github/workflows/pr-comment-cpe.yml, @.github/workflows/issue-to-pr.yml

## Documentation for Go Symbols

When gathering context about symbols like types, global variables, constants, functions and methods, prefer to use `go doc` command. You may use `go doc github.com/example/pkg.Type` to get documentation about a specific symbol. Avoid using `go doc -all` as it may overwhelm your context window. Instead, if you need to perform a search or fuzzy search for a symbol, feed the output of `go doc -all` into a cli like `rg`, `fzf`, etc.   