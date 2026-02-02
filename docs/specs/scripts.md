# CPE Scripts Specification

This document outlines the design and usage of the `scripts/` folder in CPE, which provides development utility tasks managed via the Goyek task runner framework.

## Overview

The `scripts/` folder contains Go-based development automation tasks that replace traditional shell scripts or Makefiles. Tasks are defined as Go functions and invoked through a unified CLI interface.

### Why Goyek?

CPE uses [Goyek](https://github.com/goyek/goyek) (v2.3.0) as its task runner for several reasons:

1. **Go-native**: Tasks are written in Go, allowing type-safety and access to Go's ecosystem
2. **Dependency management**: Tasks can depend on other tasks with automatic execution ordering
3. **Flag integration**: Seamless integration with Go's `flag` package for task arguments
4. **Cross-platform**: Works consistently across macOS, Linux, and Windows
5. **Self-documenting**: Task usage strings provide built-in help

### Invocation Pattern

All tasks are invoked via `go run ./scripts`:

```bash
go run ./scripts [flags] <task-name>
```

The `go run` command compiles and runs the scripts package, which uses Goyek's `Main()` function to dispatch to the requested task.

## Goyek Framework

### Task Definition

Tasks are defined using `goyek.Define()`, which registers a `goyek.Task` struct with the default flow. Each task requires:

- **Name**: The CLI identifier used to invoke the task
- **Usage**: A description shown in help output
- **Action**: The function that executes when the task runs

```go
var MyTask = goyek.Define(goyek.Task{
    Name:  "my-task",
    Usage: "Description of what my task does",
    Action: func(a *goyek.A) {
        // Task implementation
    },
})
```

### Task Dependencies

Tasks can declare dependencies via the `Deps` field, ensuring prerequisite tasks run first:

```go
var Deploy = goyek.Define(goyek.Task{
    Name:  "deploy",
    Usage: "Deploy the application",
    Deps:  goyek.Deps{Build, Test},  // Build and Test run before Deploy
    Action: func(a *goyek.A) {
        // Deploy logic
    },
})
```

By default, dependencies run sequentially. Setting `Parallel: true` allows a task to run concurrently with other parallel tasks.

### Flag Handling

Flags are defined in `main.go` at the package level using Go's `flag` package. Getter functions expose flag values to task files:

```go
// In main.go
var targetURL = flag.String("target", "", "Target URL to proxy")

func GetTargetURL() string { return *targetURL }

func main() {
    flag.Parse()
    goyek.Main(flag.Args())
}
```

Tasks access flag values via the getter functions:

```go
// In some_task.go
var SomeTask = goyek.Define(goyek.Task{
    Name: "some-task",
    Action: func(a *goyek.A) {
        url := GetTargetURL()  // Access flag value
        // ...
    },
})
```

Flags must appear **before** the task name on the command line:

```bash
go run ./scripts -target=https://example.com some-task  # ✓ Correct
go run ./scripts some-task -target=https://example.com  # ✗ Wrong
```

### Error Handling

Goyek's `*goyek.A` parameter provides methods for reporting errors:

| Method | Behavior |
|--------|----------|
| `a.Log(args...)` | Print message (like `fmt.Print`) |
| `a.Logf(format, args...)` | Print formatted message |
| `a.Error(args...)` | Mark task as failed, continue execution |
| `a.Errorf(format, args...)` | Mark task as failed with formatted message |
| `a.Fatal(args...)` | Mark task as failed, stop immediately |
| `a.Fatalf(format, args...)` | Fatal with formatted message |

## Available Tasks

### lint

Run golangci-lint on the codebase with configurable options.

**File:** `lint_task.go`

**Flags:**
- `-lint-fix`: Auto-fix linting issues where possible
- `-lint-verbose`: Enable verbose output

**Usage:**

```bash
# Basic lint check
go run ./scripts lint

# Lint with auto-fix
go run ./scripts -lint-fix lint

# Lint with verbose output
go run ./scripts -lint-verbose lint

# Lint with both options
go run ./scripts -lint-fix -lint-verbose lint
```

**Implementation:**

The task executes `go tool golangci-lint run ./...` as a subprocess. Exit code 1 from golangci-lint indicates linting issues were found, which is reported via `a.Errorf()`. Other errors (e.g., golangci-lint not installed) are fatal.

**Linters enabled:** Configured via `.golangci.yml` in the repository root. Focuses on bug detection with linters like staticcheck, govet, bodyclose, nilerr, and contextcheck.

---

### debug-proxy

HTTP reverse proxy that logs all requests and responses with colored output. Useful for debugging API calls to external services like AI model providers.

**File:** `debug_proxy_task.go`

**Flags:**
- `-target` (required): Target URL to proxy requests to
- `-port` (default: `8080`): Local port to listen on

**Usage:**

```bash
# Proxy requests to Anthropic API
go run ./scripts -target=https://api.anthropic.com debug-proxy

# Use custom port
go run ./scripts -target=https://api.anthropic.com -port=9090 debug-proxy

# Proxy to OpenAI
go run ./scripts -target=https://api.openai.com/v1 -port=8080 debug-proxy
```

**Implementation:**

Uses `net/http/httputil.ReverseProxy` with custom `Director` and `ModifyResponse` functions. Features:

1. **Request logging**: Prints method, path, headers (with API keys truncated), and body
2. **Response logging**: Prints status, headers, and body
3. **Gzip handling**: Automatically decompresses gzipped responses for logging
4. **JSON formatting**: Pretty-prints JSON bodies with 2000-character truncation
5. **Color output**: Uses ANSI escape codes for visual distinction:
   - Cyan: Request timestamp and dividers
   - Green: Response header
   - Yellow: Header/body sections

**Typical use case:**

1. Start the proxy: `go run ./scripts -target=https://api.anthropic.com debug-proxy`
2. Configure CPE to use `http://localhost:8080` as the base URL
3. Run CPE commands and observe the full request/response flow

---

### mcp-debug-proxy

Stdio proxy that sits between an MCP client and server, logging all JSON-RPC messages to a file. Essential for debugging MCP protocol issues.

**File:** `mcp_debug_proxy_task.go`

**Flags:**
- `-log` (required): Path to the log file
- `-cmd` (required): MCP server command to spawn

**Usage:**

```bash
# Debug a CPE subagent server
go run ./scripts -log=debug.log -cmd='./cpe mcp serve --config ./subagent.cpe.yaml' mcp-debug-proxy

# Debug an external MCP server
go run ./scripts -log=/tmp/mcp.log -cmd='npx @modelcontextprotocol/server-filesystem /' mcp-debug-proxy
```

**Implementation:**

The proxy:

1. Spawns the MCP server command as a child process
2. Intercepts stdin from the parent (MCP client) and forwards to the child
3. Intercepts stdout from the child and forwards to the parent
4. Logs all messages bidirectionally with timestamps:
   - `-->`: Messages from client to server
   - `<--`: Messages from server to client
5. Handles signal propagation (SIGINT/SIGTERM) to the child process
6. Passes stderr through directly (not logged)

**Log format:**

```
=== MCP Debug Proxy Started at 2026-01-25T10:30:00Z ===
Command: ./cpe mcp serve --config ./subagent.cpe.yaml
=========================================

[10:30:01.123] --> {"jsonrpc":"2.0","method":"initialize","id":1,...}
[10:30:01.456] <-- {"jsonrpc":"2.0","result":{...},"id":1}
[10:30:01.789] --> {"jsonrpc":"2.0","method":"tools/list","id":2}
[10:30:02.012] <-- {"jsonrpc":"2.0","result":{"tools":[...]},"id":2}
```

**Typical use case:**

Configure an MCP client to spawn the proxy instead of the server directly:

```yaml
mcpServers:
  my_server:
    command: go
    args: ["run", "./scripts", "-log=debug.log", "-cmd=./actual-server", "mcp-debug-proxy"]
    type: stdio
```

---

### gen-schema

Generate the JSON Schema for CPE configuration files.

**File:** `gen_schema_task.go`

**Usage:**

```bash
go run ./scripts gen-schema
```

**Implementation:**

Uses the `github.com/invopop/jsonschema` package to reflect on `config.RawConfig` and generate a JSON Schema. The schema:

- Is written to `schema/cpe-config-schema.json`
- Has `additionalProperties: false` to catch typos
- Uses `jsonschema` struct tags for required field detection
- Includes title and description metadata

The task automatically finds the module root by:
1. Checking the `GOMOD` environment variable (set by `go run`)
2. Falling back to traversing up from the current directory to find `go.mod`

## Directory Structure

```
scripts/
├── main.go                  # Entry point, flag definitions, Goyek setup
├── lint_task.go             # Lint task definition
├── debug_proxy_task.go      # HTTP debug proxy task
├── mcp_debug_proxy_task.go  # MCP stdio debug proxy task
└── gen_schema_task.go       # JSON schema generation task
```

## Adding New Tasks

### Step 1: Create the Task File

Create a new file named `<name>_task.go` in the `scripts/` directory:

```go
package main

import (
    "os"
    "os/exec"

    "github.com/goyek/goyek/v2"
)

// MyTask does something useful
var MyTask = goyek.Define(goyek.Task{
    Name:  "my-task",
    Usage: "Short description. Use -my-flag for optional behavior",
    Action: func(a *goyek.A) {
        // Implementation here
        
        // Access flags via getter functions
        if GetMyFlag() {
            // ...
        }
        
        // Report errors
        if err != nil {
            a.Fatalf("Failed: %v", err)
        }
    },
})
```

### Step 2: Add Flags (if needed)

If your task needs arguments, add flags to `main.go`:

```go
// In main.go

// Flags for my-task
var (
    myFlag = flag.Bool("my-flag", false, "Enable my feature")
)

// Add getter function
func GetMyFlag() bool { return *myFlag }
```

### Step 3: Test the Task

```bash
# Verify it appears in the task list
go run ./scripts

# Run the task
go run ./scripts my-task

# Run with flags
go run ./scripts -my-flag my-task
```

### Naming Conventions

- **File names**: `<task_name>_task.go` using snake_case
- **Task names**: Use kebab-case (e.g., `debug-proxy`, `gen-schema`)
- **Variable names**: PascalCase for the exported task variable (e.g., `DebugProxy`, `GenSchema`)
- **Flag names**: Prefix with task name for task-specific flags (e.g., `-lint-fix`, `-lint-verbose`)

### Best Practices

1. **Keep tasks focused**: Each task should do one thing well
2. **Use `os/exec` for external commands**: Allows proper error handling and output capture
3. **Write clear usage strings**: Include flag descriptions and example invocations
4. **Handle signals gracefully**: For long-running tasks, handle SIGINT/SIGTERM
5. **Use `a.Fatalf()` for unrecoverable errors**: Ensures proper exit codes
6. **Log progress for long operations**: Users should know what's happening

## Usage Examples

### Daily Development Workflow

```bash
# Before committing: lint the codebase
go run ./scripts lint

# Fix auto-fixable issues
go run ./scripts -lint-fix lint
```

### Debugging API Issues

```bash
# Terminal 1: Start the debug proxy
go run ./scripts -target=https://api.anthropic.com debug-proxy

# Terminal 2: Run CPE with proxy
./cpe --config ./cpe.yaml -m sonnet "Hello"
# Or set base_url in config to http://localhost:8080
```

### Debugging MCP Server Issues

```bash
# Option 1: Direct proxy invocation
go run ./scripts -log=/tmp/mcp-debug.log \
    -cmd='./cpe mcp serve --config ./agent.cpe.yaml' \
    mcp-debug-proxy

# Option 2: Configure in parent's MCP servers config
# Then check /tmp/mcp-debug.log for protocol messages
```

### Regenerating Configuration Schema

```bash
# After modifying config types
go run ./scripts gen-schema

# Verify the output
cat schema/cpe-config-schema.json | jq .
```

### List All Available Tasks

```bash
go run ./scripts
```

Output:

```
Tasks:
  debug-proxy      HTTP debug proxy. Use -target=URL [-port=8080]
  gen-schema       Generate JSON schema for CPE configuration files
  lint             Run golangci-lint. Use -lint-fix to auto-fix, -lint-verbose for details
  mcp-debug-proxy  MCP stdio debug proxy. Use -log=FILE -cmd='command args'
```
