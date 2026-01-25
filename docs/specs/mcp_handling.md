# CPE MCP Handling Specification

This document describes how CPE integrates with Model Context Protocol (MCP) servers as a client, covering configuration, connection management, tool discovery, and tool execution.

## Overview

CPE acts as an MCP client that connects to one or more MCP servers to access their tools. These tools extend the capabilities available to the LLM, enabling interactions with filesystems, databases, APIs, and other external systems.

The MCP integration in CPE supports:
- Multiple concurrent server connections
- Three transport types: stdio, HTTP (streamable), and SSE
- Tool filtering per server (whitelist or blacklist mode)
- Code mode integration for composable tool execution
- Subagent event forwarding for observability

## Configuration

MCP servers are configured in the `mcpServers` section of the CPE configuration file. Each server is identified by a unique name and has its own connection and filtering settings.

### Basic Configuration Structure

```yaml
version: "1.0"

mcpServers:
  filesystem:
    command: filesystem-mcp
    args: ["--root", "/home/user"]
    type: stdio
    env:
      DEBUG: "true"
    timeout: 60
    
  web-api:
    url: https://api.example.com/mcp
    type: http
    headers:
      Authorization: "Bearer ${API_TOKEN}"
    timeout: 30
    
  search:
    url: https://search.example.com/sse
    type: sse
    enabledTools:
      - web_search
      - image_search
```

### ServerConfig Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Transport type: `stdio`, `http`, or `sse` |
| `command` | string | Yes (stdio) | Executable command for stdio servers |
| `args` | []string | No | Arguments to pass to the command |
| `url` | string | Yes (http/sse) | Endpoint URL for network-based servers |
| `env` | map[string]string | No (stdio only) | Additional environment variables |
| `headers` | map[string]string | No (http/sse only) | Custom HTTP headers |
| `timeout` | int | No | Connection timeout in seconds (default: 60) |
| `enabledTools` | []string | No | Whitelist of tool names to enable |
| `disabledTools` | []string | No | Blacklist of tool names to disable |

### Transport Types

#### stdio (Standard I/O)

The most common transport for locally-installed MCP servers. CPE spawns the server as a child process and communicates via stdin/stdout.

```yaml
mcpServers:
  shell:
    command: shell-mcp
    args: ["--allow-execute"]
    type: stdio
    env:
      SHELL: /bin/bash
```

Key behaviors:
- Command is executed via `exec.Command`
- Child process inherits parent environment (`os.Environ()`)
- Custom `env` values are appended to inherited environment
- stderr from child is forwarded to CPE's stderr
- `CPE_SUBAGENT_LOGGING_ADDRESS` is injected for subagent event forwarding

#### http (Streamable HTTP)

For HTTP-based MCP servers supporting the streamable transport protocol.

```yaml
mcpServers:
  remote-api:
    url: https://mcp.example.com/stream
    type: http
    headers:
      Authorization: "Bearer token"
```

#### sse (Server-Sent Events)

For MCP servers using the SSE transport protocol.

```yaml
mcpServers:
  realtime:
    url: https://mcp.example.com/sse
    type: sse
```

### Tool Filtering

Each server can optionally filter which tools are exposed to the LLM. This is useful for:
- Restricting access to sensitive operations
- Reducing context window usage
- Focusing the LLM on relevant tools

#### Whitelist Mode (enabledTools)

Only specified tools are available; all others are hidden.

```yaml
mcpServers:
  filesystem:
    command: filesystem-mcp
    type: stdio
    enabledTools:
      - read_file
      - write_file
      # list_directory is hidden
```

#### Blacklist Mode (disabledTools)

All tools are available except those specified.

```yaml
mcpServers:
  shell:
    command: shell-mcp
    type: stdio
    disabledTools:
      - execute_command  # Too dangerous
      # read_environment, get_cwd, etc. are available
```

**Note:** `enabledTools` and `disabledTools` are mutually exclusive. If neither is specified, all tools from the server are available.

