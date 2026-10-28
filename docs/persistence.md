# Persistence and recovery

[All guides](README.md)

CPE saves a session as work progresses. Returning to it restores the conversation
and its settings, then opens fresh resources for new work.

## What survives

There are three places to think about state:

```text
 Saved session                 Live resources             Outside CPE
 +------------------------+    +----------------------+   +------------------+
 | Accepted transcript    |    | Script variables     |   | Workspace files  |
 | Compaction summary     |    | Open file handles    |   | Git state        |
 | Model and reasoning    |    | Running commands     |   | Remote services  |
 | Token usage            |    | MCP connections      |   | External jobs    |
 +------------------------+    +----------------------+   | Overflow files   |
                                   created afresh         +------------------+
                                                           keep their own
       restored from disk          as needed               current state
```

The transcript can include prompts, images, tool inputs and output, and provider
reasoning metadata. Incomplete model responses and their live abort/retry
messages and notices are not saved. They are excluded from future model context
and transcript replay to the client. See [Failures and cancellation](failures.md).

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

## Keeping data across scripts

Every `starlark_exec` call starts fresh, and CPE keeps no separate store for
script data. A script that needs to keep a result writes it to a file, and a
later script or command reads it back:

```python
write_file(".cpe-notes/checked.json", json.encode(["parser", "renderer"]))
```

```python
checked = json.decode(read_file(".cpe-notes/checked.json").decode())
```

Files have their own lifetime: they survive turns, compaction, restarts, and
restoration, and every session working in the directory sees the same files.
Forking a conversation does not copy them. Commands can process them with any
program, such as `jq` or `rg`. A [compaction summary](compaction.md) can mention
useful paths so the model knows where to look later.

## Overflow files

When a tool's output exceeds the [output limits](limits.md#tool-output), the
complete output is written to a file in the session's temporary directory, under
the system temporary directory, and the tool result names it. CPE does not
delete these files, so a resumed conversation can still refer to them, but they
are not part of the saved session. The operating system may remove them, for
example at reboot, in which case the model has to run the command again.
Restoring or forking a session creates a new temporary directory.

## Restoring the environment

Restoration brings back the saved model and reasoning choices, even if the
global default has changed. A model missing from the current configuration
causes an error. MCP servers are connected again, or an existing shared
connection is reused, and their tools are listed again.
Project instructions and skill metadata are read again.

Files, databases, remote accounts, and command processes have their own
lifetimes. Restoring or forking a conversation leaves them as they are. The
model may need to inspect the current workspace before continuing an old task.
See [Sessions and turns](sessions.md) for the differences between load, resume,
and fork.
