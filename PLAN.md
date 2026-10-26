# Tool runtime refactor

This document is a version controlled planning document for refactoring how the tool runtime works. I want to refactor the tool runtime that powers tool calling in the agent loop.

## Behavior

The below section describe different behaviors that we desire the tool runtime to have.

### Available tools

The tool runtime will expose exactly 2 tools to the model for it to invoke. The 2 tools are `bash` and `starlark_exec`.

The `bash` tool will be a simple tool to execute whatever bash script the model generates and return the result of stdout and stderr as text tool result. The description will be:
```text
Execute generated bash script in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, the full output is saved to a temporary file, and filepath will be provided in the tool result. Optionally provide a timeout in seconds.
```
The tool input schema is:
```json
{
  "parameters": {
    "type": "object",
    "properties": {
      "script": {
        "type": "string",
        "description": "Bash script to execute"
      },
      "timeout": {
        "type": "number",
        "description": "Timeout in seconds (optional, no default timeout)"
      }
    },
    "required": ["command"]
  }
}
```

The `starlark_exec` tool will be utilized to edit files, orchestrate tool calls, and view binary files like images. The tool will execute model generated [StarlarkX](https://github.com/spachava753/starlarkx) code, which is langauge that has Python-like syntax, though there are minor syntax differences. The biggest difference is that there is no classes system, no reflection, and no standard library. However, the syntax and support for datatypes, functions and variables is the same as Python. In many cases, StarlarkX code will just look like Python code.
The description will be:
```text
Execute StarlarkX code in the current working directory. Returns the text printed in the script. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, the full output is saved to a temporary file, and filepath will be provided in the tool result. Optionally provide a timeout in seconds.
```
The tool input schema is:
```json
{
  "parameters": {
    "type": "object",
    "properties": {
      "code": {
        "type": "string",
        "description": "StarlarkX code to execute"
      },
      "timeout": {
        "type": "number",
        "description": "Timeout in seconds (optional, no default timeout)"
      }
    },
    "required": ["command"]
  }
}
```
The `starlark_exec` tool is not a REPL, each tool call will launch a new StarlarkX thread to execute. StarlarkX threads are extremely cheap to start and execute so we can create new ones for each tool call. The StarlarkX runtime will have MCP tools exposed as builtin functions that the model can generate code to orchestrate.

### Loading MCP server tools

MCP tools will be availabe to the runtime from 3 different sources:
1. Configured MCP servers via settings.json
2. Plugin defined MCP servers
3. ACP session provided MCP servers

The user will configure MCP servers in ~/.cpe/settings.json. Configured MCP servers can look like this:
```jsonc
{
  ... other settings
  "mcpServers": {
    "mcp-stdio": {
      "command": "mcp-command",
      "enabled": false
    },
    "mcp-http": {
      "url": "https://example.com/mcp-oauth",
      "description": "Example mcp server config"
    }
  }
}
```

Installed [Agent Plugins](https://agent-plugins.org/specification) may also define MCP servers in its plugin manifest, and we load MCP tools from the MCP servers defined in the plugin manifest.

Finally, when initializing an ACP session, the ACP client may provide an MCP servers configuration that we should use to connect to and load tools.

We aim to support MCP servers from protocol revision 2025-06-18 and onward[^1].

MCP tools will be exposed as `tools.mcp_server_name__tool_name(input) -> ToolCallOutput`. The names of the builtin functions will be constructed via the MCP server name and tool name. However, since function names have naming limitations


### Orchestrating MCP tools

TODO

### Builtin Starlarkx Tools

In addition to having tools exposed as functions, the runtime will also have the following builtin functions available:
- `image(file: bytes)`: 
- `read_file(path: str) -> bytes`:
- `edit_file(path: str, old: str, new: str) -> str`:



### System instruction construction

TODO

### Overflow tool result

TODO

## Implementation details

TODO

### Types and Functions

TODO

### Connection re-use

TODO

### MCP name conflict

TODO

### Overflow tool wrapper

TODO

[^1]: Does the latest revision support descriptions?
