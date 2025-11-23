# CPE Code Mode Specification

This document outlines the design for "Code Mode" in CPE, a feature that allows LLMs to execute Typescript code via Deno to interact with MCP tools in a composable and type-safe manner.

## Overview

Currently, LLMs interact with tools via discrete tool calls. This has limitations:
1.  **Context Usage**: Tool schemas and descriptions consume significant context.
2.  **Composability**: Chaining tools requires multiple round-trips (LLM -> Tool -> LLM -> Tool), increasing latency and cost.
3.  **Expressiveness**: LLMs are restricted to the tool's specific API without logic or control flow.

"Code Mode" solves this by exposing MCP tools as strongly-typed Typescript functions within a Deno runtime environment. The LLM generates Typescript code that calls these functions, allowing it to perform complex logic, data processing, and composed tool executions in a single turn.

## Architecture

### 1. The "Execute Typescript" Tool

When Code Mode is enabled, CPE hides the individual MCP tools from the LLM. Instead, it exposes a single tool:

**Name**: `execute_typescript`
**Description**: 
> Execute Typescript code in a Deno runtime. 
> You have access to a set of pre-defined functions that map to the configured tools. 
> Each function returns a `Result<T>` type.
> The code is executed as a standalone script (not a REPL).
> Use `console.log` to print output. 
> The final result of the tool call will be the standard output of the script.

**Input Schema**:
```json
{
  "type": "object",
  "properties": {
    "code": {
      "type": "string",
      "description": "The complete Typescript code to execute."
    }
  },
  "required": ["code"]
}
```

### 2. Function Generation & Type Mapping

At runtime (during `CreateToolCapableGenerator` initialization), CPE introspects the configured MCP tools to generate Typescript definitions.

**Process**:
1.  **Fetch Tools**: Retrieve tool definitions from MCP servers. Modified `FetchTools` to include `InputSchema`, `OutputSchema`, `Name`, and `Description`.
2.  **Type Generation**: 
    *   Use `deno run npm:json-schema-to-typescript` to convert `InputSchema` and `OutputSchema` to Typescript interfaces.
    *   Flags: `--no-additionalProperties`, `--bannerComment ""`.
3.  **Function Wrapper**: Generate a Typescript function for each tool that wraps an HTTP call to the CPE Bridge Server.

**Type Definitions**:

```typescript
// Discriminated union for result handling
type Result<T> =
  | { success: true; value: T }
  | { success: false; error: string };

// Generic structures for the bridge
interface ToolCall<T> {
    tool_name: string;
    arguments: T;
}

interface ToolResult<T> {
    content: string | T;
    is_error: boolean;
}
```

**Generated Function Example**:

```typescript
// Generated Interface from InputSchema
export interface GetWeatherDataInput {
    location: string;
}

// Generated Interface from OutputSchema
// If OutputSchema is missing, this defaults to Record<string, unknown>
export interface GetWeatherDataOutput {
    temperature: number;
    conditions: string;
    humidity: number;
}

/**
 * Get current weather data for a location
 * @param input - The input arguments
 */
async function get_weather_data(input: GetWeatherDataInput): Promise<Result<GetWeatherDataOutput>> {
    const reqBody: ToolCall<GetWeatherDataInput> = {
        tool_name: "get_weather_data",
        arguments: input
    };
    
    // PORT is injected at runtime
    const response = await fetch(`http://localhost:${SERVER_PORT}/`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(reqBody),
    });

    const result: ToolResult<GetWeatherDataOutput> = await response.json();

    if (!response.ok || result.is_error) {
        return {
            success: false,
            // If content is an object/json during error, stringify it, otherwise use as string
            error: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
        };
    }

    return {
        success: true,
        // The bridge ensures content matches T or is a JSON-parsed equivalent
        value: result.content as GetWeatherDataOutput,
    };
}
```

### 3. CPE Bridge Server

To bridge the Deno subprocess and the Go MCP client, CPE spins up a temporary HTTP server.

*   **Lifecycle**: Started inside `CreateToolCapableGenerator`. Stops when the generator's context is cancelled.
*   **Address**: Localhost with a random ephemeral port.
*   **Endpoint**: `POST /`
*   **Request**: `{ "tool_name": "...", "arguments": { ... } }`
*   **Logic**:
    1.  Receives request from Deno.
    2.  Lookups the MCP tool by name.
    3.  Executes the tool using the existing MCP client.
    4.  **Content Handling**:
        *   If MCP result has `structured_content`, use it.
        *   Else if MCP result `text` can be parsed as JSON, parse it and use the object.
        *   Else, use the `text` string directly.
    5.  Returns response: `{ "content": (object or string), "is_error": boolean }`

### 4. Deno Execution Environment

*   **Command**: `deno run --allow-all --no-prompt <temp_file.ts>`
*   **Temp File**: Created via `os.CreateTemp`. Contains the preamble (types + generated functions + port constant) followed by the LLM's generated code.
*   **Output**: Capture `stdout` as the tool result. Capture `stderr` for error reporting.
*   **Error Handling**: 
    *   If Deno exits with non-zero status (compilation error or runtime panic), return `stderr` as the tool result.
    *   This allows the LLM to read the error and attempt to fix the code in the next turn.

## Configuration

"Code Mode" is controlled via the configuration file.

```yaml
defaults:
  codeMode: true # Global default

models:
  - ref: sonnet
    codeMode: true # Model-specific override
  - ref: small-model
    codeMode: false
```

## Implementation Steps

1.  **Update `FetchTools`**: Modify `internal/mcp/client.go` to return `OutputSchema` alongside existing data.
2.  **Implement Bridge Server**: Create the HTTP server in `internal/agent` that handles the `POST /` requests and proxies to `gai.ToolCallback`.
3.  **Code Generator**: Implement the logic to:
    *   Run `json-schema-to-typescript` via `deno`.
    *   Assemble the Typescript preamble (interfaces + functions).
4.  **Update Generator**: Modify `CreateToolCapableGenerator` in `internal/agent/generator.go`:
    *   Check `codeMode` config.
    *   If enabled: 
        *   Start Bridge Server.
        *   Generate Typescript definitions.
        *   Register ONLY the `execute_typescript` tool.
    *   If disabled: Register standard MCP tools (existing behavior).
5.  **Implement `execute_typescript`**: The callback for this tool writes the code to a temp file, runs Deno, and returns output/errors.
