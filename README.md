# CPE

CPE is a coding agent served over the Agent Client Protocol (ACP), built with
[gai](https://github.com/spachava753/gai),
[StarlarkX](https://github.com/spachava753/starlarkx), and
[Dyson](https://github.com/spachava753/dyson). Models use one `starlark` tool for
commands, filesystem and HTTP operations, and MCP orchestration.

## ACP server

Configure a JSON array of models in `~/.cpe/models.json`, then configure your ACP
client to launch:

```sh
cpe acp
```

For example, an OpenAI-compatible endpoint can use:

```json
[
  {
    "id": "my-model",
    "api": "Openai",
    "base_url": "https://your-provider.example/v1/",
    "api_key": "YOUR_API_KEY",
    "input_modalities": ["text", "image"],
    "output_modalities": ["text"],
    "limit": 128000,
    "max_tokens": 8192,
    "reasoning_levels": ["low", "medium", "high"]
  }
]
```

Use the provider's actual model ID, limits, modalities, and reasoning levels.
Model IDs must be unique in the file. API names are case-sensitive: `Responses`,
`Anthropic`, `Gemini`, `Openai`, `Zai`, `Moonshot`, `Deepseek`, `Cerebras`, and
`OpenRouter`. Protect `models.json` like a credential file when it contains keys.
Some OpenAI-compatible adapters require a nonempty key even for local endpoints;
configure the placeholder accepted by your local server explicitly.

New sessions default to the last model selected through ACP or used for a prompt,
remembered across launches in `~/.cpe/last-model.json`. If no model has been
remembered, or it is no longer configured, the first entry in `models.json` is
used. Creating or restoring a session alone does not change this preference.
Loaded, resumed, and forked sessions keep their saved model and reasoning
selection; the global default never overrides them. A saved model that is no
longer configured produces an error rather than silently switching models.

`--models` selects another model file; `--reasoning` sets the initial effort for
new sessions, otherwise the provider default is used. Clients can switch model
and reasoning through ACP session configuration options between turns. A model
switch keeps the conversation and live session state, but resets reasoning to the new provider's default. Foreign-model reasoning
metadata is omitted from requests, without deleting it from saved history.
A failure to remember the last-used model is reported on stderr without undoing
a successful session selection or stopping a prompt. An unreadable or malformed
preference file fails new-session creation, but does not block restoration.

Each process handles one ACP connection with independent sessions. A session
accepts one active prompt at a time; other sessions remain usable. `session/cancel`
stops that turn, while disconnecting closes all live runtimes and MCP connections.
Session setup has a ten-minute deadline by default, configurable with
`--setup-timeout`. Prompt turns have no CPE-imposed deadline or model-generation
count limit. Prompt text, input images, and command stdin have no CPE-imposed
size cap; provider limits and tool-output bounds still apply.
Interrupted/transient model responses can retry twice by default
(`--response-retries`; zero disables retries). Retries respect provider retry
delays. Cancellation and permanent provider errors do not retry.

### System prompt

CPE uses its built-in system prompt unless `~/.cpe/SYSTEM.md` exists. That file
replaces the built-in base prompt, including its host-tool guidance.
`~/.cpe/APPEND-SYSTEM.md` adds instructions after the selected prompt: the custom
prompt when present, otherwise the built-in prompt. Use the uppercase filenames
shown here; `APPEND-SYSTEM.md` uses a hyphen.

Files are read when `cpe acp` starts; restart the process after editing them.
Missing files are optional; other read errors stop startup. An empty `SYSTEM.md`
selects an empty base prompt, while an empty `APPEND-SYSTEM.md` adds nothing.
Compaction guidance, project instructions, skill metadata, and MCP namespace
summaries are supplied separately with the system instructions, even with a
custom base prompt.

The ACP session's working directory is the project root. CPE includes that
directory's `AGENTS.md` contents in the system prompt and recursively lists the
paths of nested `AGENTS.md` files without reading their bodies. The model is
instructed to read applicable nested files before working in those directories,
from outermost to innermost; more specific instructions take precedence.

Project instructions and their index are loaded when a session is created,
loaded, resumed, or forked. They remain unchanged across turns in that live
session. Discovery stays beneath the session working directory: it does not
search ancestor directories or infer a Git root. It skips `.git` directories
and does not traverse directory symlinks. Regular files and file symlinks named
`AGENTS.md` are included, including those in hidden or Git-ignored directories.
Missing instruction files are optional; read or directory-scan errors fail
session setup rather than silently dropping instructions.

### Host tools

Each `starlark` call runs in a fresh Dyson sphere. Variables, functions, and
imports last only for that call, and open files are closed when it finishes.
Dyson provides filesystem, HTTP, subprocess, and other standard-library modules,
for example `load("subprocess.star", "subprocess")`. Installing and running CPE
grants the agent full access to the machine with the user's privileges, including
configured plugins and MCP servers. Tools execute directly: there are no approval prompts,
allowlists, or sandbox boundaries. The session working directory is a path base,
not an access restriction. Dyson environment and process APIs also have host
access; process-global mutations affect the shared CLI process.

Run commands through Dyson's subprocess module. Shell syntax is available by
invoking a shell explicitly:

```python
load("subprocess.star", "subprocess")
result = subprocess.run(["bash", "--noprofile", "--norc", "-c", "pwd && ls"], capture_output=True)
print(result.stdout.decode())
print(result.stderr.decode())
print(result.returncode)
```

Inspect `returncode`, or pass `check=True` to make a nonzero exit fail the script.
Subprocess output stays inside the script until explicitly printed.

Use the `edit(filename, old, new)` builtin for exact text replacement in an
existing file. It requires exactly one literal match, including overlapping
matches; missing or ambiguous matches raise an error without changing the file.
`old` must be nonempty. `new` may be empty to delete the match. A replacement that
would leave the contents unchanged also raises an error. Whitespace and line
endings are matched as supplied, without normalization.

```python
edit("app.go", "old function", "new function")
edit("app.go", "old caller", "new caller")
edit("app_test.go", "old expectation", "new expectation")
```

Each successful edit automatically emits a confirmation and unified diff in the
tool result; no `print()` or return-value inspection is needed. The builtin returns
`None`. Calls execute sequentially against the current contents, including earlier
edits. An error stops the script: earlier edits and their confirmations remain,
and later calls do not run. There is no batch transaction or rollback.

Relative paths use the session working directory; absolute paths and parent
traversal are also supported. Symlinks are followed and retained. Replacement uses
a temporary file in the target directory and an atomic rename, preserving
permission bits, but not ownership, ACLs, or other extended metadata. A
best-effort content check detects changes during the edit, not changes since the
model's earlier read. Other writers are not locked out and can still write between
the final check and rename. Dyson's `open()` is read-only; `edit` does not create
missing files.

Use the session-owned `state` mapping to retain data explicitly:

```python
state["results"] = [{"name": "example", "count": 1}]

# In a later call, read the frozen value or assign a replacement.
print(state["results"])
state["results"] = list(state["results"]) + [{"name": "next", "count": 2}]
```

Keys must be strings. Values may be `None`, booleans, integers, floats, strings,
bytes, and acyclic lists, tuples, or dictionaries containing those types.
Assignment recursively freezes the value, including existing aliases; stored
collections cannot be mutated in place. Functions, modules, resource handles,
and MCP result wrappers are rejected. Store an MCP result's `structured_content`
or a content item's `raw` data instead. Indexing, assignment, membership tests,
iteration over keys, and `len(state)` are supported. Missing keys raise an error.
Assignments accepted before a script fails remain stored.

State persists across calls, turns, model switches, and compaction in a live
session. It is not checkpointed. Loading, resuming, or forking a conversation
starts with an empty store. When there is restored history, the model receives a
notice before new work: earlier stored data is unavailable, but files and external
effects may still exist. Reconstruct needed state without blindly repeating
earlier actions.

This notice is model-only, not part of saved history or ACP transcript replay.
It follows any restored compaction summary. A new compaction covering the reset
replaces the notice along with the earlier context.

Cancellation, process cleanup, and output capture manage execution lifetime and
protect the ACP protocol stream; they do not restrict machine access.
Starlark subprocess calls capture bounded output and never inherit ACP
stdin/stdout. Commands belong to the invocation, not a background-job
service; Unix process groups are cleaned up on completion or cancellation. Other
platforms terminate the direct child only. Failed or canceled tools are recorded
without automatic replay: a failure can still have changed files or external state.

MCP uses the official Go SDK and accepts client-supplied or plugin-defined stdio,
streamable HTTP, and SSE servers. Every discovered plugin MCP server is enabled,
along with servers supplied by the ACP client. CPE connects and discovers their
tools during session setup; a connection or discovery failure fails setup and
closes connections already opened. There is no model-facing loading step.

Tools are exposed as Starlark builtins named `mcp__<server>__<tool>`. Plugin server
IDs include the plugin name, so `screenshots:server` becomes
`mcp__screenshots_server`. Invalid identifier characters become underscores;
colliding names receive deterministic hash suffixes. Use the names returned by
discovery rather than constructing them.

The effective system prompt includes namespace names and one-line descriptions
alongside skills, including when a custom system prompt is used. A namespace
uses its plugin description when present, otherwise the first line of the MCP
server's initialization instructions. Full server instructions and tool schemas
remain available through discovery:

- `search_tools(query, limit=8, namespace="")` ranks tool metadata with local BM25
  keyword search. It searches names, descriptions, input-schema property names
  and descriptions, and namespace descriptions/instructions—not remote data.
- `describe_tool(name)` returns the description, namespace, callable signature,
  and input schema.
- `describe_namespace(name)` returns the namespace description, full server
  instructions, and tool names.

Search returns a list of names and descriptions; an unmatched query returns an
empty list. Unknown names passed to either description function return `None`.
Discovery functions do not execute remote tools. For example, after discovering
a screenshot tool:

```python
print(describe_tool("mcp__screenshots_server__capture"))
result = mcp__screenshots_server__capture({})
print(result.is_error)
# Inspect structured_content and content before choosing what the model sees.
image(result.content[0].data)
```

Connections and cached tool lists belong to the live session. An MCP
`notifications/tools/list_changed` notification invalidates that server's cache.
Before the next Starlark invocation, CPE refreshes changed lists and builds a
catalog shared by discovery and callable builtins for the entire invocation.
Notifications received during execution take effect on the next call. A refresh
failure fails the invocation and is retried on the next call; it does not silently
use stale tools. A remote tool removed during execution may itself fail.
Connections and cached lists are released when the session closes. Loading,
resuming, or forking a saved session creates fresh connections and a fresh
catalog; historical tool calls are never replayed.
Installing or uninstalling a plugin determines whether its servers are discovered
for new sessions. Skill-only plugins require no MCP connections.

Results retain content blocks, structured content, metadata, and `is_error` for
scripts. Only `print(...)` and `image(data)` expose output to the model;
intermediate tool results do not become separate conversation messages. Use
`print(item.text)` for an MCP text item and `image(item.data)` for an image item.

`image(data)` accepts raw Starlark bytes containing a PNG, JPEG, GIF, or WebP image.
It detects the media type from the bytes and enforces the output size limit. It
does not accept paths, URLs, or base64 strings and performs no file or network
access itself. Use Dyson to obtain bytes, or retain them in `state` for a later
call. For example:

```python
image(open("screenshot.png", "rb").read())
```

Text and image output retain their order, including output produced before a
script fails. Audio and other binary resources remain inspectable through MCP
result fields but cannot be displayed as images. Resource links are not
automatically fetched. Models without image input receive a text notice instead
of the image.

Skills and plugins are discovered from `~/.agents/{skills,plugins}` and each
session's `.agents/{skills,plugins}`. `--skills` and `--plugins` add roots, resolving
relative flag paths against the launch directory. Project skills override global
skills with the same name; duplicate plugin names are errors. Nonfatal plugin
diagnostics are written to stderr, not ACP updates or conversation history,
rather than blocking session creation;
malformed manifests and unsupported manifest schemas remain fatal.

The model receives skill names, descriptions, and file paths, not their bodies.
It reads relevant skill files and bundled resources using bash or Starlark/Dyson.
Skills also appear as slash commands, with plugin skills named `/plugin:skill`;
a recognized command supplies the skill file reference and the user's arguments.
Unknown slash commands and absolute paths are passed through as ordinary prompt
text. Plugin MCP configuration supports `${PLUGIN_ROOT}`; `${PLUGIN_DATA}` is not
supported yet.

### Compaction

Use `/compact` to ask the model to summarize the conversation now. The model
calls `compact(summary)` inside `starlark`, using Starlark string construction
and scripting to build the summary. The builtin takes one nonempty string and
returns `None`; the last accepted call supplies the summary if called repeatedly.
The model can also compact during a task. `/compact` is reserved and takes
precedence over a skill with that name.

Summary-writing guidance is included in the system instructions from the first
request, rather than injected as a user message. The default guidance covers
the current goal, constraints, completed work, decisions, unresolved questions,
and next steps. `~/.cpe/COMPACTION.md` replaces this guidance independently of
`SYSTEM.md` and `APPEND-SYSTEM.md`. It is read at startup; an empty file removes
the default guidance, and read errors other than a missing file stop startup.

Compaction saves the summary without deleting the full transcript. The next model
request contains only the system instructions and summary. All tool calls in
the current response finish normally before compaction takes effect, including
other Starlark calls alongside the script requesting compaction. The entire
response and its tool results are dropped from working context, including output
produced after
`compact` runs. Any information needed from those results must be captured in
the summary by the script. Subsequent messages accumulate after that summary.
The summary is checkpointed before the next model request, or after outstanding
calls are settled at turn end if the turn stops. A later script error does not
undo an accepted summary. Loading, resuming, and forking retain
the compacted working context; ACP transcript replay still shows the full history.
Compaction does not reset the live `state` store or rerun tools.

When the latest successful generation's reported input plus output tokens reach
85% of the selected model's required `limit`, CPE prepends a warning to each tool
result asking the model to finish the current task and call `compact(summary)`.
Cached input still counts toward the limit. The warning requires both token
counts; tool-result size is not estimated. It is advisory: CPE does not stop the
turn, compact automatically, or make a separate summarization request. The model
writes the summary using the context already available to it.

### Saved sessions

JSON checkpoints under `~/.cpe/sessions` preserve conversation and selected model
settings. `--sessions` selects another absolute directory. Files are written
atomically with private permissions on Unix; they can contain prompts, tool
output, images, and provider reasoning metadata, so treat them as sensitive.

ACP clients can list, load, resume, fork, close, and delete sessions. Closing keeps
the checkpoint; deleting removes it. Loading, resuming, or forking creates a fresh
state store and MCP connections: checkpoints are not VM snapshots, side
effects are never replayed, and this format is not ATIF. On Windows, protect the
checkpoint directory with filesystem ACLs.

Incomplete model responses are explicitly stored in `AbortedResponses`, separate
from accepted conversation. They are never supplied to the model or replayed to
an ACP client when a session is loaded, resumed, or forked. In a live turn, CPE
checkpoints the abort, sends an advisory notice (or a text update for clients
without notice support), and retries only the model response when eligible.
Completed tool effects are not rerun. A checkpoint failure stops retrying, and
restoring a saved session never automatically restarts an aborted response.

### Usage reporting

ACP `usage_update` notifications report the latest successful generation's input
plus output tokens as `used`, and the selected model's context limit as `size`.
These are context measurements, not cumulative token consumption. Cached input
still occupies context. Updates require both token counts and a known window
size; CPE does not invent counts at startup or restoration. Tool results,
compaction, and model changes are reflected in the next measured generation,
not estimated or counted through extra provider requests.

`PromptResponse.usage` reports cumulative provider-reported consumption across
all turns and generation attempts in the saved conversation, including retries
and cancelled attempts when their usage is available. Totals survive load,
resume, and fork along with the conversation; new sessions start without counts.
Input and output totals are added once; cache reads/writes and reasoning are
breakdowns, not additional tokens in `totalTokens`. Missing categories stay
unreported, and the usage object is omitted until both input and output counts
have been reported. Missing provider measurements cannot be reconstructed.

This follows the individual cumulative field definitions in the ACP SDK v0.5.0
schema. The [end-turn usage proposal](https://agentclientprotocol.com/rfds/end-turn-token-usage)
is still unstable and has conflicting per-turn wording. The stable
[context and cost contract](https://agentclientprotocol.com/rfds/session-usage)
is separate. CPE omits optional cost because it does not have reliable billed-cost
data; configured list prices are not treated as actual charges.

Text and inline-image prompts are supported. Additional working directories,
ACP-tunneled MCP, session modes, audio prompts, and automatic compaction
are not implemented. Unsupported operations return explicit errors.

## Authentication

Sign in to a ChatGPT subscription or save an [OpenCode Go](https://opencode.ai/docs/go)
API key:

```sh
cpe auth login openai
cpe auth login opencode
cpe auth logout openai
cpe auth logout opencode
```

OpenAI login prints a browser link and waits for a callback on `127.0.0.1:1455`.
For SSH or a browser on another machine, use `cpe auth login openai --device`
and enter the displayed code at OpenAI's sign-in page. Device-code authorization
may need to be enabled in your ChatGPT security settings. Login waits up to
15 minutes; Ctrl+C cancels it.

OpenCode login uses a masked terminal form from the imported `charm.land/huh/v2`
module. Paste the key from the OpenCode console after subscribing to Go.
The key is checked locally; login does not verify subscription status or spend credits.

CPE stores credentials in one plaintext `auth.json` in its configuration directory
(`~/.cpe/auth.json` by default), with an entry for each provider. The file uses
mode `0600` on Unix; newly created configuration directories use `0700`.
On Windows, protect the directory with your user account's filesystem ACLs.
Treat `auth.json` as a password. Login replaces only the selected provider's entry;
logout removes only that entry. Other providers are preserved, including during
concurrent commands, and failed or canceled updates leave credentials unchanged.
Logout leaves remote sessions and provider-issued API keys active.

A `Responses` model at `https://chatgpt.com/backend-api/codex/` can omit `api_key`
to use the stored ChatGPT subscription; models at the exact HTTPS host
`opencode.ai` can use the stored OpenCode key. Explicit model keys take precedence.
Stored subscription credentials are never attached to other hosts or plain HTTP.
ACP authentication verifies stored credentials without opening a login UI.

ChatGPT credentials are checked before every provider request, including streamed
requests and subsequent requests within a turn. When the access token is within
five minutes of expiry, CPE starts a background refresh. Requests continue using
the unexpired token while refresh is pending or failing, and use newly saved
credentials once refresh succeeds. API keys, including OpenCode keys, are not
refreshed.

Refresh attempts have a 30-second timeout and retry with exponential backoff from
one to 30 seconds until success or the existing access token expires. Retries
continue independently of individual turns, but stop on ACP logout or connection
shutdown. Once the exchange succeeds, CPE retains the rotated tokens and retries
saving them atomically, even beyond the old access token's expiry. It does not
exchange the old refresh token again. Saving stops on success, replacement by
login/logout, or shutdown. Other providers' credentials are preserved.

Expired tokens are never sent to the model provider. A request finding expired
credentials waits for renewal and persistence, subject to the request deadline.
Already-expired credentials get one token exchange attempt with a 30-second
timeout; successful rotation then follows the same save-only retry policy.
If credentials cannot be renewed and saved, the request fails. Revoked refresh credentials require `cpe auth login openai` again.

## Agent Plugins

CPE installs native [Agent Plugins](https://agent-plugins.org/specification)
packages from Git repositories. For example, install Stripe's plugin into the
current project:

```sh
cpe plugins install stripe/ai --path providers/agent-plugins/plugin --scope project
cpe plugins list --scope project
cpe plugins remove stripe --scope project
```

Use `--scope global` for `~/.agents/plugins`. Project scope defaults to
`./.agents/plugins`; `--dir` selects an exact custom directory instead.
`--plugin NAME` selects by manifest name, while `--path` selects an exact
source-relative package directory. `--override` replaces an existing bundle,
including removal of obsolete files.

Installation preserves skills, MCP configuration, and bundled resources without
running code, connecting to MCP servers, or signing into Stripe. Component
problems are reported as warnings without discarding the bundle. Results are
JSON arrays when stdout is redirected; `--json` also forces JSON on a terminal.

Verify changes with `go test ./...` and `go run ./build lint`.
