/*
Package mcp provides MCP client integration for CPE runs.

Responsibilities:
  - build transports (stdio/http/sse) from configuration;
  - start CPE-provided builtin servers in-process;
  - connect to configured servers and fetch tool catalogs;
  - apply per-server tool filtering (enabledTools/disabledTools);
  - detect duplicate tool names across servers;
  - adapt MCP tools and calls to gai tool registration/callbacks.

Connection schema note:
shared MCP server configuration structs live in `internal/mcpconfig` so this
runtime package does not need to be imported by config loading code.
*/
package mcp
