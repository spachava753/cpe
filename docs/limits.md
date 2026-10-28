# Limits and defaults

[All guides](README.md)

This page collects the limits you can encounter while using CPE, their current
values, and what happens when you reach them. Where a setting can change a
value, it is listed alongside it. The other values are fixed in the current
release.

Sizes use binary units: **1 KiB = 1,024 bytes** and **1 MiB = 1,048,576 bytes**.
Byte limits apply to the data size; a character can occupy several bytes.

## Tool output

Both tools show the model the end of their output, within fixed limits. Longer
output is written in full to a file and the result names it.

| Output | Limit or threshold | What happens |
| --- | --- | --- |
| Text in one tool result | Last 2,000 lines or 50 KiB (51,200 bytes), whichever is reached first | Longer output is cut from the front. The result starts with a notice giving the lines shown, the sizes, and the path of a file holding the complete output. |
| Exit status, timeout notice, or script error | Never truncated | Appended after the visible output, outside the limit. |
| A single line longer than the byte limit | Its last 50 KiB, from a character boundary | The notice says the first line shown is partial. |
| Output from the `bash` builtin inside a script | Last 1 MiB (1,048,576 bytes) | `truncated` and `full_output_path` report a longer stream; no line limit applies. |
| Media attached with `view` | No CPE limit | Not counted toward the text limit. The provider decides what it accepts. |
| Concurrent calls in one `parallel` call | 16 | Further calls wait for a free slot. |

Output within the limits stays in memory. Once it exceeds them, the complete
stream is written to a file in the session's temporary directory while the
command runs, so the full output never has to fit in memory. If that file
cannot be created, the notice says the complete output could not be saved, and
the visible tail is still returned.

```text
 command or script output
            |
   within the limits? ---- yes ----> whole output in the result
            |
            no
            |
 complete stream written to a file
            |
 result: notice with file path + last lines
            |
 then exit status, timeout, or error (never cut)
```

