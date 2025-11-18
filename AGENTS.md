## Project Overview

CPE (Chat-based Programming Editor) is a CLI that connects local developer workflows to multiple AI model providers. It analyzes, edits, and creates code via natural-language prompts, with optional MCP tool integration and persistent conversation storage. Link: https://github.com/spachava753/cpe

Key capabilities:

- Multi-model generation via unified YAML/JSON configuration
- Tool use via MCP servers
- Streaming or prettified non-streaming output
- Conversation persistence and branching

## Project structure and organization

- `cmd/`: The package which has the cobra commands that user invokes
- `internal/`: Hosts all of the actual business logic and utilities
    -
    `agent/`: Package that hosts generator adapters, streaming/printing, thinking filter, system prompt generation, and agent creation to execute a user query
    - `config/`: Package that hosts configuration loading, validation, parameter merging, and config specific types
    - `mcp/`: Package that hosts MCP config validation and client, as well as code for connecting to MPC servers
    - `storage/`: Package that hosts SQLite-backed conversation storage (.cpeconvo) and related persistence code
    - `urlhandler/`: Package that hosts utility code for URL detection and safe downloading
    - `version/`: Package that hosts CLI version reporting
- `main.go`: invokes cmd.Execute()
- `gen.go`: code generation hooks, like sqlc codegen
- `examples/`: Folder that hosts examples of configuration, system prompt templates, etc.
- `docs/`: Folder that hosts markdown files documenting various things like PRDs, specs, etc.

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

Schema and configuration:

```bash
# Generate JSON Schema for config
go generate ./internal/config/

# Validate configuration
./cpe config lint ./examples/cpe.yaml
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

## Testing guidelines

- Use go test ./...; write table-driven unit tests
- Preference: Use table-driven tests
    - Share common setup/validation logic through helper functions or validation callbacks
    - Name test cases descriptively in the `name` field
- Prefer httptest for HTTP; avoid real network calls
- Keep tests deterministic; use short timeouts; avoid sleeping where possible
- Isolate filesystem effects; clean up temp files; do not depend on developer-local state
- For dialog storage, prefer temp DB paths when adding tests
- Name tests with _test.go; keep per-package tests close to implementation

## Performance considerations

CPE is a CLI tool and MCP client where execution time is dominated by network calls to AI model APIs. Performance optimizations are typically not a concern unless specifically requested by the user. Focus on correctness, maintainability, and user experience over micro-optimizations.

## Documentation for Go Symbols

When gathering context about symbols like types, global variables, constants, functions and methods, prefer to use
`go doc` command. You may use
`go doc github.com/example/pkg.Type` to get documentation about a specific symbol. Avoid using
`go doc -all` as it may overwhelm your context window. Instead, if you need to perform a search or fuzzy search for a symbol, feed the output of
`go doc -all` into a cli like `rg`, `fzf`, etc.   