## Connection Lifecycle

### Server Startup and Connection

1. **Transport Creation**: CPE creates the appropriate transport based on the server's `type`:
   - `stdio`: Creates `mcp.CommandTransport` with the configured command
   - `http`: Creates `mcp.StreamableClientTransport` with the URL
   - `sse`: Creates `mcp.SSEClientTransport` with the URL

2. **Client Connection**: The MCP client connects to the transport and establishes a session:
   ```go
   client := mcp.NewClient(
       &mcp.Implementation{
           Name:    "cpe",
           Title:   "CPE",
           Version: version.Get(),
       }, nil,
   )
   session, err := client.Connect(ctx, transport, nil)
   ```

3. **Tool Discovery**: After connection, CPE iterates over available tools using `session.Tools()`

### Session Management

- Sessions are maintained for the lifetime of a CPE invocation
- Each tool callback holds a reference to its session for making tool calls
- Sessions are closed gracefully when CPE exits

### Code Mode Session Behavior

When code mode is enabled, tool calls occur in a separate subprocess. Each `execute_go_code` invocation:
1. Creates fresh MCP client connections in the generated Go program
2. Connects to each server needed for the tools being called
3. Closes all sessions when execution completes

This "reconnect per execution" model:
- Ensures clean state for each code execution
- Works reliably with stateless MCP servers
- May introduce latency for servers with slow startup (typically milliseconds for stdio)

### Graceful Shutdown

On context cancellation or SIGINT/SIGTERM:
1. Active tool calls are cancelled via context
2. MCP client sessions are closed (`session.Close()`)
3. For stdio servers, the child process receives the signal

## Tool Discovery

### Discovery Process

The `FetchTools` function orchestrates tool discovery across all configured servers:

1. **Iterate Servers**: Process each server in `mcpServers` configuration
2. **Connect**: Establish connection using `CreateTransport` and `client.Connect`
3. **List Tools**: Call `session.Tools()` to get all available tools
4. **Apply Filtering**: Filter tools based on `enabledTools` or `disabledTools`
5. **Detect Duplicates**: Warn if the same tool name appears in multiple servers
6. **Create Callbacks**: Wrap each tool with a `ToolCallback` that can invoke it

### Tool Schema Handling

MCP tools have `InputSchema` and optionally `OutputSchema` in JSON Schema format. CPE handles these as follows:

**For Normal Tool Registration:**
- `InputSchema` is converted from `map[string]interface{}` to `*jsonschema.Schema`
- The schema is passed to the generator for LLM tool definitions

**For Code Mode:**
- Schemas are converted to Go type definitions (see `internal/codemode/schema.go`)
- Input schemas become `*Input` structs
- Output schemas become `*Output` structs (or `string` if no output schema)

### Duplicate Tool Detection

If the same tool name appears in multiple servers, CPE:
1. Registers only the first occurrence
2. Logs a warning with both server names
3. Continues processing remaining tools

```
WARN mcp tool registration warning warning="skipping duplicate tool name 'read_file' in server 'server2' (already registered from server 'server1')"
```

### Tool Registration with Generator

Tools are registered with the generator via `ToolResultPrinterWrapper.Register()`:

```go
for _, toolData := range tools {
    err := toolResultPrinter.Register(toolData.Tool, toolData.ToolCallback)
}
```

Each registration includes:
- `gai.Tool`: Name, description, and input schema for LLM
- `gai.ToolCallback`: Implementation that invokes the MCP tool

## Tool Execution

### Normal Tool Calling Flow

1. **LLM Request**: Generator receives tool call with name and JSON parameters
2. **Callback Lookup**: `ToolGenerator` finds the registered callback by tool name
3. **Execute Callback**: `ToolCallback.Call()` is invoked with parameters and tool call ID
4. **MCP Call**: Callback calls `session.CallTool()` with parsed parameters
5. **Result Conversion**: MCP result is converted to `gai.Message` with appropriate blocks
6. **Return to LLM**: Tool result message is added to dialog

