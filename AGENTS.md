# AGENTS.md

## Project Overview

CPE (Chat-based Programming Editor) is a CLI that connects local developer workflows to multiple AI model providers. It analyzes, edits, and creates code via natural-language prompts, with optional MCP tool integration and persistent conversation storage. Link: https://github.com/spachava753/cpe

Key capabilities:

- Multi-model generation via unified YAML/JSON configuration
- Tool use (file ops, shell execution, MCP servers)
- Streaming or non-streaming output with “thinking” filtering
- Conversation persistence and branching

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
- main.go: invokes cmd.Execute()
- gen.go: code generation hooks (if any)
- .github/workflows/: CI (currently empty)
- examples/cpe.yaml: example unified configuration (YAML)

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

Use Conventional Commits format:

```text
type(scope)!: short summary

detailed breakdown (prefer full sentences/short paragraphs over lists)

paragraph 2

...

BREAKING CHANGE: footer describing breaking change if necessary  
```

- Common types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
- Imperative, present tense; no trailing period or whitespace; scope optional; add ! for breaking changes
- Describe what changed and why, not how. Avoid describing surface level code changes; can just view the code diff. Should instead detail the reason for this commit and feature wise what changed.
- Include body/footer when helpful; use BREAKING CHANGE: and issue refs (e.g., Closes #123)

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