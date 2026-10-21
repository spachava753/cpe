# Sessions and turns

[All guides](README.md)

A session is a continuing conversation with a working directory, a selected
model, saved history, and its own tool resources. You can send many prompts to
one session, close it, and return to it later.

## From connection to invocation

One CPE process serves one ACP connection. That connection can have several live
sessions. Each session accepts one active prompt at a time; separate sessions
can work concurrently.

```text
 ACP connection
 |
 +-- Session A: conversation, working directory, model, MCP connections
 |    |
 |    +-- Turn: one user prompt through completion or interruption
 |         |
 |         +-- Generation: model response
 |         +-- Tool invocation: fresh script execution
 |         +-- Generation: model sees results and continues
 |         +-- ... final response
 |
 +-- Session B: its own conversation and live resources
```

Separate sessions have independent conversation and durable script data. They
can still touch the same files, environment, or external services on the host.
Two agents editing the same checkout need the same care as two people editing
it. A separate worktree can separate their file changes.

## Opening a session

The client supplies an absolute working directory. CPE uses it as the base for
relative tool paths and for discovering project instructions, skills, and
plugins. It checks that the directory exists, prepares the session's guidance,
connects its MCP servers, discovers their tools, and saves an initial checkpoint.
The session becomes available after setup succeeds.

Setup has a **10-minute deadline** by default, configurable with a positive
`--setup-timeout` value such as `--setup-timeout=2m`. A failure to read required
configuration or connect a configured MCP server prevents the session from
opening. Connections already opened during failed setup are closed. Once setup
succeeds, its deadline has no effect on later work.

The working directory also determines the scope of automatic instruction
[discovery](instructions.md). Tools retain access to paths elsewhere on the host.

## Following a turn

```text
 User prompt
     |
     v
 Save prompt --> Request model response <------------+
                       |                             |
                +------+-------+                     |
                |              |                     |
             answer       tool requests              |
                |              |                     |
                |       Save requested calls         |
                |              |                     |
                |       Execute and save results ----+
                v
             turn ends
```

Text and reasoning can stream to the client while the model is responding.
Tools start after a complete response has been accepted. CPE saves the requested
calls before execution, and saves completed results before reporting completion
to the client or continuing with later work. These checkpoints make
[recovery](persistence.md) possible when a turn ends unexpectedly.

CPE places no overall time limit or generation-count limit on prompt turns.
A provider can still limit a response, refuse a request, or return an error.
The user or client can cancel an active turn. The [limit reference](limits.md)
collects the exact timeouts, retry budgets, output limits, and defaults.

## Session controls

| Action | What happens |
| --- | --- |
| Cancel | Stops the active turn; the session remains available for another prompt. |
| Close | Cancels and waits for active work, then releases tool resources and MCP connections. The saved session remains. |
| Load | Opens a saved session with fresh resources and sends its accepted transcript to the client for display. |
| Resume | Opens a saved session with fresh resources, leaving transcript display to the client's existing state. |
| Fork | Copies an open, idle session into a new session with its own identifier and resources. |
| Delete | Closes live resources if necessary, then removes the saved session, including durable script data. |
| List | Returns up to **100 saved sessions per page**, including sessions with no live resources. The client can request the next page with the returned cursor. |

Loading and resuming require the session to be closed in that connection.
Changing the model or reasoning setting also requires an idle session.

A fork inherits the transcript, compaction state, model and reasoning selection,
usage totals, and an independent copy of durable script data. The client supplies
its working directory and MCP configuration. Forking leaves files and remote
services untouched, so choosing the same directory means both sessions see the
same workspace.

Load, resume, and fork rediscover project guidance and extensions from the
requested directory. The restored conversation can therefore meet a different
workspace or set of tools. See [Instructions and context](instructions.md) and
[MCP, plugins, and skills](extensions.md).

## When the client disconnects

Disconnecting cancels work across all live sessions. CPE waits for execution to
finish and releases each session's runtime and MCP connections. Credential
refresh work also ends when the connection shuts down. Saved checkpoints remain
available for a later process.

Cancellation and shutdown preserve completed effects. For example, a command
may have written a file before it was stopped. The [failure guide](failures.md)
explains how to continue when the last operation's outcome is uncertain.
