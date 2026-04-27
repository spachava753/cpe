# Bundled Text Edit MCP Tool Plan

## Goal

Bundle a `text_edit` tool into CPE so users can create and modify files without installing an external editor MCP server. The bundled tool should behave like the current `text_edit` tool exposed by external MCP servers, while still flowing through CPE's MCP integration instead of creating a separate tool-registration path.

## Config Design

Use the existing per-model `mcpServers` map and introduce a new MCP server `type` enum value:

```yaml
models:
  - ref: gpt
    mcpServers:
      editor:
        type: builtin
        enabledTools:
          - text_edit
```

`type: builtin` means CPE provides the MCP server in-process. For the first implementation, the bundled server exposes `text_edit` only.

Validation rules:

- `type` accepts `stdio`, `sse`, `http`, and `builtin`.
- `type: builtin` is mutually exclusive with non-empty `command`, `args`, `url`, `headers`, and `env`.
- `type: builtin` allows `timeout`, `enabledTools`, and `disabledTools`.
- Builtin tools still participate in existing per-server filtering.
- Builtin tools still participate in duplicate tool-name detection after filtering.

## Tool Shape

Expose one MCP tool named `text_edit`.

Input:

```json
{
  "path": "relative/or/absolute/path",
  "old_text": "exact text to replace, optional",
  "text": "replacement text or new file content"
}
```

Behavior:

- If `old_text` is omitted or empty, create a new file.
- Create creates parent directories as needed.
- Create fails if the target file already exists.
- Create reports write and close errors before returning success.
- If `old_text` is non-empty, replace exactly one occurrence in an existing file.
- Replacement fails when the file does not exist, the target is a directory, `old_text` is not found, or `old_text` appears more than once, including overlapping matches.
- Replacement rejects symlink targets instead of replacing the symlink with a regular file.
- Replacement preserves existing file permissions.
- Replacement rejects invalid UTF-8 input files to avoid corrupting binary files.
- Relative paths resolve against the process working directory.

Output should include both readable text content and structured output with fields like `path`, `operation`, and `replacements`.

## Runtime Integration

Extend `internal/mcp.InitializeConnections` and its helper path:

- Existing `stdio`, `http`, and `sse` server types keep the current transport behavior.
- `type: builtin` creates an in-process MCP server using the Go MCP SDK.
- The server is connected to CPE's MCP client using `mcp.NewInMemoryTransports()`.
- Normal runtime initialization lists tools through the normal MCP client session.
- Existing filtering and duplicate detection run unchanged after connection.
- Existing tool adaptation through `mcp.ToGaiTool` and `mcp.NewToolCallback` registers the tool with the agent runtime.

The MCP package keeps connection and tool listing separate:

- `ConnectServer` connects only and does not require `tools/list`.
- `ConnectAndListServer` connects, lists tools, and applies filters.
- `InitializeConnections` uses the connect-and-list path for runtime registration and duplicate detection.

This preserves existing `cpe mcp info` and direct `mcp call-tool` behavior while still letting runtime startup and `mcp list-tools` inspect tool catalogs.

## Code Mode Integration

Builtin tools must not be exposed to code mode.

Change `codemode.PartitionTools` so connections with `conn.Config.Type == "builtin"` are always placed in `excludedByServer`, regardless of `codeMode.excludedTools`. They will be registered as normal conversational tools while `execute_go_code` receives only non-builtin MCP tools.

This avoids re-exec or stdio shims for bundled tools and keeps file editing as an explicit tool turn outside generated code.

## Implementation Steps

1. Add `builtin` to `mcpconfig.ServerConfig.Type` validation and update config schema generation.
2. Add `internal/textedit` with pure create/replace file behavior and tests.
3. Add bundled MCP server construction under `internal/mcp`.
4. Extend MCP connection setup to support `type: builtin` via in-memory transports.
5. Ensure builtin server lifecycle is closed/canceled through `MCPState.Close`.
6. Update code-mode partitioning to exclude builtin connections.
7. Update README, design docs, examples, and package docs.
8. Add tests for config validation, MCP initialization/filtering/duplicate behavior, MCP CLI commands, code-mode exclusion, and text-edit behavior.
9. Run `go fmt ./...`, `go test ./...`, `go vet ./...`, and `go run ./build lint`.
