# Limits and defaults

[All guides](README.md)

This page collects the limits you can encounter while using CPE, their current
values, and what happens when you reach them. Where a setting can change a
value, it is listed alongside it. The other values are fixed in the current
release.

Sizes use binary units: **1 KiB = 1,024 bytes** and **1 MiB = 1,048,576 bytes**.
Byte limits apply to the data size; a character can occupy several bytes.

## Tool output

Output passes through several limits before it reaches the model. Capturing a
command's output and emitting it from a script are separate steps.

| Output | Limit | What happens at the limit |
| --- | --- | --- |
| Captured command stdout | 128 KiB (131,072 bytes) per command | Keeps the first 128 KiB, then appends a truncation notice if more was produced. |
| Captured command stderr | 128 KiB (131,072 bytes) per command | Same behavior as stdout. When stderr is merged into stdout, they share one 128 KiB capture budget. |
| One emitted text block | 128 KiB (131,072 bytes) | Keeps the first 128 KiB and appends a truncation notice. This includes printed text, edit confirmations and diffs, and execution-error text. |
| All emitted content in one Starlark invocation | 8 MiB (8,388,608 bytes) | An emission that would exceed the budget fails the invocation. Text and encoded images share this budget. |
| Emitted blocks in one Starlark invocation | 1,024 blocks | Attempting to emit a 1,025th block fails the invocation, even if the byte budget has room. |
| One image passed to `image(data)` | 6 MiB (6,291,456 raw bytes) | A larger image is rejected. Empty images are also rejected. |

The truncation notice is a newline followed by `[output truncated]`: **19 extra
bytes** beyond the retained text or command output. For emitted text, those
extra bytes count toward the invocation's 8 MiB budget.

Each `print(...)` call emits one text block. Each successful `image(...)` call
emits one image block, and each successful `edit(...)` emits one text block with
its confirmation and diff. Many small prints can reach the block ceiling before
reaching the byte ceiling.

Images count at their base64-encoded size: **4 bytes for every 3 raw bytes**,
rounding the encoded length up to a multiple of 4. A 6 MiB image occupies the
entire 8 MiB budget. It fits only if no other content consumes that budget. A
3 MiB image occupies 4 MiB, leaving 4 MiB for other output.

```text
 Command output
   stdout: keep 128 KiB     stderr: keep 128 KiB
              \              /
               v            v
                 Script data
                      |
           print / image / edit
                      |
       +--------------+----------------+
       |                               |
 text block: 128 KiB             image: 6 MiB raw
 + notice if truncated          counts after base64 encoding
       |                               |
       +--------------+----------------+
                      |
       shared invocation budget:
       8 MiB and 1,024 emitted blocks
                      |
                      v
               Model-visible output
```

A command continues running and its pipes continue to be drained after capture
fills. Truncation alone does not make the command fail. The discarded bytes are
unavailable to the script; printing the captured result in smaller pieces
cannot recover them. For large logs, have the command write a file and inspect
selected sections.

An invocation that exceeds the combined byte or block budget reports an output
limit error. The final retained block may be replaced with a failure notice.
Earlier file changes, successful stores, and remote effects remain. See
[Tool execution](tool-runtime.md) and [Failures and cancellation](failures.md).

These output budgets apply to emitted content. Data kept inside the script or
saved through `kv.store` does not consume them until emitted. The selected
provider's context and image limits still apply to what is sent to it.

## HTTP requests from scripts

Dyson's `requests` module has its own limits before a script prints any result.
These apply to script HTTP requests independently of model-provider requests.

| Operation | Limit or default | What to expect |
| --- | --- | --- |
| Buffered HTTP response body | 64 MiB (67,108,864 bytes) per response | A larger body fails the request. The limit also applies separately to bodies retained from redirects. |
| Redirect chain | At most 30 followed redirects | Attempting another redirect fails the request. `allow_redirects=False` disables following redirects. |
| Explicit request timeout | `timeout=None` by default | The script can supply a positive number of seconds for both connect and read timeouts, or a pair such as `timeout=(5, 30)`. |

