# Agent package boundaries

`agent` coordinates the ACP connection, live sessions, and prompt turns. The
packages below own resources and saved data that do not need ACP state. The CLI
assembles their implementations in `internal/cli/agent.go`.

| Package | Owns |
| --- | --- |
| `agent` | ACP translation and notifications, live sessions, turn cancellation, response retries, slash commands, prompt assembly, and checkpoint timing |
| `conversation` | Saved records, transcript and compaction invariants, token accounting, and atomic JSON checkpoint storage |
| `providers` | Model adapters, subscription credential attachment, and credential refresh workers |
| `mcpclient` | MCP transports, session-owned connections, and cached remote tool lists |
| `toolruntime` | Invocation-scoped Dyson spheres, session data, subprocess execution, tool naming and search, MCP result values, and model-facing output |
| `models` | Configured model definitions, model IDs, and reasoning selections |

The extracted packages do not import `agent` or ACP. `agent` depends on
`providers` only to recognize credential failures that must not be retried as
interrupted model responses. `toolruntime` uses `mcpclient.ServerID` and an
`MCPTools` interface; it does not own MCP connection shutdown.

## Resource ownership

The CLI creates the provider set and closes it when the ACP connection ends.
The provider set owns credential-refresh workers, while each live session owns
its MCP registry and tool runtime. Closing a session cancels and joins its active
turn before closing the runtime and MCP connections. A context used to set up a
session does not determine the lifetime of successfully opened resources.

`toolruntime.Local` supplies trusted host filesystem, HTTP, and command access.
`toolruntime.New` accepts an injected Dyson standard library for tests and other
explicit wiring. Both return the same `Host` interface. Neither introduces
execution approval or sandbox policy.

## Saved history and working context

A `conversation.Record` contains the transcript, selected model and reasoning,
usage, aborted responses, and optional compaction boundary. It does not contain
live resources or the Starlark state store. Loading, resuming, or forking restores
the record with an empty state store without replaying tool calls. `agent` adds
the runtime-reset notice to model context so inherited stored data is not mistaken
for live state.

The Starlark `compact(summary)` builtin belongs to `toolruntime`. It passes each
accepted summary synchronously to a receiver installed by `agent` for the turn.
`agent` keeps the last accepted summary, settles the response's tool calls, and
chooses the checkpoint boundary. An accepted summary survives a later script
failure. `conversation` checks that the saved boundary does not split a tool call
from its result, and constructs model context without deleting the transcript.

ACP prompt parsing stays in `agent`; script output conversion stays in
`toolruntime`. Each boundary owns its media checks and limits, rather than
making incoming prompts depend on runtime output policy. The small image
conversion functions are private to those boundaries.

## Tests

Package-local tests cover persistence, accounting, credential refresh, MCP
connection lifecycle, and runtime execution. `agent` integration tests exercise
these packages together through ACP, including compaction, cancellation,
response retries, and restoration. Run `go test ./...`, `go test -race ./...`, and
`go run ./build lint` from the repository root.
