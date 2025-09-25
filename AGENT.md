# CPE AGENT Guide

This document equips agents and new contributors to work effectively in this repository.

@README.md and @ROADMAP.md provide additional context. See @internal/agent/agent_instructions.txt for the system prompt
used by CPE itself.

## Project Overview

CPE (Chat-based Programming Editor) is a CLI that connects local developer workflows to multiple AI model providers. It
analyzes, edits, and creates code via natural-language prompts, with optional MCP tool integration and persistent
conversation storage.

Key capabilities:

- Multi-model generation via a JSON model catalog
- Tool use (file ops, shell execution, MCP servers)
- Streaming or non-streaming output with “thinking” filtering
- Conversation persistence and branching

BREAKING: Running cpe now requires a model catalog and a model present in that catalog.

## Project structure and organization

- cmd/
    - root.go: main CLI entry and flow (flags, IO processing, dialog, execution)
    - model.go: model list/info from the JSON catalog
    - tools.go: token counting utilities
    - mcp.go, conversation.go, env.go, etc.: supporting subcommands
- internal/
    - agent/: generator adapters, streaming/printing, thinking filter, sysinfo
    - modelcatalog/: Model struct, JSON loader, validation
    - mcp/: MCP config loader/validation and client
    - storage/: SQLite-backed conversation storage (.cpeconvo)
    - token/: token counting builder and directory tree utilities
    - urlhandler/: URL detection and safe downloading
    - version/: version reporting
- scripts/: CI helper utilities (e.g., process PR comments)
- main.go: invokes cmd.Execute()
- gen.go: code generation hooks (if any)
- .github/workflows/: CI (PR comment processing, issue-to-PR)
- .cpemodels.json: example model catalog (JSON)
- .cpemcp.json: optional MCP server configuration
- .cpeignore: ignore patterns for analysis tools

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
# Required: provide a model catalog and select a model present in it
./cpe --model-catalog ./.cpemodels.json -m sonnet "Your prompt"

# With inputs
./cpe --model-catalog ./.cpemodels.json -m sonnet -i path/to/file "Task"

# New conversation or continue specific one
./cpe --model-catalog ./.cpemodels.json -m sonnet -n "Start fresh"
./cpe --model-catalog ./.cpemodels.json -m sonnet -c <message_id> "Continue"

# Streaming off, custom system prompt
./cpe --model-catalog ./.cpemodels.json -m sonnet --no-stream -s prompt.txt "..."
```

Model utilities:

```bash
./cpe model list --model-catalog ./.cpemodels.json
./cpe model info sonnet --model-catalog ./.cpemodels.json
```

Token tools:

```bash
./cpe tools token-count . --model-catalog ./.cpemodels.json -m sonnet
```

Formatting, vetting, testing:

```bash
go fmt ./...
go vet ./...
go test ./...
```

## Code style and conventions

- Language: Go 1.23.x
- Formatting: go fmt; keep imports tidy; idiomatic Go naming
- Errors: wrap with fmt.Errorf("...: %w", err); prefer contextual errors
- Context: pass context.Context where IO, network, or cancellation applies
- CLI: Cobra for flags/commands; minimize side effects in init(); validate flags early
- Packages: keep public surface minimal; prefer internal/ for non-exported APIs
- Concurrency: prefer small, bounded goroutine pools; configurable limits in token tools
- I/O: guard large inputs; processUserInput caps at 50MB per input; detect MIME when needed

## Architecture and design patterns

- CLI layer (cmd/*) orchestrates request:
    1) Parse flags and inputs (stdin/files/URLs) into gai.Blocks
    2) Load model from JSON catalog (required) and init generator
    3) Wrap generator with printing/streaming adapters
    4) Register MCP servers (optional) and tools
    5) Generate, then persist dialog to SQLite (.cpeconvo) unless incognito
- Agent layer (internal/agent):
    - NewBlockWhitelistFilter: filters blocks based on a whitelist of allowed block types
    - Streaming vs response printing adapters
- Model catalog (internal/modelcatalog): decouples model selection (name->type/id/base_url/limits/costs)
- MCP (internal/mcp): declarative server config + validation; client registers tools for use by the generator
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
  sessions. Add entries to @.cpeignore to exclude paths from analysis.
- Network IO: URL inputs are downloaded over HTTP(S) with size limits; validate sources before use.
- Model catalog: only trust catalogs from trusted sources; base_url redirects requests; respect provider ToS.
- MCP servers: stdio/http/sse tools can execute external code; review @.cpemcp.json, limit tools with enabled/disabled
  lists, and pin commands/versions.
- CI: GitHub Actions use repository secrets; avoid echoing secrets in logs.

## Testing

- Primary: go test ./...
- Example integration: see cmd/integration_test.go for URL handling tests
- Token counting: test with representative trees; guard MaxFileSize and concurrency flags

## Build & Commands

Common flags:

- --model-catalog, -m/--model, -t/--temperature, -x/--max-tokens, -i/--input
- -n/--new, -c/--continue, -G/--incognito, -s/--system-prompt-file, --no-stream, --timeout
- --mcp-config for MCP servers; see @.cpemcp.json

Examples:

```bash
cpe --model-catalog ./.cpemodels.json -m gpt5 "Explain the code in cmd/root.go"
cpe --model-catalog ./.cpemodels.json -m sonnet -i README.md "Summarize"
```

## Configuration

Environment variables:

- Provider keys: OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, GROQ_API_KEY, CEREBRAS_API_KEY (as required by
  catalog entries via api_key_env)
- CPE_MODEL: optional default model name (still must exist in the catalog)
- CPE_CUSTOM_URL: optional global base URL override

Files:

- @.cpemodels.json: JSON model catalog (required at runtime via --model-catalog)
- @.cpemcp.json: MCP servers (optional)
- @.cpeignore: ignore patterns for file analysis
- @internal/agent/agent_instructions.txt: system instructions template

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
./cpe --model-catalog ./.cpemodels.json -m sonnet -s custom_prompt.txt "your query"
```

### Example Templates

See `examples/system_prompts/` for ready-to-use templates:
- `project_aware.txt` - Includes project documentation and git status
- `development_env.txt` - Shows installed tools and system resources

## Testing frameworks, conventions, and execution

- Framework: standard library testing package
- Conventions: table-driven tests; use t.Helper() where useful; keep functions pure for testability
- Execution: go test ./...

## Additional resources

- GitHub: https://github.com/spachava753/cpe
- CI workflows: @.github/workflows/pr-comment-cpe.yml, @.github/workflows/issue-to-pr.yml

## Documentation for Go Symbols

When gathering context about symbols like types, global variables, constants, functions and methods, prefer to use go doc command. You may use `go doc -all github.com/example/pkg` to get a full overview of a package, but use sparingly, as it may overwhelm the context window. Alternatively, you may use `go doc github.com/example/pkg.Type` to get documentation about a specific symbol.  