The read timeout limits a wait for incoming data and is renewed on reads. A
response that keeps delivering data can take longer than that timeout in total.
With no explicit timeout, there is no overall request deadline supplied by the
module; network and transport failures can still end the request. Turn
cancellation also stops the request.

A response below 64 MiB may still be too large to print in full because of the
128 KiB text-block and 8 MiB invocation budgets above.

## Sessions, timeouts, and retries

| Operation | Limit or default | Configuration and behavior |
| --- | --- | --- |
| Active prompts in one session | 1 | A second prompt is rejected while the first is active. Different sessions can run concurrently. |
| Session setup | 10 minutes | Set `--setup-timeout`, for example `--setup-timeout=2m`. It must be positive. Expiry fails setup. |
| Prompt-turn duration | No CPE-imposed deadline | The client can cancel; provider and individual operation limits still apply. |
| Model generations in a turn | No CPE-imposed count limit | Tool use can continue across many model responses. |
| Retries of one eligible model response | 2 retries by default: up to 3 attempts including the first | Set `--response-retries`; `0` disables these retries. This budget starts afresh for each model response. |
| Model-response retry delay | 250 ms, 500 ms, 1 s, 2 s, 4 s, then 8 s for later retries | A longer provider-requested delay takes precedence. With the default retry count, only the first two delays are used. |
| Subprocess runtime | No timeout by default | A script can set `subprocess.run(..., timeout=30)` to use a timeout in seconds. Turn cancellation also stops the command. |
| Waiting for command exit or lingering output pipes after cancellation or observed process exit | 1 second | CPE terminates the child or closes remaining pipes when needed to finish waiting. This does not limit a normally running command to one second. |
| Cleanup after an aborted response, at turn end, or during connection shutdown | 10-second budget for each cleanup phase | Gives checkpointing and client notifications a chance to finish after cancellation. It is not a guarantee that the entire shutdown finishes within ten seconds; joining active work and resource shutdown can take longer. |
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
| `search_tools` results | 8 by default | Set a positive `limit` argument to request another maximum. There is no additional fixed result-count ceiling; available matches and emitted-output limits still apply. |
| Skill/plugin source search during installation | 5 directory levels below the source root by default | Deeper packages are skipped. `--full-depth` removes the depth limit; `--path` selects an exact package directory. |
| Plugin name | 1–64 ASCII characters | An invalid or longer name is rejected. |
| Skill name length | 64 Unicode characters | A longer name is retained with a length warning. |
| Skill description length | 1,024 Unicode characters | A longer description is retained with a length warning. |
| Skill compatibility field | 500 Unicode characters | A longer value makes the skill definition invalid. |
| Symlinks followed while resolving an `edit` target | 40 across the path | Following a 41st link fails the edit. |
| Matches for an exact `edit` | Exactly 1 | Zero or multiple matches, including overlaps, fail without replacing the file. |

The installation search depth applies to finding packages in a source tree.
Project `AGENTS.md` discovery has no fixed depth cap; it skips directory symlinks
and `.git` directories. Skill character counts use Unicode code points.

See [MCP, plugins, and skills](extensions.md),
[Instructions and context](instructions.md), and [Tool execution](tool-runtime.md).

## Data without a separate CPE size quota

CPE adds no explicit byte-size cap for prompt text, incoming prompt images,
command stdin, compaction summaries, session checkpoints, or durable KV values.
It also adds no fixed count cap for KV keys or saved sessions. These still use
host memory and storage, and data sent to a model is subject to the provider's
limits. Incoming prompt images and images emitted by tools have different
limits: the **6 MiB** image limit above applies to `image(data)` output.

A compaction summary must contain non-whitespace text. KV values must be
supported data without cycles. Having no byte quota does not remove those
requirements or make arbitrarily large data practical. See
[Persistence and recovery](persistence.md).
