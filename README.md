# CPE

CPE is a coding agent served over the Agent Client Protocol (ACP), built with
[gai](https://github.com/spachava753/gai) and
[StarlarkX](https://github.com/spachava753/starlarkx). Models use two tools:
`bash` for commands, and `starlark_exec` for exact file edits, MCP tool
orchestration, and viewing media.

## Understanding the agent

The [behavior guides](docs/README.md) explain sessions and turns, persistence,
compaction, tool execution, extensions, instructions, models, credentials, usage,
and recovery. They include diagrams and examples of what happens during normal
work and interruptions. [Limits and defaults](docs/limits.md) lists the exact
output budgets, timeouts, retry counts, and configurable limits.

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
count limit. Prompt text and input images have no CPE-imposed size cap; provider
limits and tool-output bounds still apply.
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
Compaction guidance, project instructions, skill metadata, and the list of
plugins and MCP servers with their tools are supplied separately with the system
instructions, even with a custom base prompt.

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

The model has two tools. `bash` runs a bash script in the session directory and
returns its output and exit status. `starlark_exec` runs a StarlarkX script, a
Python dialect without classes, exceptions, or a standard library, with these
builtins:

- `read_file`, `write_file`, and `edit_file` for files; `edit_file` replaces
  exactly one literal occurrence and raises otherwise.
- `bash(script)` to run a command from a script.
- `view(data, mime_type)` to show the model an image, audio, video, or PDF.
- `tools.<name>(...)`, `search_tools`, `describe_tool`, and `describe_server`
  for MCP tools, and `parallel` to run several calls at once.
- `compact(summary)` for compaction.

Each call starts fresh; scripts keep data across calls by writing files. Both
tools accept an optional `timeout` in seconds. Each tool result shows at most the
last 2,000 lines or 50 KiB of output; longer output is saved to a temporary file
that the result names. [Tool execution](docs/tool-runtime.md) explains the
builtins, errors, and output in detail.

Installing and running CPE grants the agent full access to the machine with the
user's privileges, including configured plugins and MCP servers. Tools execute
directly: there are no approval prompts, allowlists, or sandbox boundaries. The
session working directory is a path base, not an access restriction.

### MCP servers

Configure MCP servers in `~/.cpe/settings.json`. A server with a `command` uses
stdio; a server with a `url` uses streamable HTTP, or SSE with `"type": "sse"`:

```json
{
  "mcpServers": {
    "docs": {
      "command": "docs-mcp",
      "args": ["--readonly"],
      "env": {"DOCS_ROOT": "/srv/docs"},
      "description": "Internal documentation search"
    },
    "tracker": {
      "url": "https://example.com/mcp",
      "headers": {"Authorization": "Bearer <token>"},
      "enabled": false
    }
  }
}
```

`description` replaces what the server says about itself in the system prompt,
and `"enabled": false` removes a server from sessions. A stdio server's `cwd`
must be absolute. Unknown fields are errors. CPE reads the file at startup.

Installed plugins and the ACP client can also supply servers. A client server
whose name matches a configured or plugin server fails the session with an
error naming both. Each session connects to every server when it opens; a server
that fails to connect within `--mcp-timeout` (1 minute by default) is skipped
and reported on stderr. Sessions in one process share connections to
identically configured servers.

Use these commands to check servers outside a session:

```sh
cpe mcp list               # connect to every server; report status and tools
cpe mcp show docs          # one server's info, instructions, and tools
cpe mcp show docs --schemas
```

Both resolve the servers a session started in the current directory would use,
from settings and installed plugins. Output is a table on a terminal and JSON
otherwise; pass `--json` to force JSON. `list` exits nonzero if any enabled
server failed, and `show` prints a failing stdio server's stderr. See
[MCP, plugins, and skills](docs/extensions.md) for naming, discovery, and results.

### Skills and plugins

Skills and plugins are discovered from `~/.agents/{skills,plugins}` and each
session's `.agents/{skills,plugins}`. `--skills` and `--plugins` add roots, resolving
relative flag paths against the launch directory. Project skills override global
skills with the same name; duplicate plugin names are errors. Nonfatal plugin
diagnostics are written to stderr, not ACP updates or conversation history,
rather than blocking session creation;
malformed manifests and unsupported manifest schemas remain fatal.

The model receives skill names, descriptions, and file paths, not their bodies,
and reads relevant skill files with bash or `read_file`. Plugins are listed with
their skills and MCP servers. Skills also appear as slash commands, with plugin
skills named `/plugin:skill`; a recognized command supplies the skill file
reference and the user's arguments. Unknown slash commands and absolute paths
are passed through as ordinary prompt text. Plugin MCP configuration supports
`${PLUGIN_ROOT}`; `${PLUGIN_DATA}` is not supported yet.

### Compaction

Use `/compact` to ask the model to summarize the conversation now. The model
calls `compact(summary)` inside `starlark_exec`, using string construction and
scripting to build the summary. The builtin takes one nonempty string and
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
other tool calls alongside the script requesting compaction. The entire
response and its tool results are dropped from working context, including output
produced after
`compact` runs. Any information needed from those results must be captured in
the summary by the script. Subsequent messages accumulate after that summary.
The summary is checkpointed before the next model request, or after outstanding
calls are settled at turn end if the turn stops. A later script error does not
undo an accepted summary. Loading, resuming, and forking retain
the compacted working context; ACP transcript replay still shows the full history.
Compaction does not touch files or rerun tools.

When the latest successful generation's reported input plus output tokens reach
85% of the selected model's required `limit`, CPE prepends a warning to each tool
result asking the model to finish the current task and call `compact(summary)`.
Cached input still counts toward the limit. The warning requires both token
counts; tool-result size is not estimated. It is advisory: CPE does not stop the
turn, compact automatically, or make a separate summarization request. The model
writes the summary using the context already available to it.

### Saved sessions

JSON checkpoints under `~/.cpe/sessions` preserve the conversation and selected
model settings. `--sessions` selects another absolute directory. Files are written
atomically with private permissions on Unix; they can contain prompts, tool
output, images, and provider reasoning metadata, so treat them as sensitive.

ACP clients can list, load, resume, fork, close, and delete sessions. Closing keeps
the checkpoint; deleting removes it. Loading, resuming, or forking reconnects MCP
servers and lists their tools again. Checkpoints are not VM snapshots, side
effects are never replayed, and this format is not ATIF.
On Windows, protect the checkpoint directory with filesystem ACLs.

Incomplete model responses are not saved or supplied to future model requests.
In a live turn, CPE sends an assistant-message update explaining the interruption
and whether it will retry. Clients with session-notice support also receive an
advisory notice. Neither update is saved or replayed when a session is loaded.
Restoring a saved session never automatically restarts an aborted response.

### Prompt caching

CPE uses the saved conversation ID as a stable `prompt_cache_key` for Responses
and Chat Completions adapters that support it. Turns, tool rounds, retries,
loading, and resuming reuse that key; forks and new sessions use their own IDs.
The key helps route requests to cached prefixes; it does not itself enable caching.

Native Anthropic requests use GAI's system and multi-turn caching helpers to add
`ephemeral` cache controls to the system instructions and latest conversation
content block. Parallel tool results are ordered consistently so unchanged
history remains a stable cache prefix. CPE leaves retention at provider defaults
and does not select OpenAI's explicit breakpoint mode. Cache hits still depend
on matching prompt prefixes and provider requirements. Provider-reported cache
reads and writes are included in usage reporting.

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

## Skills

Install skills from a Git repository and update them from their recorded source:

```sh
cpe skills install mattpocock/skills --skill writing-great-skills --scope project
cpe skills update writing-great-skills --scope project
cpe skills update
cpe skills list
cpe skills remove writing-great-skills --scope project
```

Project skills live in `./.agents/skills` and are tracked in `./skills-lock.json`
(version 1). Global skills live in `~/.agents/skills`; their lock is
`$XDG_STATE_HOME/skills/.skill-lock.json` when `XDG_STATE_HOME` is set, otherwise
`~/.agents/.skill-lock.json` (version 3). These are the lock formats used by
[the skills CLI](https://github.com/vercel-labs/skills). Project records use its
folder-content SHA-256 algorithm; global GitHub records use Git tree hashes.
With `--dir`, CPE uses that exact skill directory and keeps a `skills-lock.json`
inside it, separate from the default project inventory.

`update` without names updates both scopes; `--scope` restricts it to one.
A named skill installed in both scopes is skipped with a warning unless a scope
is explicit. Missing and untracked skills are also warned about and skipped.
CPE updates GitHub and Git sources at their default checkout; records using other
source types or explicit refs are skipped. Updates replace local edits and remove
obsolete files, just like installing with `--override`.

Listing includes untracked skills. Removal also works for untracked skills and
removes the lock record when one exists. A malformed or unsupported lock file
blocks changes to its inventory rather than being discarded. A lock changed by
another writer during acquisition causes the operation to abort; retry to use its
new contents. Bundle changes and lock changes are published together with
rollback on failure; an error reports
any committed results or recovery files left by failed cleanup or rollback.
Results are JSON arrays when stdout is redirected; `--json` forces JSON on a
terminal. Warnings go to stderr.

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
