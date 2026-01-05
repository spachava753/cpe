# Subagents

Subagents allow CPE to be exposed as an MCP server, enabling composition of AI agents as tools within other MCP-compliant environments.

## Overview

A **subagent** is a focused CPE instance that performs a specific task and returns results to a parent agent. By running CPE as an MCP server, you can:

- **Reduce context window pressure**: Offload complex tasks to dedicated subagents with their own context
- **Compose capabilities**: Chain multiple specialized agents together
- **Run in parallel**: Execute multiple subagents concurrently for faster results
- **Maintain focus**: Keep each agent scoped to a specific domain or task type

## Quick Start

### 1. Create a Subagent Config

```yaml
# my_subagent.cpe.yaml
version: "1.0"

models:
  - ref: sonnet
    display_name: "Claude Sonnet"
    id: claude-sonnet-4-20250514
    type: anthropic
    api_key_env: ANTHROPIC_API_KEY

subagent:
  name: my_task
  description: Performs a specific task and returns results.

defaults:
  model: sonnet
  systemPromptPath: ./my_prompt.prompt
```

### 2. Create a System Prompt

```text
# my_prompt.prompt
You are a specialized agent for [task description].

## Guidelines
- Focus on [specific behavior]
- Output [expected format]
```

### 3. Start the Server

```bash
cpe mcp serve --config ./my_subagent.cpe.yaml
```

### 4. Configure Parent to Use Subagent

```yaml
# parent.cpe.yaml
mcpServers:
  my_subagent:
    command: cpe
    args: ["mcp", "serve", "--config", "./my_subagent.cpe.yaml"]
    type: stdio
```

## Configuration Reference

### Subagent Definition

The `subagent` block defines the tool exposed via MCP:

```yaml
subagent:
  # Required: Tool name (used in MCP tool calls)
  name: review_changes
  
  # Required: Tool description (shown to parent agent)
  description: |
    Review code changes and return prioritized feedback.
    Accepts diffs or file content via 'prompt' or 'inputs'.
  
  # Optional: Path to JSON schema for structured output
  outputSchemaPath: ./output_schema.json
```

### Tool Input Schema

Every subagent tool accepts this input:

```json
{
  "prompt": "The task to execute (required)",
  "inputs": ["optional/file/paths.txt"]
}
```

- **prompt**: The task description or content to process
- **inputs**: Optional list of file paths to include as context (relative to server CWD)

### Defaults

The `defaults` block configures subagent behavior:

```yaml
defaults:
  # Required: Model reference (must match a model in 'models' list)
  model: sonnet
  
  # Optional: Path to system prompt template
  systemPromptPath: ./prompt.prompt
  
  # Optional: Code mode configuration
  codeMode:
    enabled: true
    maxTimeout: 300  # seconds
    excludedTools:
      - some_tool
  
  # Optional: Timeout for model requests
  timeout: "5m"
  
  # Optional: Disable streaming
  noStream: false
```

### MCP Servers

Subagents can use MCP servers for additional tools:

```yaml
mcpServers:
  filesystem:
    command: filesystem-mcp
    type: stdio
  
  web:
    url: https://search-mcp.example.com/mcp
    type: http
```

## Structured Output

For predictable output, define a JSON schema:

```json
// output_schema.json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["result", "confidence"],
  "properties": {
    "result": { "type": "string" },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 }
  }
}
```

Reference it in the subagent config:

```yaml
subagent:
  name: analyzer
  description: Analyze input and return structured result.
  outputSchemaPath: ./output_schema.json
```

When an output schema is configured, the subagent gets a `final_answer` tool. The model must call this tool with data matching the schema to complete execution.

**Tip**: Instruct the model in the system prompt to use `final_answer`:

```text
You MUST call the final_answer tool with your structured response.
Do not respond with plain text.
```

## Code Mode

Enable code mode for subagents that need to:
- Read/write files
- Execute shell commands
- Perform complex multi-step operations

```yaml
defaults:
  codeMode:
    enabled: true
    maxTimeout: 300  # Max execution time in seconds
```

Disable code mode for pure reasoning/analysis subagents:

```yaml
defaults:
  codeMode:
    enabled: false
```

## Use Cases

### Diff Reviewer

A focused subagent for code review:

```yaml
subagent:
  name: review_changes
  description: Review code diffs and return prioritized feedback.
  outputSchemaPath: ./review_schema.json

defaults:
  model: sonnet
  systemPromptPath: ./review.prompt
  codeMode:
    enabled: false
```

### Test Writer

A code mode subagent for writing tests:

```yaml
subagent:
  name: write_tests
  description: Write and run tests for specified code.

defaults:
  model: sonnet
  systemPromptPath: ./test_writer.prompt
  codeMode:
    enabled: true
```

### Documentation Editor

Specialized for documentation tasks:

```yaml
subagent:
  name: update_docs
  description: Update documentation based on code changes.

defaults:
  model: sonnet
  systemPromptPath: ./doc_writer.prompt
  codeMode:
    enabled: true
```

## Execution Model

When a parent invokes a subagent tool:

1. The MCP server receives the tool call with `prompt` and optional `inputs`
2. Input files are read and added to context
3. The subagent runs to completion
4. Results are returned to the parent

### Inheritance

The subagent inherits from the server process:
- Current working directory (CWD)
- Environment variables
- File system access

This means subagents "feel" co-located with the parent session.

### Persistence

Subagent execution traces are saved to `.cpeconvo` with labels like:

```
subagent:review_changes:a1b2c3d4
```

View traces with:

```bash
cpe conversation list
cpe conversation print <id>
```

## Troubleshooting

### "config must define a subagent for MCP server mode"

Your config is missing the `subagent` block. Add:

```yaml
subagent:
  name: my_tool
  description: What this tool does.
```

### "--config flag is required for mcp serve"

The `mcp serve` command requires an explicit config path:

```bash
cpe mcp serve --config ./my_subagent.cpe.yaml
```

### "failed to read output schema file"

Check that `outputSchemaPath` points to a valid JSON file relative to CWD.

### Subagent returns errors instead of results

Check:
1. API key environment variable is set
2. System prompt file exists at specified path
3. Model reference matches a model in the `models` list

### Structured output not returned

Ensure your system prompt instructs the model to call `final_answer`:

```text
You MUST call the final_answer tool with your response.
```

### Tool not appearing in parent

Verify the MCP server config in the parent:

```yaml
mcpServers:
  my_subagent:
    command: cpe
    args: ["mcp", "serve", "--config", "./path/to/config.yaml"]
    type: stdio  # Must be stdio
```

Test the server directly:

```bash
# Start server in one terminal
cpe mcp serve --config ./my_subagent.cpe.yaml

# In another terminal, check it responds to MCP protocol
```

## Examples

See [examples/subagent/](../examples/subagent/) for complete working examples:

- **review.cpe.yaml**: Diff reviewer with structured output
- **coder.cpe.yaml**: Code mode subagent for making changes
