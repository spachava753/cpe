/*
Package acp implements CPE's Agent Client Protocol server.

The server is launched by `cpe acp serve` and communicates with an ACP client
over stdio JSON-RPC. It owns ACP session lifecycle, session configuration,
prompt execution, cancellation, session load/resume/fork/delete behavior, and
translation between ACP protocol updates and gai dialogs. Session listing passes
the protocol's optional working-directory filter through to persistence so
clients can scope centralized history to a workspace. Loading and resuming a
session require the request working directory to exactly match its persisted
working directory.

At session runtime, this package resolves the selected CPE model profile,
renders the configured system prompt, initializes provider generators through
internal/agent, registers built-in tools, connects configured and client-provided
MCP servers, and persists session state through an injected SQLite store.
Process-level config loading, database path selection, and storage lifecycle are
composed by internal/cmd before the ACP server starts.
*/
package acp