### ToolCallback Implementation

```go
func (c *ToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
    // Parse parameters
    var params map[string]any
    json.Unmarshal(parametersJSON, &params)

    // Call MCP tool
    result, err := c.ClientSession.CallTool(ctx, &mcp.CallToolParams{
        Name:      c.ToolName,
        Arguments: params,
    })

    // Convert result to gai.Message blocks
    // Handles TextContent, ImageContent, etc.
    return gai.Message{Role: gai.ToolResult, Blocks: blocks}, nil
}
```

### Code Mode Integration

When code mode is enabled, tools are partitioned into two categories:

**Code Mode Tools**: Called via generated Go functions in `execute_go_code`
- Compiled into type-safe function signatures
- Enable loops, conditionals, and composition
- Create fresh MCP connections per execution

**Excluded Tools**: Registered as normal MCP tools
- Configured via `codeMode.excludedTools`
- Useful for stateful servers or multimedia responses
- Called through standard tool calling flow

```yaml
defaults:
  codeMode:
    enabled: true
    excludedTools:
      - image_generator  # Returns images, needs normal tool call
      - database_session # Stateful, needs persistent connection
```

### Error Handling

**Connection Errors:**
- Transport creation failures return immediately with descriptive errors
- Connection failures return immediately with errors
- Tool listing failures within a connected session are logged as warnings
- If all servers fail to register tools, an error is returned

**Tool Call Errors:**
- Parse errors for invalid JSON parameters
- MCP protocol errors from `CallTool`
- `IsError` flag in MCP result indicates tool-level errors

Errors are formatted as tool result messages so the LLM can adapt:

```go
return gai.Message{
    Role: gai.ToolResult,
    Blocks: []gai.Block{{
        Content: gai.Str(fmt.Sprintf("Error calling MCP tool %s/%s: %v", 
            serverName, toolName, err)),
    }},
}, nil
```

## Architecture

### Key Files

| File | Purpose |
|------|---------|
| `internal/mcp/client.go` | MCP client, transport creation, tool fetching, callbacks, `ServerConfig` type definition |
| `internal/mcp/server.go` | MCP server mode (subagent exposure) |
| `internal/config/config.go` | Configuration loading, imports `ServerConfig` from mcp package |
| `internal/agent/generator.go` | Generator creation, tool registration orchestration |
| `internal/agent/tool_result_printer.go` | `ToolResultPrinterWrapper` for tool registration and result printing |
| `internal/commands/mcp.go` | CLI command implementations (list-servers, list-tools, call-tool) |
| `cmd/mcp.go` | Cobra command definitions for MCP subcommands |
| `internal/codemode/partition.go` | Code mode tool partitioning |
| `internal/codemode/maingen.go` | Generated main.go template with MCP client setup |

### Client Mode vs Server Mode

**Client Mode** (default):
- CPE connects to MCP servers as a client
- Servers are spawned as child processes (stdio) or connected via HTTP
- Tools from servers are exposed to the LLM

**Server Mode** (`cpe mcp serve`):
- CPE runs as an MCP server itself
- Exposes a configured subagent as a single tool
- Uses stdio transport for communication with parent
- See `docs/specs/mcp_server_mode.md` for details

### Transport Abstractions

The `CreateTransport` function creates the appropriate transport type:

```go
func CreateTransport(ctx context.Context, config ServerConfig, loggingAddress string) (mcp.Transport, error) {
    switch config.Type {
    case "stdio", "":
        cmd := exec.CommandContext(ctx, config.Command, config.Args...)
        cmd.Env = os.Environ()
        // Add config.Env and logging address
        return &mcp.CommandTransport{Command: cmd}
    case "http":
        return &mcp.StreamableClientTransport{
            Endpoint:   config.URL,
            HTTPClient: httpClient, // with custom headers
        }
    case "sse":
        return &mcp.SSEClientTransport{
            Endpoint:   config.URL,
            HTTPClient: httpClient,
        }
    }
}
```

