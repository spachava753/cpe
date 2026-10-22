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
the ACP protocol streams. Captured stdout and stderr are returned in full, with
no CPE byte cap. A script can search or slice the complete streams before
printing them. Captured streams remain script-local unless the script prints
or explicitly stores them, and large captures consume host memory.

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
 saved for later            text preview / images
                           full long text saved in KV
```

`print(...)` emits text. `image(data)` emits an image from raw PNG, JPEG, GIF, or
WebP bytes. The image function requires the bytes themselves; a script obtains
them from a file, HTTP response, or MCP result first. All emitted text is combined
into one block, followed by image blocks. Images retain their relative emission
order even when the script interleaves text and images.

MCP results remain available to the script as structured values. Merely calling
a remote tool adds no separate message to the conversation. The script can
check the error flag, extract useful fields, and emit only what is needed.
[MCP, plugins, and skills](extensions.md) explains discovery and result handling.

Successful exact edits automatically emit a confirmation and diff. Runtime
errors also appear in the tool result. If nothing is emitted, CPE returns a
short completion notice.

## Long output and retrieval

Printing does not impose an execution budget or block-count limit. When an
invocation finishes, CPE combines its emitted text and execution diagnostics.
Text of **128 KiB or less** reaches the model in full. Longer text is saved as a
string in session KV under `overflow_<sha256>`. The model receives a head preview
with the exact retrieval key; the preview and notice together fit in 128 KiB.
Identical complete text reuses the same key.

Use the key supplied in the result to inspect the saved output. For example,
with that key assigned to `key` in a later script:

```python
log = kv.get(key)
print(log[-4000:])
```

This shows the last 4,000 bytes, which can be useful for a build summary
that falls beyond the preview. Scripts can also search the full string and
print just the relevant lines. Printing the whole log again produces another
preview if the combined text remains over 128 KiB.

Overflow saving happens after script execution. It includes text emitted before
failure or cancellation, along with the execution diagnostic. If saving fails,
the result reports the failure without claiming that a full log is available.
Earlier script actions remain completed. Saved logs survive compaction and
restoration, and forks inherit independent copies; see
[Persistence and recovery](persistence.md#saved-script-output).

Each image still has a **6 MiB raw-byte limit**. There is no combined text-and-image
byte cap or image-count cap in CPE. A selected model without image input gets a
text notice in place of the image, while the original remains in saved history.
Provider limits and host memory still constrain practical output sizes.

The [output-limit reference](limits.md#tool-output) lists the exact thresholds
and explains the differences between captured streams, saved logs, and previews.

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
