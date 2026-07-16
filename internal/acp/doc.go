/*
Package acp implements CPE's Agent Client Protocol server.

The server is launched by `cpe acp serve` and communicates with an ACP client
over stdio JSON-RPC. It owns ACP session lifecycle, session configuration,
prompt execution, cancellation, session load/resume/fork/delete behavior, and
translation between ACP protocol updates and gai dialogs. Session listing passes
the protocol's optional working-directory filter through to persistence so
clients can scope centralized history to a workspace. Loading and resuming a
session require the request working directory to exactly match its persisted
working directory. Prompt completion advances from the previously observed
session head so another process cannot be silently overwritten. A conflicting
advance means multiple ACP processes own the same session, which is an invalid
deployment; prompt handling panics rather than treating it as a recoverable
result. Reclaiming messages or cost from that failed process is intentionally a
separate maintenance concern.

At session runtime, this package resolves the selected CPE model profile,
renders the configured system prompt, initializes provider generators through
internal/agent, registers built-in tools, connects configured and client-provided
MCP servers, and persists session state through an injected SQLite store.
Process-level config loading, database path selection, and storage lifecycle are
composed by internal/cmd before the ACP server starts.

ACP prompt work attaches session_id and the session's immutable cwd to its
context. Context-aware logs emitted by ACP and downstream MCP, skill discovery,
and code-mode operations inherit those structured fields. JSON-RPC access logs
also promote sessionId and cwd from request or response payloads when present.
*/
package acp
