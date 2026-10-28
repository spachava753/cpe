# Tool execution

[All guides](README.md)

The model has exactly two tools. `bash` runs a bash script. `starlark_exec` runs
a [StarlarkX](https://github.com/spachava753/starlarkx) script, a Python dialect,
with builtins for files, media, MCP tools, and running bash. Both run in the
session directory, with the privileges of the user running CPE.

MCP tools are never offered to the model as separate tools. A script calls them,
combines their results, and prints only what the model needs. That keeps tool
schemas out of every request and lets one script do work that would otherwise
take many model round trips. [MCP, plugins, and skills](extensions.md) explains
where servers come from and how the model finds their tools.

## The two tools

`bash` runs its script with `bash -c` in the session directory. stdout and
stderr share one stream, so the result shows them interleaved in the order they
were written. Standard input is empty. A nonzero exit status is reported after
the output and marks the result as failed.

`starlark_exec` runs its code in a fresh StarlarkX environment. Variables,
functions, and results last for one call; a later call starts over. Data that
should outlive a call goes to a file, which later scripts and commands can read.

```text
 Model asks for a tool call
            |
     +------+---------------+
     |                      |
   bash               starlark_exec
     |                      |
 bash -c script       fresh StarlarkX environment
     |                  files, bash, MCP tools, view
     |                      |
     +------+---------------+
            |
  Output tail, then exit status, timeout, or error
            |
  One tool result returned to the conversation
```

Both tools accept an optional `timeout` in seconds. Without one, a call runs
until it finishes or the turn is cancelled. A timeout stops the call: bash's
process group is killed, and a script is stopped along with any MCP calls and
commands it is waiting on. The result keeps the output produced before the
timeout and ends with a notice saying so.

## Commands and their lifetime

Each bash script runs in its own process group. When the script exits, times
out, or is cancelled, CPE kills the whole group, including background children.
Starting a background command is therefore unsuitable for keeping a service
alive between calls; a job submitted to an external service keeps that
service's lifetime. On platforms without process groups, CPE terminates only
the direct child.

After the script exits or is stopped, CPE waits up to **1 second** for output
pipes that a lingering child still holds, then closes them.

## Scripts

StarlarkX reads like Python with a few deliberate differences. There are no
classes, exceptions, `import`, or `load`, and no standard library: the builtins
below are everything a script has. Recursion is rejected; `while` loops and
top-level `if` and `for` statements work. `json.encode`, `json.decode`, and
`json.indent` are available without loading anything.

| Builtin | Purpose |
| --- | --- |
| `read_file(path)` | Returns a whole file as bytes; use `.decode()` for text. |
| `write_file(path, data)` | Creates or replaces a file with str or bytes, creating parent directories. |
| `edit_file(path, old, new)` | Replaces exactly one literal occurrence of `old`. |
| `bash(script, timeout=None)` | Runs bash exactly as the `bash` tool does and returns its result. |
| `view(data, mime_type)` | Shows media to the model: images, audio, video, or PDFs. |
| `tools.<name>(...)` | Calls an MCP tool. |
| `search_tools`, `describe_tool`, `describe_server` | Find MCP tools and read their schemas. |
| `parallel(calls)` | Runs several host calls at once. |
| `compact(summary)` | Requests [compaction](compaction.md). |

Relative paths are relative to the session directory; absolute paths and `..`
also work. `write_file` writes a temporary file beside the target and renames it
into place, so readers never see a partial file. It keeps an existing file's
permission bits and follows symbolic links, so the link stays a link.

### Errors end the script

An error stops a script at the failing statement. Nothing after it runs, and
nothing before it is undone. Suppose a script edits a file, then fails calling
a remote tool: the edit stays, the remote call is reported as failed, and later
statements never run. The result contains the output printed before the
failure, followed by the error and its traceback, so the model can see where the
script stopped.

Two kinds of failure are deliberately returned instead of raised, because a
script can do something useful with them:

- `bash(...)` returns its exit status in `exit_code`. Many commands use exit
  statuses as answers, such as `grep` finding nothing.
- An MCP tool's own error returns normally with `is_error` set, carrying the
  server's explanation. A lost connection still raises, because the call's
  outcome is then unknown.

### Exact edits

`edit_file(path, old, new)` succeeds only when `old` is nonempty, matches exactly
once, and the replacement changes the file. Overlapping occurrences count as
multiple matches. An empty `new` deletes the match. Whitespace and line endings
must match exactly. On success it returns `None`; on failure it raises an error
naming the problem: no match, how many matches, or no change.

Because a failed edit raises, **it ends the whole script**. Later edits that
would have succeeded, MCP calls, commands, and prints do not run, while earlier
edits stay:

```text
 edit A succeeds -> file contains A
 edit B fails    -> script stops here
 edit C          -> never runs
 MCP call        -> never runs
```

Put edits before anything that depends on them, and use separate scripts for
edits that should not block one another. The replacement is atomic for the
file. Rereading the file just before writing catches some concurrent changes,
but only on a best-effort basis: two edits of the same file running at once can
both succeed with one change lost, and changes made since the model last read
the file go undetected. Coordinate edits to the same file, and read before
editing when sharing a workspace.

### Running bash from a script

`bash(script, timeout=None)` uses the same execution path as the `bash` tool and
returns a result with these fields:

| Field | Meaning |
| --- | --- |
| `output` | stdout and stderr interleaved; at most the last 1 MiB |
| `exit_code` | Exit status; 128 plus the signal number when killed by a signal |
| `timed_out` | Whether the timeout stopped the command |
| `truncated` | Whether `output` is a tail of longer output |
| `full_output_path` | File holding the complete output when truncated, otherwise `None` |

A script receives more output than the model would, because it is expected to
filter it before printing. The bound keeps a runaway command from filling the
memory of the CPE process, which serves every session.

### Running calls in parallel

`parallel(calls)` takes a list of `(function, input)` pairs and returns their
results in the same order. The function must be an MCP tool, `bash`,
`read_file`, `write_file`, or `edit_file`, and the input is a dictionary of
keyword arguments:

```python
outs = parallel([
    (tools.github__get_issue, {"owner": "acme", "repo": "app", "number": n})
    for n in [12, 15, 19]
])
```

Up to **16** calls run at once. StarlarkX code itself stays single-threaded:
only the host calls overlap, and each receives its own copy of its input. If a
call raises, the remaining calls are cancelled and the error ends the script.
A tool that merely reports `is_error` does not stop the others.

### Viewing media

`view(data, mime_type)` attaches media to the tool result after the printed
text, in the order the script attached it. `mime_type` is required; CPE never
guesses a type from the bytes. Image, audio, and video types and
`application/pdf` are accepted. MCP image and audio content carries its MIME
type, so `view(block.data, block.mime_type)` works directly on MCP results.

CPE sets no size or format limits of its own. Whether media is acceptable
depends on the provider and model. When the selected model's configuration does
not accept a modality, the model receives a notice instead, naming a file that
holds a copy of the media; history keeps the original for models that can use
it. When the provider rejects media in the latest tool results, CPE saves each
rejected item to a file, replaces it with a notice containing the provider's
message and the file path, and retries the request once. The model can then
resize or convert the file and view it again. If the retry fails as well, the
media was not the cause; it stays in history and the turn fails as usual. The
retry does not count against [response retries](failures.md).

## Output the model sees

Each tool result shows at most the **last 2000 lines or 50 KiB** of output,
whichever limit is reached first. The end of the output usually matters most:
the failing test, the compiler error, the summary line. When output exceeds
either limit, the result begins with a notice:

```text
[Output truncated: showing lines 98001-100000 of 100000 (11.7 KB of 575.1 KB). Full output: /tmp/cpe-session-…/call_…-bash-….txt]
```

The model inspects the complete output with ordinary tools, such as `rg`,
`sed -n`, `tail`, or `read_file`. Exit statuses, timeout notices, and script
errors follow the output and are never truncated, so the model always learns why
a call failed. If the last line alone exceeds the byte limit, the model sees its
end, starting at a character boundary, and the notice says the first line shown
is partial. Media attached with `view` does not count toward the text limit.

Small output stays in memory. Once output exceeds a limit, the complete stream
is written to a file in the session's temporary directory, so a very large
output never sits whole in memory. If that file cannot be created, the notice
says the full output could not be saved. A script that finishes without printing
anything returns a short completion notice.

The [output-limit reference](limits.md#tool-output) lists the exact thresholds.
Overflow files are temporary; see
[Persistence and recovery](persistence.md#overflow-files).

## Host access

Tools run with the privileges of the user running CPE. They can access the host
filesystem, network, environment, and processes. CPE has no approval prompts,
allowlists, or sandbox. Run CPE and install extensions with the same care you
would give any program with your account's access.

For interruption and retry decisions, continue with
[Failures and cancellation](failures.md).
