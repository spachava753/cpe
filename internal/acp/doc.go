/*
Package acp implements CPE's Agent Client Protocol server.

The server is launched by `cpe acp serve` and communicates with an ACP client
over stdio JSON-RPC. It owns ACP session lifecycle, session configuration,
prompt execution, cancellation, session load/resume/fork/delete behavior, and
translation between ACP protocol updates and gai dialogs.

At session runtime, this package resolves the selected CPE model profile,
renders the configured system prompt, initializes provider generators through
internal/agent, registers built-in tools, connects configured and client-provided
MCP servers, and persists session state in storage.
*/
package acp
