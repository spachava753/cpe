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
The defaults are a ten-minute turn deadline and 32 model requests per turn,
configurable with `--turn-timeout` and `--max-generations`.

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
streamable HTTP, and SSE servers. Discover with `mcp_servers()`, inspect schemas
with `describe_mcp_tools(server)`, and call tools through `load_mcp_tools(server)`:

```python
tools = load_mcp_tools("screenshots")
result = tools["capture"]({})
print(result.is_error)
# Inspect structured_content and content before choosing what the model sees.
emit(result.content[0])
```

Results retain content blocks, structured content, metadata, and `is_error` for
scripts. Only `print` and `emit` expose output to the model; intermediate tool
results do not become separate conversation messages. `emit` supports text and
PNG/JPEG/GIF/WebP images. Audio and other binary resources remain inspectable in
the result and emit an omission notice; resource links are not automatically
fetched. Models without image input receive a text notice instead of the image.

Skills and plugins are discovered from `~/.agents/{skills,plugins}` and each
session's `.agents/{skills,plugins}`. `--skills` and `--plugins` add roots, resolving
relative flag paths against the launch directory. Project skills override global
skills with the same name; duplicate plugin names are errors. Skills appear as
slash commands, with plugin skills named `/plugin:skill`. Plugin MCP configuration
supports `${PLUGIN_ROOT}`; `${PLUGIN_DATA}` is not supported yet.

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

Text and inline-image prompts are supported. Additional working directories,
ACP-tunneled MCP, session modes, audio prompts, and automatic context compaction
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
