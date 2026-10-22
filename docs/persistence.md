# Persistence and recovery

[All guides](README.md)

CPE saves a session as work progresses. Returning to it restores the conversation
and saved data, including script-output overflow logs, then opens fresh resources
for new work.

## What survives

There are three places to think about state:

```text
 Saved session                 Live resources             Outside CPE
 +------------------------+    +----------------------+   +------------------+
 | Accepted transcript    |    | Script variables     |   | Workspace files  |
 | Compaction summary     |    | Open file handles    |   | Git state        |
 | Model and reasoning    |    | Running commands     |   | Remote services  |
 | Token usage            |    | MCP connections      |   | External jobs    |
 | Aborted responses      |    +----------------------+   +------------------+
 | Durable script data    |        created afresh          keep their own
 +------------------------+        as needed               current state
       restored from disk
```

The transcript can include prompts, images, tool inputs and output, and provider
reasoning metadata. Incomplete model responses are also saved, but kept apart
from the accepted transcript. They are excluded from future model context and
from transcript replay to the client. See [Failures and cancellation](failures.md).

The default checkpoint directory is `~/.cpe/sessions`. The `--sessions` launch
option selects another absolute directory. Checkpoints are saved with
private permissions on Unix. On Windows, protect the directory with filesystem
ACLs. The contents can be as sensitive as the source code, screenshots, and
credentials encountered during a task. Compaction leaves those contents on disk.

## Checkpoints during a turn

CPE records progress at points where continuing could otherwise lose track of
what happened:

```text
 save user prompt
       |
 ask model
       |
 save complete response containing tool requests
       |
 execute a tool
       |
 save its result ----> report completion to client
       |
 continue with remaining work
       |
 save final state at turn end
```

A checkpoint write replaces the previous checkpoint atomically. If a required
save fails, the turn stops; turn-end cleanup still attempts to save known
results. This reduces the chance of further effects accumulating without a
saved record of them.

The checkpoint and an external action cannot be saved as one transaction. There
is always a possible interval between a command changing a file or a remote
service and CPE saving its result. Atomic checkpoint writes protect the saved
record from partial replacement; they cannot make an external action and its
record inseparable.

When a saved call has no result, restoration adds an interrupted result with an
uncertain outcome. CPE never reruns it automatically. Check the affected state
before deciding whether to repeat it. A pushed branch or submitted remote job,
for example, may already exist even if its success message was lost.

Session storage supports concurrent sessions in a process. Avoid opening and
writing the same saved session from several CPE processes; checkpoints provide
no cross-process coordination for that use.

## Durable data for scripts

Every Starlark invocation starts fresh. The session's key-value store gives
scripts a place to retain selected data across invocations:

```python
kv.store("checked_packages", ["parser", "renderer"])
```

A later invocation can retrieve and update it:

```python
packages = kv.get("checked_packages")
packages.append("cli")
kv.store("checked_packages", packages)
print(kv.keys())
```

Each successful `kv.store` is checkpointed before it returns. It survives a later
error or cancellation in the same script. A failed store leaves the previous
stored value intact. Retrieval returns an independent copy; changes become
persistent when explicitly stored again.

Keys are strings, and `kv.keys()` returns them in sorted order. A missing key
raises an error. Values can contain numbers, strings, booleans, `None`, bytes,
and lists, tuples, or dictionaries without cycles. Functions, modules, open
resources, and MCP result wrappers cannot be stored. For MCP data, save the
structured content or a content item's raw data.

```text
 Invocation 1                  Saved session               Invocation 2
 local data -- kv.store -----> snapshot
 local data can change            |
 independently                    +---- kv.get ----------> mutable copy
                                                            |
                              new snapshot <--- kv.store ---+
```

This data survives turns, model changes, compaction, closing and reopening, and
process restarts. Forks inherit independent copies. New unrelated sessions
start empty, and deleting a session removes its data.

CPE sets no separate byte quota for checkpoints or KV values, and no fixed
count cap for keys or saved sessions. Host memory and storage still constrain
them. The [limit reference](limits.md#data-without-a-separate-cpe-size-quota)
explains how stored data differs from emitted output.

Stored values enter model context only when a script emits them. A large saved
result can remain available without repeatedly appearing in the conversation.
A [compaction summary](compaction.md) can mention the useful keys so the model
knows where to look later.

## Saved script output

When an invocation's combined emitted text exceeds **128 KiB**, CPE saves the
complete text as a string in session KV. The tool result contains a preview and
the exact `overflow_<sha256>` key. Use that key with `kv.get` to inspect the full
log; identical complete output reuses the same key.

These logs include execution diagnostics and text emitted before a script fails
or is canceled. They survive model changes, compaction, closing and reopening,
and process restarts. Forks inherit independent copies, and deleting the session
removes them along with its other KV data.

Automatic overflow saving happens when the invocation ends. A crash before that
point can lose the emitted text. Explicit `kv.store` calls still checkpoint
immediately when used earlier in a script. If automatic saving fails, the tool
result reports the failure and gives no claim that the complete log was saved.

Captured command stdout and stderr reach the script in full. Automatic overflow
storage covers text the script emits and execution diagnostics. To preserve an
unprinted command stream, explicitly store it before the invocation ends.
See [Long output and retrieval](tool-runtime.md#long-output-and-retrieval).

## Restoring the environment

Restoration brings back the saved model and reasoning choices, even if the
global default has changed. A model missing from the current configuration
causes an error. MCP connections are reopened and their tools rediscovered.
Project instructions and skill metadata are read again.

Files, databases, remote accounts, and command processes have their own
lifetimes. Restoring or forking a conversation leaves them as they are. The
model may need to inspect the current workspace before continuing an old task.
See [Sessions and turns](sessions.md) for the differences between load, resume,
and fork.
