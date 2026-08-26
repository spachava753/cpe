/*
Package acp implements CPE's agent Client Protocol server and persisted-session
command operations.

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
Generated assistant blocks receive model-ref and provider-URL provenance before
persistence so provider-specific thinking is replayed only to the exact profile
and endpoint that produced it.
Process-level config loading, database path selection, storage lifecycle, and
Cobra wiring are composed by internal/cmd. Framework-agnostic helpers in this
package list persisted sessions, render complete compaction-aware history as
Markdown, delete sessions, and create shared-history forks for those commands.
A session's Starlark REPL is process-local and is not part of persisted history.
Canceling a prompt during Starlark evaluation interrupts the active execution,
publishes and persists a failed tool result with any captured output, and then
returns the ACP cancelled stop reason. The ACP loop attaches the persisted
assistant message containing each tool call to its callback context. Code Mode
uses that execution-scoped cutoff for acp.star current-session reads because the
session's committed head advances only after the complete prompt turn. The
module reconstructs prior compacted branches through storage and may list or
read sessions from other exact working directories.

Conversation compaction validates tool arguments against the configured JSON
Schema before rendering the replacement root message. Compaction must be the
only tool call in its assistant response; mixed responses reject every call
without executing siblings. Mixed-call and schema-validation failures allow at
most three recoverable retries for each compaction cycle. A successful
compaction resets the retry budget. Initial-message template execution failures
are terminal configuration or runtime errors. Failed attempts do not reset
stateful tools or consume a successful compaction restart. Successful attempts
persist the completed tool result and replacement root before resetting
compaction-scoped state or publishing completion, so session loading can replay
both the call and its completion. Because the two branches cannot currently be
saved atomically, failure to persist the replacement root after the successful
result is an invariant panic rather than a returned false-success branch. When
load or resume reconstructs inactive session state, or fork creates a new branch
runtime, ACP appends a one-time reset warning to the next user prompt so the
model does not rely on REPL globals or resources from the prior runtime.

ACP prompt work attaches session_id and the session's immutable cwd to its
context. Context-aware logs emitted by ACP and downstream MCP, skill discovery,
and code-mode operations inherit those structured fields. JSON-RPC access logs
also promote sessionId and cwd from request or response payloads when present.
*/
package acp
