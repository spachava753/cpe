# Failures and cancellation

[All guides](README.md)

When work stops, the most useful question is how far it got. A model response
may be incomplete, a script may have completed its first few operations, or an
external action may have succeeded before its result could be saved.

CPE preserves known progress and avoids automatically replaying actions whose
effects may already have happened.

## An interrupted model response

Text and reasoning can stream to the client before a response finishes. If the
response fails, some of that text may already be visible. CPE discards the
incomplete response rather than saving it. It never enters future model context
or later transcript replay.

The client always receives an assistant-message update explaining the
interruption and whether CPE will retry. Clients with session-notice support also
receive an advisory notice. These messages and notices are not saved or replayed.
Live display can therefore contain partial text and retry messages that will be
absent when the session is later loaded.

```text
 Saved conversation --> model attempt --> partial output --> failure
          |                                      |
          |                          discard incomplete response
          |                             send live updates
          |                                      |
          +---- same request, if retryable <------+
                          |
                   accepted response
                          |
                   continue the turn
```

Eligible transient or interrupted responses can retry **twice by default**,
for up to **three attempts** including the first. `--response-retries` changes
the retry count; zero disables retries. Each model response gets its own budget.

The first two retry delays are **250 ms** and **500 ms**. If more retries are
configured, the delay doubles up to **8 seconds**. A longer provider-requested
delay takes precedence. Permanent provider errors, credential failures, and
user cancellation do not trigger response retries. Unknown failures before any
output are not blindly retried.

A retry repeats the model request at the failure point. Failure to deliver
updates to the client stops retrying. Restoring a saved session never
automatically restarts an aborted response.

Provider-reported token consumption still accumulates across failed attempts and
retries. These counters are saved at normal checkpoints, not at every failure. A
process crash before the next checkpoint can lose the latest usage counts.

## Media the provider rejects

A request can also fail because the provider rejects media that a tool attached
with `view`, such as an image that is too large. When an invalid-request error
follows tool results containing media, CPE saves each item to a file, replaces it
with a notice giving the provider's message and the file path, and retries the
request once. The model can then convert the file and view it again. This retry
does not use the response-retry budget above. If it fails as well, the media was
not the cause: the turn fails as usual and history keeps the media. See
[Viewing media](tool-runtime.md#viewing-media).

## A failed script or command

A script error stops later statements. Earlier file changes, remote actions,
and printed output remain. A failed script is returned to the model as a tool
result, so the model can inspect the situation and decide how to continue.

```text
 Script
   edit a file           succeeds; file stays changed
   write a result file   succeeds; file stays written
   call a remote tool    connection fails
   run a test            never reached
```

A failed `edit_file` ends the script the same way. A command's nonzero exit
status from the `bash` tool marks the result as failed; inside a script, the
`bash` builtin returns it in `exit_code` for the script to check. An MCP tool can
report an error as an inspectable result; connection and protocol errors fail
the execution. See [Tool execution](tool-runtime.md) and
[MCP, plugins, and skills](extensions.md).

Long output is cut to its last lines, and the complete output is written to a
file the result names. If that file cannot be created, the result says so; the
visible tail does not show everything the command printed. See
[Output the model sees](tool-runtime.md#output-the-model-sees).

## Cancellation

Canceling stops the active turn and leaves the session available. CPE stops
local command execution, stops running scripts, and attempts to save the
known results. A tool call with no known completed result is recorded as
interrupted with an uncertain outcome.

Cleanup after an aborted response, at turn end, and during connection shutdown
each receives a **10-second budget**. Active work still has to finish and
resources still have to close, so total shutdown can take longer. The
[limit reference](limits.md#sessions-timeouts-and-retries) separates these cleanup
budgets from setup, command, and turn timeouts.

Cancellation leaves earlier effects in place. A remote service may have accepted
a request before cancellation reached it. Check that service or the affected
files before submitting the same operation again.

Closing a session cancels and waits for active work before releasing its runtime
and MCP connections. Disconnecting does this across all live sessions. See
[Sessions and turns](sessions.md).

## Failure to save progress

CPE saves the user prompt before asking the model and saves requested tool calls
before executing them. If those saves fail, the corresponding later work does
not start. If saving a completed result fails, the turn stops before proceeding
with later tools or generations. Turn-end cleanup still tries to preserve the
known results.

After a process crash, the checkpoint may contain a tool request without its
result. Restoration records that outcome as uncertain and continues without
replaying the call. This also covers the interval where an action succeeded but
its result had not yet reached disk. [Persistence and recovery](persistence.md)
explains the saved-state boundaries.

## Continuing after an interruption

Start with the affected state. For a file edit, inspect the file and diff. For a
remote action, check whether the service accepted it. For a long command, look
for its output files or other evidence of completion.

Then tell the agent what is known and ask it to continue from there. Repeating
an entire script can repeat successful early operations along with the failed
one. Checking first is especially important for actions such as publishing,
submitting jobs, or sending messages, where a duplicate has consequences outside
the workspace.