Overflow files are temporary; see
[Persistence and recovery](persistence.md#overflow-files). The selected
provider's context and media limits still apply.

## Sessions, timeouts, and retries

| Operation | Limit or default | Configuration and behavior |
| --- | --- | --- |
| Active prompts in one session | 1 | A second prompt is rejected while the first is active. Different sessions can run concurrently. |
| Session setup | 10 minutes | Set `--setup-timeout`, for example `--setup-timeout=2m`. It must be positive. Expiry fails setup. |
| Connecting to one MCP server and listing its tools | 1 minute | Set `--mcp-timeout` for `cpe acp`, or `--timeout` for `cpe mcp`. A server that exceeds it is skipped. |
| Keeping an unused shared MCP connection open | 30 seconds | After the last session using it closes. |
| Prompt-turn duration | No CPE-imposed deadline | The client can cancel; provider and individual operation limits still apply. |
| Model generations in a turn | No CPE-imposed count limit | Tool use can continue across many model responses. |
| Retries of one eligible model response | 2 retries by default: up to 3 attempts including the first | Set `--response-retries`; `0` disables these retries. This budget starts afresh for each model response. |
| Model-response retry delay | 250 ms, 500 ms, 1 s, 2 s, 4 s, then 8 s for later retries | A longer provider-requested delay takes precedence. With the default retry count, only the first two delays are used. |
| `bash` and `starlark_exec` runtime | No timeout by default | Pass `timeout` in seconds to either tool or to the `bash` builtin. Turn cancellation also stops the call. |
| Waiting for command exit or lingering output pipes after cancellation or observed process exit | 1 second | CPE terminates the child or closes remaining pipes when needed to finish waiting. This does not limit a normally running command to one second. |
| Cleanup after an aborted response, at turn end, or during connection shutdown | 10-second budget for each cleanup phase | Allows client notifications and, at turn end or shutdown, checkpointing after cancellation. It is not a guarantee that the entire shutdown finishes within ten seconds; joining active work and resource shutdown can take longer. |
| Listing saved sessions | Up to 100 sessions per page | The client uses the returned cursor to request the next page. |

[Sessions and turns](sessions.md) explains resource lifetimes.
[Failures and cancellation](failures.md) explains which responses qualify for
retry and why tools are never automatically replayed.

## Model context and output

| Measurement | Value | How it is used |
| --- | --- | --- |
| Model context window | The selected entry's `limit` in the model configuration | Required positive integer, in tokens; there is no universal default. Used for the context meter and warning threshold. |
| Requested generation output limit | The selected entry's `max_tokens` | Required positive integer, in tokens; there is no universal default. Normally sent as the provider's per-response output limit. |
| Context warning | 85% of the configured `limit`, rounded up to a whole token | Uses the latest successful generation's reported input plus output tokens, including cached input. Adds a warning to tool results. |

For example, `limit: 128000` gives a warning threshold of **108,800 tokens**.
Those values describe an example configuration; your model's entry supplies the
actual window. `max_tokens: 8192` requests at most **8,192 output tokens per
response** where the adapter sends that setting. A Responses model on the
ChatGPT subscription host uses the service's output controls; CPE omits its
configured `max_tokens` from those requests.

The context warning is advisory. It neither stops the turn nor compacts it
automatically. The provider ultimately decides whether a request fits. See
[Usage and context limits](usage.md), [Models and reasoning](models.md), and
[Compaction](compaction.md).

## Authentication

These limits apply to CPE's login and credential handling. They do not limit
ordinary model generations to the same durations.

| Operation or data | Value | Behavior |
| --- | --- | --- |
| Browser or device ChatGPT login | 15 minutes | The authorization flow times out; Ctrl+C can cancel earlier. |
| An authentication HTTP request during CLI login | 30 seconds | A request that takes longer fails, within the overall login deadline. |
| Device authorization polling | 5-second interval when the provider omits it | Provider-supplied intervals must be whole seconds from 1 through 900; invalid values fail login. |
| Start refreshing a ChatGPT token | Within 5 minutes of expiry | Starts background renewal while the unexpired token remains usable. |
| One refresh exchange attempt | Up to 30 seconds | The old token's remaining lifetime can shorten this while it is still valid. |
| Refresh retry delays | 1, 2, 4, 8, 16, then 30 seconds for later retries | Exchange retries stop at the old token's expiry. Already-expired credentials get one bounded exchange attempt. |
| Saving successfully rotated credentials | No fixed retry count or elapsed-time limit | Save retries continue until success, replacement by login/logout, or shutdown; the old token's expiry does not discard the replacement. |
| Authentication response body | 1 MiB (1,048,576 bytes) | Larger responses are rejected. |
| Saved credential file | 2 MiB (2,097,152 bytes) | Larger files are rejected on read or write. |
| OpenCode API key entered at login | 16 KiB (16,384 bytes) | Longer keys are rejected. A key must also be nonempty UTF-8 without whitespace or control characters. |
| Access-token lifetime reported by the provider | 1 through 31,536,000 seconds (365 days) | Values outside this range are rejected. If omitted, CPE uses 3,600 seconds (1 hour). |
| Local browser-login callback HTTP headers | 16 KiB (16,384 bytes), including the request line | Oversized requests are rejected by the callback server. |
| Local browser-login callback timing | 5 seconds each for reading headers, writing a response, and waiting idle between requests | Slow callback connections are closed or time out. These are separate from the 15-minute login deadline. |

The [credential guide](credentials.md) explains how renewal, persistence, and
concurrent login or logout interact.

## Discovery and file editing

| Operation or field | Value | Behavior |
| --- | --- | --- |
| `search_tools` results | 8 by default | Set a positive `limit` argument to request another maximum. There is no additional fixed result-count ceiling. |
| Tool and server descriptions in the system prompt | First line, cut at 500 characters | Full descriptions remain available through `describe_tool` and `describe_server`. |
| Skill/plugin source search during installation | 5 directory levels below the source root by default | Deeper packages are skipped. `--full-depth` removes the depth limit; `--path` selects an exact package directory. |
| Plugin name | 1–64 ASCII characters | An invalid or longer name is rejected. |
| Skill name length | 64 Unicode characters | A longer name is retained with a length warning. |
| Skill description length | 1,024 Unicode characters | A longer description is retained with a length warning. |
| Skill compatibility field | 500 Unicode characters | A longer value makes the skill definition invalid. |
| Matches for `edit_file` | Exactly 1 | Zero or multiple matches, including overlaps, fail without replacing the file and end the script. |

The installation search depth applies to finding packages in a source tree.
Project `AGENTS.md` discovery has no fixed depth cap; it skips directory symlinks
and `.git` directories. Skill character counts use Unicode code points.

See [MCP, plugins, and skills](extensions.md),
[Instructions and context](instructions.md), and [Tool execution](tool-runtime.md).

## Data without a separate CPE size quota

CPE adds no explicit byte-size cap for prompt text, incoming prompt images,
files a script reads or writes, media attached with `view`, overflow files,
compaction summaries, or session checkpoints. It also adds no fixed count cap for
saved sessions. These still use host memory and storage, and data sent to a
model is subject to the provider's limits.

A compaction summary must contain non-whitespace text. Having no byte quota does
not make arbitrarily large data practical. See
[Persistence and recovery](persistence.md).
