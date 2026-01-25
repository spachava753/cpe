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
  - `agent/`: Package that hosts generator adapters, streaming/printing, thinking filter, system prompt generation, and agent creation to execute a user query
  - `codemode/`: Package that hosts code mode implementation - schema to Go type conversion, tool collision detection, code execution sandbox, and execute_go_code tool
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

# Enable code mode (composes MCP tools as Go functions)
./cpe --config ./examples/cpe.yaml -m sonnet "Your prompt"  # If codeMode.enabled=true in config

# Disable code mode for a specific invocation (if enabled by default)
# Note: currently requires config change, no CLI flag exists
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

# Lint (via golangci-lint)
go run ./scripts lint

# Lint with auto-fix for formatting issues
go run ./scripts -lint-fix lint
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
- **Use exact matching for test assertions**: Always compare expected vs actual output exactly. Do not use `strings.Contains` or partial matching for output verification; use full expected strings in `want` fields
- Prefer httptest for HTTP; avoid real network calls
- Keep tests deterministic; use short timeouts; avoid sleeping where possible
- Isolate filesystem effects; clean up temp files; do not depend on developer-local state
- For dialog storage, prefer temp DB paths when adding tests
- Name tests with \_test.go; keep per-package tests close to implementation

## Performance considerations

CPE is a CLI tool and MCP client where execution time is dominated by network calls to AI model APIs. Performance optimizations are typically not a concern unless specifically requested by the user. Focus on correctness, maintainability, and user experience over micro-optimizations.

## Code Mode

Code mode allows LLMs to execute Go code to interact with MCP tools, providing:

- **Composability**: Chain multiple tool calls in a single execution
- **Control flow**: Use loops, conditionals, and error handling
- **Efficiency**: Reduce round-trips between LLM and tools
- **Standard library access**: File I/O, HTTP requests, data processing

Configuration:

```yaml
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - some_tool # Expose as regular tool instead
```

The LLM generates complete Go programs implementing a `Run(ctx context.Context) error` function. CPE compiles and executes them in a temporary sandbox with access to MCP tools as strongly-typed Go functions.

Implementation notes:

- Tool schemas are converted to Go structs using `internal/codemode/schema.go`
- Generated programs run with `go run` in `/tmp/cpe-tmp-*` directories
- Execution timeout enforced via SIGINT→SIGKILL with 5s grace period
- Exit codes: 0=success, 1=recoverable error, 2=panic (recoverable), 3=fatal error
- Non-streaming printer renders generated code as syntax-highlighted Go blocks

## MCP Server Mode

MCP Server Mode exposes CPE as an MCP server, enabling subagent composition within other MCP clients. Each subagent is defined in its own config file and exposed as a single tool.

**Running a subagent server:**

```bash
./cpe mcp serve --config ./subagent.cpe.yaml
```

**Subagent configuration:**

```yaml
version: "1.0"

models:
  - ref: opus
    id: claude-opus-4-20250514
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY

subagent:
  name: task_name # Tool name exposed to parent
  description: "..." # Tool description
  outputSchemaPath: ./out.json # Optional structured output

defaults:
  model: opus
  systemPromptPath: ./prompt.md
  codeMode:
    enabled: true # Subagent can use code mode
```

**Using from parent config:**

```yaml
mcpServers:
  my_subagent:
    command: cpe
    args: ["mcp", "serve", "--config", "./subagent.cpe.yaml"]
    type: stdio
```

Implementation notes:

- Server uses stdio transport only; stdout is reserved for MCP protocol
- Subagent inherits CWD and environment from server process
- If `outputSchemaPath` is set, a `final_answer` tool extracts structured output
- Execution traces are saved to `.cpeconvo` with `subagent:<name>:<run_id>` labels
- No retries on failure; errors propagate directly to caller
- Key files: `cmd/mcp.go`, `internal/mcp/server.go`, `internal/commands/subagent.go`

## Subagent Logging

When a subagent runs, events stream to the root CPE process for real-time visibility. The root process starts a localhost HTTP server and injects `CPE_SUBAGENT_LOGGING_ADDRESS` into child environments. Subagents POST events to this address, which are printed to stderr with name-prefixed headers:

- Tool calls: `#### <subagentName> [tool call] (timeout: Xs)`
- Tool results: `#### <subagentName> Tool "X" result:`
- Code execution: `#### <subagentName> Code execution output:`
- Thought traces: `#### <subagentName> thought trace`

Events are printed to **stderr** to avoid corrupting MCP protocol on stdout. If event emission fails (connection refused, non-2xx, timeout), the subagent aborts immediately—observability is considered essential.

See `docs/specs/subagent_logging.md` for full specification.

## Documentation for Go Symbols

When gathering context about symbols like types, global variables, constants, functions and methods, prefer to use
`go doc` command. You may use
`go doc github.com/example/pkg.Type` to get documentation about a specific symbol. Avoid using
`go doc -all` as it may overwhelm your context window. Instead, if you need to perform a search or fuzzy search for a symbol, feed the output of
`go doc -all` into a cli like `rg`, `fzf`, etc.

## Harbor Integration

CPE can be evaluated using the [Harbor](https://github.com/laude-institute/harbor) agent evaluation framework. The integration files are in `cpe_harbor/`:

- `cpe.py` - Installed agent class extending `BaseInstalledAgent`
- `install-cpe.sh.j2` - Jinja2 template for container setup (installs Go, CPE, config)

**Testing locally:**

```bash
# Harbor venv on this machine: /home/shashank/.harbor_venv
harbor run -d "hello-world@head" -e docker --agent-import-path cpe_harbor.cpe:CPE -n 1
```

**Notes:**

- The system prompt is fetched via curl from GitHub to avoid Jinja2/Go template syntax conflicts
- CPE runs with `-n -G --skip-stdin` flags (new conversation, incognito, no stdin)
- API keys are passed from host environment based on model provider


## Scripts

The `scripts/` folder contains development utility scripts managed via [Goyek](https://github.com/goyek/goyek), a Go-based task runner. Tasks are defined as Go functions and invoked with flags.

**Available tasks:**

- `lint` - Run golangci-lint with bug-focused linters (staticcheck, govet, bodyclose, nilerr, contextcheck, etc.)
- `debug-proxy` - HTTP reverse proxy that logs all requests/responses (useful for debugging API calls)
- `mcp-debug-proxy` - Stdio proxy that logs MCP protocol messages to a file

**Usage:**

```bash
# Lint the codebase
go run ./scripts lint

# Lint with auto-fix for formatting issues
go run ./scripts -lint-fix lint

# HTTP debug proxy
go run ./scripts -target=https://api.anthropic.com -port=8080 debug-proxy

# MCP debug proxy
go run ./scripts -log=debug.log -cmd='go run main.go mcp serve' mcp-debug-proxy
```

**Adding new tasks:**

1. Create a new `*_task.go` file in `scripts/`
2. Define flags in `main.go` if the task needs arguments
3. Use `goyek.Define(goyek.Task{...})` to register the task
