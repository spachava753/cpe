# Subagent Examples

This directory contains example subagent configurations for CPE's MCP server mode.

## Examples

### review.cpe.yaml - Diff Reviewer

A thinking-focused subagent that reviews code changes and returns structured feedback.

- **No code execution**: Pure analysis and reasoning
- **Structured output**: Returns JSON with severity levels and suggestions
- **Use case**: Automated PR reviews, code quality gates

```bash
cpe mcp serve --config ./review.cpe.yaml
```

### coder.cpe.yaml - Code Mode Subagent

A code mode subagent that can make changes and run commands.

- **Full access**: Can read/write files and execute shell commands
- **Verification**: Runs tests after making changes
- **Use case**: Implementing features, fixing bugs, refactoring

```bash
cpe mcp serve --config ./coder.cpe.yaml
```

## Using with a Parent Agent

Configure your parent CPE to use these subagents as MCP servers:

```yaml
# parent.cpe.yaml
mcpServers:
  reviewer:
    command: cpe
    args: ["mcp", "serve", "--config", "./examples/subagent/review.cpe.yaml"]
    type: stdio
  
  coder:
    command: cpe
    args: ["mcp", "serve", "--config", "./examples/subagent/coder.cpe.yaml"]
    type: stdio
```

Then invoke from the parent:

```bash
cpe "Review my recent changes" -i git_diff.txt
# Parent can delegate to review_changes tool

cpe "Add unit tests for the User model"
# Parent can delegate to implement_change tool
```

## Customization

These examples are starting points. Customize them by:

1. Adjusting system prompts for your workflow
2. Adding MCP servers for additional capabilities
3. Modifying output schemas for your needs
4. Tuning model and generation parameters

See [docs/subagents.md](../../docs/subagents.md) for detailed documentation.