### MCP SDK Types Used

| Type | Purpose |
|------|---------|
| `mcp.Client` | MCP protocol client |
| `mcp.ClientSession` | Active connection to a server |
| `mcp.Tool` | Tool definition with schemas |
| `mcp.CallToolParams` | Parameters for tool invocation |
| `mcp.TextContent` | Text content in results |
| `mcp.ImageContent` | Image content in results |
| `mcp.ResourceLink` | Resource link in results (returns error if encountered) |
| `mcp.Transport` | Transport interface returned by CreateTransport |
| `mcp.Implementation` | Client implementation info |
| `mcp.CommandTransport` | stdio transport |
| `mcp.StreamableClientTransport` | HTTP transport |
| `mcp.SSEClientTransport` | SSE transport |

## CLI Commands

CPE provides several commands for interacting with MCP servers:

### cpe mcp list-servers

Lists all configured MCP servers from the config file.

```bash
$ cpe mcp list-servers
Configured MCP Servers:
- filesystem (Type: stdio, Timeout: 60s)
  Command: filesystem-mcp --root /home/user
- web-api (Type: http, Timeout: 30s)
  URL: https://api.example.com/mcp
```

### cpe mcp info <server>

Connects to a server and displays its information.

```bash
$ cpe mcp info filesystem
Connected to server: filesystem
```

### cpe mcp list-tools <server>

Lists available tools on a server with their schemas. The output includes:
- Filter mode and statistics
- Tool names and descriptions
- Input schemas in JSON format
- Output schemas (if defined)

Options:
- `--show-all`: Show all tools including filtered ones
- `--show-filtered`: Show only filtered-out tools

### cpe mcp call-tool

Directly call a tool on a server (useful for testing).

```bash
$ cpe mcp call-tool --server filesystem --tool read_file --args '{"path": "README.md"}'
# File contents are printed to stdout
```

## Subagent Event Forwarding

When CPE spawns child MCP servers (stdio transport), it can forward subagent execution events to a central logging endpoint. This enables real-time observability of nested subagent activity.

### Mechanism

1. Parent CPE starts an HTTP server on localhost
2. `CPE_SUBAGENT_LOGGING_ADDRESS` environment variable is injected into child processes
3. Child subagents POST events to this endpoint
4. Parent prints events to stderr with subagent name prefixes

### Event Types

- Tool calls with timeout information
- Tool results
- Thought traces (thinking blocks)
- Subagent start/end lifecycle events

See `docs/specs/subagent_logging.md` for full specification.

## Implementation Notes

### Thread Safety

- `FetchTools` processes servers sequentially (not in parallel)
- Each `ToolCallback` holds its own session reference
- Concurrent tool calls to the same session are handled by the MCP SDK

### Environment Variable Handling

For stdio servers, environment variables are handled in this order:
1. Inherit all variables from `os.Environ()`
2. Append custom variables from `config.Env`
3. Append `CPE_SUBAGENT_LOGGING_ADDRESS` if set

### HTTP Client Customization

For http/sse servers with custom headers:
1. Create `headerRoundTripper` wrapping `http.DefaultTransport`
2. Set configured headers on each request
3. Pass custom `*http.Client` to transport

### Validation

Configuration is validated using struct tags on `ServerConfig`:

| Field | Validation Rules |
|-------|------------------|
| `type` | `required,oneof=stdio sse http` |
| `command` | `required_if=Type stdio` |
| `url` | `excluded_if=Type stdio,required_if=Type sse,required_if=Type http,omitempty,https_url\|http_url` |
| `timeout` | `gte=0` |
| `env` | `excluded_unless=Type stdio` |
| `headers` | `excluded_if=Type stdio` |
| `enabledTools` | `omitempty,min=1,excluded_with=DisabledTools` |
| `disabledTools` | `omitempty,min=1,excluded_with=EnabledTools` |
