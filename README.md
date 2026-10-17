# CPE

CPE is a coding agent served over the Agent Client Protocol (ACP), built with
[gai](https://github.com/spachava753/gai),
[StarlarkX](https://github.com/spachava753/starlarkx), and
[Dyson](https://github.com/spachava753/dyson). Models can use a persistent
`starlark_repl` for structured orchestration or `bash` for direct commands.

## ACP server

Configure a JSON array of models in `~/.cpe/models.json`, then configure your ACP
client to launch:

```sh
cpe acp --model my-model
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

`--models` selects another model file; `--reasoning` sets the initial effort.
Clients can switch model and reasoning through ACP session configuration options
between turns. A model switch keeps the conversation and live Starlark globals,
but resets reasoning to the new provider's default. Foreign-model reasoning
metadata is omitted from requests, without deleting it from saved history.

Each process handles one ACP connection with independent sessions. A session
accepts one active prompt at a time; other sessions remain usable. `session/cancel`
stops that turn, while disconnecting closes all live runtimes and MCP connections.
The defaults are a ten-minute deadline per session setup or prompt turn and
32 completed model generations per turn, configurable with `--turn-timeout` and
`--max-generations`. Interrupted/transient model responses can retry twice by
default (`--response-retries`; zero disables retries). Retries count separately
from completed generations, respect provider retry delays, and remain within the
turn deadline. Explicit cancellation, expired turn deadlines, and permanent
provider errors do not retry.

### System prompt

CPE uses its built-in system prompt unless `~/.cpe/SYSTEM.md` exists. That file
replaces the built-in base prompt, including its host-tool guidance.
`~/.cpe/APPEND-SYSTEM.md` adds instructions after the selected prompt: the custom
prompt when present, otherwise the built-in prompt. Use the uppercase filenames
shown here; `APPEND-SYSTEM.md` uses a hyphen.

Files are read when `cpe acp` starts; restart the process after editing them.
Missing files are optional; other read errors stop startup. An empty `SYSTEM.md`
selects an empty base prompt, while an empty `APPEND-SYSTEM.md` adds nothing.
Compaction guidance, skill metadata, and MCP namespace summaries are supplied
separately with the system instructions, even with a custom base prompt.
`AGENTS.md` loading is not implemented yet.

### Host tools

Starlark globals persist across turns in a live session. Dyson provides filesystem,
HTTP, subprocess, and other standard-library modules, for example
`load("subprocess.star", "subprocess")`. Installing and running CPE grants the
agent full access to the machine with the user's privileges, including configured
plugins and MCP servers. Tools execute directly: there are no approval prompts,
allowlists, or sandbox boundaries. The session working directory is a path base,
not an access restriction. Dyson environment and process APIs also have host
access; process-global mutations affect the shared CLI process.

Cancellation, process cleanup, and output capture manage execution lifetime and
protect the ACP protocol stream; they do not restrict machine access.
Both direct bash and Starlark subprocess calls capture bounded output and never
inherit ACP stdin/stdout. Commands belong to the invocation, not a background-job
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
emit(result.content[0])
```

Connections and the discovered catalog are reused for the live session and
released when it closes. Loading, resuming, or forking a saved session creates
fresh connections and a fresh catalog; historical tool calls are never replayed.
Installing or uninstalling a plugin determines whether its servers are discovered
for new sessions. Skill-only plugins require no MCP connections.

Results retain content blocks, structured content, metadata, and `is_error` for
scripts. Only `print` and `emit` expose output to the model; intermediate tool
results do not become separate conversation messages. `emit` supports text and
PNG/JPEG/GIF/WebP images. Audio and other binary resources remain inspectable in
the result and emit an omission notice; resource links are not automatically
fetched. Models without image input receive a text notice instead of the image.

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
calls the direct `compact` tool with one required string field, `summary`;
it can also choose to call the tool during a task. `/compact` is reserved and
takes precedence over a skill with that name.

Summary-writing guidance is included in the system instructions from the first
request, rather than injected as a user message. The default guidance covers
the current goal, constraints, completed work, decisions, unresolved questions,
and next steps. `~/.cpe/COMPACTION.md` replaces this guidance independently of
`SYSTEM.md` and `APPEND-SYSTEM.md`. It is read at startup; an empty file removes
the default guidance, and read errors other than a missing file stop startup.

Compaction saves the summary without deleting the full transcript. Later model
requests use the summary plus the entire assistant/tool batch containing the
compact call and all subsequent messages. Keeping that batch intact preserves
tool-call/result pairing. The summary is checkpointed before the next model
request, or at turn end if the turn stops. Loading, resuming, and forking retain
the compacted working context; ACP transcript replay still shows the full history.
Compaction does not reset live Starlark globals or rerun tools.

There is no automatic token-budget trigger or separate summarization request.
The model writes the summary using the context already available to it.

### Saved sessions

JSON checkpoints under `~/.cpe/sessions` preserve conversation and selected model
settings. `--sessions` selects another absolute directory. Files are written
atomically with private permissions on Unix; they can contain prompts, tool
output, images, and provider reasoning metadata, so treat them as sensitive.

ACP clients can list, load, resume, fork, close, and delete sessions. Closing keeps
the checkpoint; deleting removes it. Loading, resuming, or forking creates a fresh
Starlark interpreter and MCP connections: checkpoints are not VM snapshots, side
effects are never replayed, and this format is not ATIF. On Windows, protect the
checkpoint directory with filesystem ACLs.

Incomplete model responses are explicitly stored in `AbortedResponses`, separate
from accepted conversation. They are never supplied to the model or replayed to
an ACP client when a session is loaded, resumed, or forked. In a live turn, CPE
checkpoints the abort, sends an advisory notice (or a text update for clients
without notice support), and retries only the model response when eligible.
Completed tool effects are not rerun. A checkpoint failure stops retrying, and
restoring a saved session never automatically restarts an aborted response.

Text and inline-image prompts are supported. Additional working directories,
ACP-tunneled MCP, session modes, audio prompts, and automatic context-budget
management are not implemented. Unsupported operations return explicit errors.

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

ACP model execution reads credentials again at the start of each turn. A
`Responses` model at `https://chatgpt.com/backend-api/codex/` can omit `api_key`
to use the stored ChatGPT subscription; models at the exact HTTPS host
`opencode.ai` can use the stored OpenCode key. Explicit model keys take precedence.
Stored subscription credentials are never attached to other hosts or plain HTTP.
ACP authentication verifies a prior terminal login; it does not open a login UI.
Expired ChatGPT credentials require `cpe auth login openai` again; automatic token
refresh is not implemented.

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
