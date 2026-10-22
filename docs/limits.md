# Limits and defaults

[All guides](README.md)

This page collects the limits you can encounter while using CPE, their current
values, and what happens when you reach them. Where a setting can change a
value, it is listed alongside it. The other values are fixed in the current
release.

Sizes use binary units: **1 KiB = 1,024 bytes** and **1 MiB = 1,048,576 bytes**.
Byte limits apply to the data size; a character can occupy several bytes.

## Tool output

CPE keeps captured command streams complete for the script to inspect. Emitted
text is collected through the end of the invocation, then shown to the model as
one text block. Long text is saved in full, with a preview and retrieval key in
the tool result.

| Output | Limit or threshold | What happens |
| --- | --- | --- |
| Captured command stdout and stderr | No CPE byte cap | Complete captured streams are returned to the script. They remain script-local unless printed or explicitly stored. |
| Combined emitted script text, including execution diagnostics | 128 KiB (131,072 bytes) for the model-facing text block | At or below this size, all text is included. Above it, the complete text is saved in session KV and the model receives a head preview with the retrieval key. |
| Preview and retrieval notice together | At most 128 KiB (131,072 bytes) | The notice fits inside the preview budget; it reduces the space available for the beginning of the log. |
| Total emitted text and image bytes | No combined CPE byte cap | Text follows the preview behavior above; each image is checked separately. Host memory and provider limits still apply. |
| Number of prints or emitted images | No CPE count cap | Prints combine into one text block. Images follow in separate blocks. |
| One image passed to `image(data)` | 6 MiB (6,291,456 raw bytes) | A larger image is rejected. Empty images are also rejected. |

Printing a large amount of text leaves later script statements free to run.
When execution finishes, text over 128 KiB is stored as a string under
`overflow_<sha256>`. The key is based on the complete text, so identical output
reuses the same key. The result supplies the exact key to pass to `kv.get`.

```text
 Complete command stdout and stderr
                  |
                  v
          Script inspects data
                  |
          print / edit / diagnostics
                  |
                  v
      Combine text when invocation ends
                  |
         +--------+----------+
         |                   |
     <= 128 KiB          > 128 KiB
         |                   |
         |            Save full text in KV
         |                   |
         v                   v
    Full text block     Head preview + key
                         together <= 128 KiB
         |                   |
         +--------+----------+
                  |
                  v
       Image blocks follow in emission order
       each image: at most 6 MiB of raw bytes
```

The saved text includes emitted output and execution diagnostics from failed
or canceled scripts. It survives compaction and session restoration, using the
same persistence as other KV values. A later script can retrieve it, search for
an error, or print a small slice. Printing the whole saved log again produces
another preview if it still exceeds 128 KiB.

If saving the complete text fails, the result contains a head preview and a
save-failure notice, and the tool is marked failed. It provides no retrieval
claim for unsaved output. Saving happens after execution, so earlier script
work and effects remain. A process crash before the save can lose emitted text;
use an explicit `kv.store` during execution when a value needs an earlier
checkpoint.

Images are supplied separately from the text preview. A 6 MiB image does not
reduce the text preview's allowance or prevent another valid image from being
emitted. All text precedes the images, even if a script interleaved `print` and
`image` calls. Images retain their relative emission order.

See [Tool execution](tool-runtime.md#long-output-and-retrieval) for retrieving
logs and [Persistence and recovery](persistence.md#saved-script-output) for their
lifetime. The selected provider's context and image limits still apply.

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

Printing a response that takes the combined text over 128 KiB produces a preview
and saves the complete emitted text in KV. The 64 MiB HTTP response-body limit
applies earlier, while receiving the response.

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
| `search_tools` results | 8 by default | Set a positive `limit` argument to request another maximum. There is no additional fixed result-count ceiling. Available matches determine the result count; large printed results use the text-preview behavior above. |
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
command stdin, captured command stdout and stderr, complete emitted script text,
compaction summaries, session checkpoints, or durable KV values. It also adds no
fixed count cap for KV keys or saved sessions. These still use host memory and
storage, and data sent to a model is subject to the provider's
limits. Incoming prompt images and images emitted by tools have different
limits: the **6 MiB** image limit above applies to `image(data)` output.

A compaction summary must contain non-whitespace text. KV values must be
supported data without cycles. Having no byte quota does not remove those
requirements or make arbitrarily large data practical. See
[Persistence and recovery](persistence.md).
