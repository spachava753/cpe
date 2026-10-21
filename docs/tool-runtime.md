# Tool execution

[All guides](README.md)

The model works through a single Starlark tool. A script can read files, run
commands, make HTTP requests, call MCP tools, and select which results to show
the model. This lets it combine several operations and reduce large results
before putting them into the conversation.

Starlark has Python-like syntax. Dyson supplies the filesystem, HTTP, process,
and other host APIs. Only the supported parts of those APIs are available;
scripts should inspect the module or its documentation when needed.

## A fresh script each time

```text
 Model asks to run a script
          |
          v
 Refresh changed MCP tool lists
          |
          v
 Fresh execution environment
   imports, variables, functions
   host operations and MCP calls
   explicit output
          |
          v
 Close invocation resources
          |
          v
 One tool result returned to the conversation
```

Variables, functions, imports, and open file handles last for one invocation.
A later invocation starts fresh. Use the session's
[durable key-value store](persistence.md) for data that should survive. A stored
value is available after compaction or restoration without keeping a script
running.

Errors stop the script at the failing operation. Earlier output and effects
remain. Suppose a script edits a file, stores a result, then fails while calling
a remote tool. The file edit and successful store remain in place. The model
receives the emitted output along with the execution error and can inspect
state before continuing.

## Host access

Tools run with the privileges of the user running CPE. They can access the host
filesystem, network, environment, and processes. The session directory is the
base for relative paths; absolute paths and parent traversal also work. CPE has
no runtime approval prompts, allowlists, or sandbox boundaries.

Host environment and process APIs can affect the shared CPE process. Such
changes may be visible to other sessions. Fresh script variables provide no
isolation from host-wide effects. Run CPE and install extensions with the same
care you would give any program with your account's access.

## Commands and their lifetime

Scripts run commands through Dyson's subprocess module. Shell features such as
pipes and redirection are available by invoking a shell explicitly:

```python
load("subprocess.star", "subprocess")
result = subprocess.run(
    ["bash", "--noprofile", "--norc", "-c", "pwd && ls"],
    capture_output=True,
    check=True,
)
print(result.stdout.decode())
print(result.stderr.decode())
```

The exit status matters. Without `check=True`, a nonzero exit status is a result
the script needs to inspect. A command returning an error status does not by
itself tell the model what went wrong. Captured stdout and stderr become visible
when the script prints them.

Commands receive explicit input or an empty input stream. They do not inherit
the ACP protocol streams. Each command retains the first **128 KiB of stdout**
and **128 KiB of stderr**, adding a truncation notice when more was produced.
Merged stdout and stderr share one 128 KiB budget. The command keeps running
after capture fills. For large logs, write them to a file and inspect relevant
sections; the discarded capture data cannot be recovered by printing it later.

Commands belong to the invocation. On Unix, CPE cleans up their process group
on completion or cancellation, including children that remain in that group.
On other platforms it terminates the direct child. Starting a background command
inside a script is therefore unsuitable for keeping a service alive between
calls. A job submitted to an external service has that service's lifetime.

Subprocess calls have no timeout by default. Scripts can set one in seconds,
for example `subprocess.run(..., timeout=30)`. After cancellation or an observed
process exit, CPE allows **1 second** for command exit or lingering output pipes
before forcing cleanup as needed. See
[Timeouts and retries](limits.md#sessions-timeouts-and-retries).

## Choosing what the model sees

A script can hold much more data than it sends back:

```text
 Files / commands / HTTP / MCP
              |
              v
       Script inspects results
              |
       +------+------------------+
       |                         |
       v                         v
 kv.store(selected data)    print(text) / image(bytes)
       |                         |
       v                         v
 saved for later            model-visible tool result
```

`print(...)` emits text. `image(data)` emits an image from raw PNG, JPEG, GIF, or
WebP bytes. The image function requires the bytes themselves; a script obtains
them from a file, HTTP response, or MCP result first. Text and images retain
their emission order.

MCP results remain available to the script as structured values. Merely calling
a remote tool adds no separate message to the conversation. The script can
check the error flag, extract useful fields, and emit only what is needed.
[MCP, plugins, and skills](extensions.md) explains discovery and result handling.

Successful exact edits automatically emit a confirmation and diff. Runtime
errors also appear in the tool result. If nothing is emitted, CPE returns a
short completion notice.

Each emitted text block retains up to **128 KiB**, plus a truncation notice if
needed. One invocation can emit up to **8 MiB** of content and **1,024 blocks**.
Exceeding either shared budget fails the invocation; earlier host effects remain.

An image can contain up to **6 MiB of raw bytes**, and its base64-encoded size
counts against the shared 8 MiB budget. A 6 MiB image fills that entire budget,
leaving no room for other output. A selected model without image input gets a
text notice in place of the image, while the original remains in saved history.

The [output-limit reference](limits.md#tool-output) explains how capture,
truncation notices, images, and block counts interact.

## HTTP request limits

Dyson's `requests` module buffers up to **64 MiB per response body**, including
each response retained from a redirect. Larger responses fail the request.
When redirect following is enabled, a request can follow at most **30 redirects**.

The script can set `timeout=30` for connect and read timeouts in seconds, or use
a pair such as `timeout=(5, 30)`. The default is `None`, with no overall request
deadline supplied by the module. The read timeout is renewed on reads, so a
response that keeps delivering data can take longer in total. See
[HTTP limits](limits.md#http-requests-from-scripts) for details.

## Exact file edits

`edit(filename, old, new)` replaces one literal occurrence in an existing file.
It succeeds only when `old` is nonempty, matches exactly once, and the replacement
changes the contents. Ambiguous matches, including overlapping occurrences,
produce an error. An empty replacement deletes the matched text. Whitespace and
line endings are matched exactly.

Edits in one script run in sequence against the current file contents:

```text
 edit A succeeds -> file contains A -> confirmation and diff
 edit B succeeds -> file contains A + B -> confirmation and diff
 edit C fails    -> script stops; A + B remain
```

Each replacement is atomic for that file. Symlinks are followed and retained,
with a limit of **40 followed links** across the target path. Permission bits
are preserved. Ownership, ACLs, and extended metadata may change. The edit checks
for content changes during its own operation, but cannot guarantee protection
against concurrent writers. It also cannot detect every change since the model's
earlier read. Read the current file before editing and review the resulting diff
when sharing a workspace.

For interruption and retry decisions, continue with
[Failures and cancellation](failures.md).
