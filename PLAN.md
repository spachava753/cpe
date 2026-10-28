# Tool runtime refactor

This document is a version controlled planning document for refactoring how the tool runtime works. I want to refactor the tool runtime that powers tool calling in the agent loop.

## Behavior

The below section describe different behaviors that we desire the tool runtime to have.

### Available tools

The tool runtime will expose exactly 2 tools to the model for it to invoke. The 2 tools are `bash` and `starlark_exec`.

The `bash` tool will be a simple tool to execute whatever bash script the model generates and return the result of stdout and stderr as text tool result. The description will be:
```text
Execute the generated bash script in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, the full output is saved to a temporary file, and filepath will be provided in the tool result. Optionally provide a timeout in seconds.
```
The tool input schema is:
```json
{
  "parameters": {
    "type": "object",
    "properties": {
      "script": {
        "type": "string",
        "description": "Bash script to execute"
      },
      "timeout": {
        "type": "number",
        "description": "Timeout in seconds (optional, no default timeout)"
      }
    },
    "required": ["script"]
  }
}
```

The `starlark_exec` tool will be utilized to edit files, orchestrate tool calls, and view binary files like images. The tool will execute model generated [StarlarkX](https://github.com/spachava753/starlarkx) code, which is a language that has Python-like syntax, though there are minor syntax differences. The biggest difference is that there is no classes system, no reflection, and no standard library. However, the syntax and support for datatypes, functions and variables is the same as Python. In many cases, StarlarkX code will just look like Python code.
The description will be:
```text
Execute StarlarkX code in the current working directory. Returns the text printed in the script. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, the full output is saved to a temporary file, and filepath will be provided in the tool result. Optionally provide a timeout in seconds.
```
The tool input schema is:
```json
{
  "parameters": {
    "type": "object",
    "properties": {
      "code": {
        "type": "string",
        "description": "StarlarkX code to execute"
      },
      "timeout": {
        "type": "number",
        "description": "Timeout in seconds (optional, no default timeout)"
      }
    },
    "required": ["code"]
  }
}
```
The `starlark_exec` tool is not a REPL, each tool call will launch a new StarlarkX thread to execute. StarlarkX threads are extremely cheap to start and execute so we can create new ones for each tool call. The StarlarkX runtime will have MCP tools exposed as builtin functions that the model can generate code to orchestrate.

### Loading MCP server tools

MCP tools will be available to the runtime from 3 different sources:
1. Configured MCP servers via settings.json
2. Plugin defined MCP servers
3. ACP session provided MCP servers

The user will configure MCP servers in ~/.cpe/settings.json. Configured MCP servers can look like this:
```jsonc
{
  ... other settings
  "mcpServers": {
    "mcp-stdio": {
      "command": "mcp-command",
      "enabled": false
    },
    "mcp-http": {
      "url": "https://example.com/mcp-oauth",
      "description": "Example mcp server config"
    }
  }
}
```

Installed [Agent Plugins](https://agent-plugins.org/specification) may also define MCP servers in its plugin manifest, and we load MCP tools from the MCP servers defined in the plugin manifest.

Finally, when initializing an ACP session, the ACP client may provide an MCP servers configuration that we should use to connect to and load tools.

We aim to support MCP servers from protocol revision 2025-06-18 and onward[^1].

MCP tools will be exposed as `tools.mcp_server_name__tool_name(input) -> ToolCallOutput`. The names of the builtin functions will be constructed via the MCP server name and tool name. However, since function names have naming limitations (a StarlarkX identifier may contain only ASCII letters, digits, and underscores, and cannot start with a digit), the runtime normalizes both parts by replacing every other character with `_`. Server names are arbitrary configuration keys, and MCP tool names commonly contain `-` and `.`, so normalization is the normal case rather than an edge case. For example, the `create-issue` tool on a server configured as `github.com` becomes `tools.github_com__create_issue`. Two different names can normalize to the same identifier, so the model should take function names from discovery (see [Orchestrating MCP tools](#orchestrating-mcp-tools)) rather than derive them from display names. [MCP name conflict](#mcp-name-conflict) covers how collisions are resolved.

Keeping MCP tools under a `tools` value, rather than making each one a global, also means an MCP tool can never shadow a runtime builtin such as `read_file`, and a model-written variable can never shadow a tool.

#### Connecting at session setup

Session setup connects to every enabled server from all three sources and lists its tools, concurrently. Connecting eagerly means a misconfigured or misbehaving server shows up immediately, as a server that failed to connect, rather than partway through a task when the model first reaches for it. It also means the session knows every server's tools, descriptions, and instructions before the first prompt, so the system prompt can describe them (see [System instruction construction](#system-instruction-construction)). Because connections are shared across sessions (see [Connection re-use](#connection-re-use)), only the first session in a CPE process pays the startup cost; later sessions reuse the already-running servers.

**Failing servers are skipped.** CPE is an ACP agent. It has no reliable channel for showing the user a configuration error, and failing the whole session over one bad server would block all work on something the user may not even need for this task. A server that fails to connect or list its tools is therefore left out of the session: it is not in the system prompt, discovery, or `tools`. Each server gets a bounded amount of time to connect and list its tools. The bound must be long enough for a cold `npx` or `uvx` start, and a server that exceeds it is skipped like one that failed, so one hung server cannot stall session creation. CPE writes a diagnostic to stderr, which ACP clients typically keep in their logs, but the intended way to find and debug a failing server is the `cpe mcp` commands (see [Managing MCP servers](#managing-mcp-servers)).

A server that fails after setup, for example a stdio process that crashes, is handled per call: the call raises a transport error, and the pool reconnects on next use.

Each session lists every server's tools during setup, even when it reuses a connection another session opened, so a new session always starts from the server's current tools. That list is the session's catalog for its whole lifetime. The system prompt, discovery, and `tools` all use it. CPE does not yet act on `notifications/tools/list_changed`. A tool a server adds mid-session is not available until the next session, and calling a tool the server has removed fails with the server's error. See [Deferred](#deferred).

`"enabled": false` in `settings.json` removes a server from the session entirely, so it is neither connected nor advertised to the model. Plugin servers have no such switch. A plugin is only a bundle of skills and MCP servers, so there is no separate step of "loading" a plugin. An installed plugin's servers are part of every session, just like servers from `settings.json`.

### Managing MCP servers

Because sessions skip failing servers rather than reporting them, users need another way to see which servers work. That is the job of a `cpe mcp` command group, alongside `cpe plugins` and `cpe skills`. These commands resolve the same server set that a session started in the current directory would use: servers from `settings.json` and from plugins discovered for that directory. Servers supplied by an ACP client exist only inside that client's sessions, so the CLI cannot see them.

- `cpe mcp list` connects to every server, concurrently and with the same time bound as session setup, and prints one row per server: its name and callable prefix, its source (settings, or the plugin it comes from), its status (connected, disabled, or failed with the error), its description, and a comma-separated list of its tool names. It exits nonzero if any enabled server failed, so it also works as a check in scripts.
- `cpe mcp show NAME` connects to one server and prints everything a session would learn from it: server info (name, version, description), instructions, and each tool with its callable name and description. A flag adds the full input and output schemas. When connection fails, the output includes the stdio server's stderr, which is usually where the real cause appears (a missing binary, a bad argument, an authentication error).

Both commands use the same connection and naming code as sessions, so what they report is what the model gets. Authentication for HTTP servers that require OAuth would also belong in this group, for example as `cpe mcp login NAME`. That is outside the scope of this refactor.

### Orchestrating MCP tools

MCP tools are never registered with the model as tool definitions. The model sees two fixed tools, and it reaches MCP tools only by writing StarlarkX code. There are three reasons for this:

1. **Context cost.** Tool schemas are large, and a few servers can contribute dozens of them. Registering them directly would put every schema in every request, including requests that use none of those tools.
2. **Composition.** Calling tools one at a time through the model means that every intermediate result passes through the context window, and each step costs a round trip. A script can call a tool, look at its result, call another based on that result, and loop over many items, all within one tool call.
3. **Selecting results.** Remote tools often return far more than the model needs. A script can drop most of a result and print only the part that matters. Models are good at writing code, so leaning on that capability should keep improving as models do, without CPE having to anticipate each workflow.

#### Discovery

Since schemas are not in the context, the model discovers tools from inside a script:

- `search_tools(query: str, limit: int = 8, server: str = None) -> list`: keyword search over each tool's name, its description, and the names and descriptions of its input schema fields, including nested fields. The name is matched in both its original MCP form and its callable form. The search also covers the server each tool belongs to, so a query about what a server is for, such as "issue tracker", finds its tools even when no tool's own text uses those words. A server can have up to three descriptions: the `description` configured in `settings.json`, the server's own `serverInfo.description`, and its initialization `instructions`. The system prompt picks one of them by precedence (see [System instruction construction](#system-instruction-construction)), but search reads all three. The prompt has to stay short, while search costs nothing extra for reading more text, and each source can contain words the others lack. The `server` argument narrows a search to one server. Each match includes the tool's name under `tools`, such as `github__create_issue`, and a short description.
- `describe_tool(name: str) -> ToolDescription | None`: everything needed to call one tool. `name` is the tool's name under `tools`, as returned by `search_tools` (a leading `tools.` is also accepted). It returns `None` if no tool has that name. The result is a struct, described below.
- `describe_server(name: str) -> dict | None`: the server's description, its full initialization instructions, and its tool names. The system prompt carries only a short summary of each server, so this is where the server's complete guidance reaches the model.

Discovery reads only cached metadata and never performs a remote tool call.

`describe_tool` returns this shape:

```python
class ToolDescription:
    name: str                      # name under `tools`: "github__create_issue"
    mcp_name: str                  # original MCP tool name: "create-issue"
    server: str                    # server prefix: "github"
    title: str | None              # human-readable title, if the server provides one
    description: str               # full description, never truncated
    input_schema: dict             # JSON Schema of the input object
    output_schema: dict | None     # JSON Schema of `structured_content`, if declared
    annotations: ToolAnnotations   # behavior hints from the server

class ToolAnnotations:
    read_only: bool | None         # does not modify its environment
    destructive: bool | None       # may destroy or overwrite data
    idempotent: bool | None        # repeating a call with the same input has no further effect
    open_world: bool | None        # interacts with external systems beyond the server
```

StarlarkX has no classes or type annotations. The notation describes a struct: fields are read as attributes (`d.input_schema`), and the schemas are ordinary dictionaries and lists decoded from JSON, so a script can walk them like any other data, for example `d.input_schema["properties"].keys()`. The base system prompt contains this data model verbatim. Python-style classes are the most compact way to tell the model the exact field names and types, and models read the notation reliably even though the language cannot run it.

`annotations` carries the MCP tool annotations. Each one is `None` when the server doesn't provide it. They are hints the server makes about its own tool, not guarantees. They help the model decide, for example, whether a call is safe to repeat after a transport error, or whether to check before calling a destructive tool. They are not trusted for anything stronger than that.

#### Calling tools

A tool function takes its input either as one dictionary or as keyword arguments, but not both:

```python
issue = tools.github__get_issue({"owner": "spachava753", "repo": "cpe", "number": 179})
issue = tools.github__get_issue(owner = "spachava753", repo = "cpe", number = 179)
```

Keyword arguments read more like ordinary function calls. The dictionary form is still needed for input properties whose names are not valid identifiers.

A call returns a `ToolCallOutput`:

- `is_error: bool`: the MCP `isError` flag.
- `content: list`: content blocks. Every block has a `type`. Text blocks have `text`. Image and audio blocks have `data` (decoded bytes) and `mime_type`. Resource links and embedded resources expose their URI and fields.
- `structured_content`: the decoded `structuredContent`, or `None`.
- `text: str`: all text blocks joined, as a shortcut for the common case.
- `meta`: the result's `_meta`, or `None`.

MCP results may be unstructured and may contain binary data. Converting them to StarlarkX values without loss lets the script choose what to do with each part: print text, show an image or audio clip with `view(block.data, block.mime_type)`, or write a PDF to disk for a later bash command to process.

A tool reporting `is_error` returns normally, while a transport or protocol failure raises an error and stops the script. StarlarkX has no `try`/`except`. If a tool-level error raised, a script could not inspect the error text, which the spec now recommends servers use to report invalid input so the model can correct it. A dropped connection, on the other hand, means the outcome of the call is unknown, and continuing the script would build on that uncertainty.

JavaScript code-mode runtimes, such as pi's `codemode` tool, make the opposite choice: an MCP result with `isError` throws inside the script. That works for them because a script can catch the exception and read its message. Raising in StarlarkX would instead end the script and hide the server's explanation from any logic that could handle it.

The cost of returning errors is that a script that never checks `is_error` can continue past a failed call. The system prompt therefore states plainly that every MCP call must be followed by an `is_error` check before its result is used, and its examples always do so. Printing a `ToolCallOutput` also shows the error flag.

CPE never retries a tool call automatically, because a call may not be safe to repeat.

#### Persisting results across calls

`starlark_exec` is not a REPL, so nothing in the interpreter survives from one tool call to the next. Results that need to outlive an invocation go to the filesystem with `write_file`, and a later script, or a bash command, reads them back. The runtime provides no separate key-value store. Files are a mechanism models already know well. Bash can process them with `jq`, `grep`, or any other program, and they need no CPE-specific API. The issue's guiding idea applies here: give the model the general tool it already knows how to use, rather than a special-purpose one.

```python
results = []
for repo in ["cpe", "gai", "starlarkx"]:
    out = tools.github__list_issues(owner = "spachava753", repo = repo, state = "open")
    if out.is_error:
        fail("listing %s: %s" % (repo, out.text))
    for issue in out.structured_content["issues"]:
        results.append({"repo": repo, "number": issue["number"], "title": issue["title"]})
write_file("/tmp/open_issues.json", json.encode(results))
print("%d open issues across 3 repos" % len(results))
```

#### Concurrency

There are two kinds of concurrency to consider.

**Within a script.** Independent tool calls often dominate a script's wall time, because each is a network round trip. The runtime can provide `parallel(calls: list) -> list`, where each element is a `(function, input)` pair. Results come back in the order of the input list:

```python
outs = parallel([
    (tools.github__get_issue, {"owner": "spachava753", "repo": "cpe", "number": n})
    for n in [170, 175, 179]
])
```

`parallel` runs only host functions concurrently, meaning MCP tools, `bash`, and the file builtins. StarlarkX code itself stays single-threaded. Starlark values are not safe to share between threads until they are frozen, and StarlarkX deliberately has no user-level concurrency syntax. Running only host calls concurrently captures nearly all of the latency benefit without introducing shared mutable state into the language. If any call raises, the remaining calls are cancelled and the error propagates. A call that merely reports `is_error` does not stop the others.

**Across tool calls.** Models can request several tool calls in one turn. Invocations share nothing but the filesystem and MCP connections, with no interpreter state carried between them, so the runtime does not serialize `bash` and `starlark_exec` calls from the same turn. gai's agent loop currently executes a response's tool calls in order, so concurrent execution across calls also needs that loop to dispatch them concurrently. Two concurrent calls that edit the same file can conflict. That is the same risk as two concurrent shell commands, and the model has to coordinate them.

### Builtin StarlarkX Tools

In addition to having tools exposed as functions, the runtime will also have the following builtin functions available:
- `view(data: bytes, mime_type: str)`: attaches media to the tool result so the model can see or hear it: images, audio, video, or PDFs, such as `view(read_file("shot.png"), "image/png")`. `mime_type` is required, and an empty value raises an error, which fails the tool call and tells the model to supply one. The type is never guessed from the bytes. A wrong guess would produce a misleading provider error or misread media, while a missing type is an error the model can fix immediately. MCP image and audio content always carries a MIME type, so `view(block.data, block.mime_type)` works directly on MCP results. Embedded resources and resource links may omit it, in which case the model has to state the type. Media follows the printed text in the order it was attached. CPE sets no size or format limits of its own; what is accepted is up to the provider and the selected model (see [Media the provider rejects](#media-the-provider-rejects)).
- `bash(script: str, timeout: float = None) -> BashResult`: runs a bash script exactly as the `bash` tool does, with the same working directory, process-group cleanup, and timeout behavior. See below.
- `read_file(path: str) -> bytes`: reads an entire file. Relative paths are resolved against the session's working directory. It returns bytes rather than text because the same function has to feed `view` and preserve exact bytes for edits. Text needs one `.decode()`. The model can also use bash's `cat`, `sed -n`, or `rg` to read files. `read_file` is for when the contents go into further script logic.
- `write_file(path: str, data: str | bytes)`: creates or replaces a file and creates missing parent directories. It writes to a temporary file and renames it into place, so a reader never sees a half-written file, and it keeps the existing file's permission bits. This is the persistence mechanism described in [Persisting results across calls](#persisting-results-across-calls), and it lets scripts write artifacts that bash commands then consume.
- `edit_file(path: str, old: str, new: str)`: replaces exactly one literal occurrence of `old` with `new`. `old` must be nonempty and match exactly once (overlapping matches count as ambiguous), and the change must alter the file. Requiring a unique match means an edit never silently lands in the wrong place: if the model's view of the file is stale or its snippet is too short, the edit fails and the model has to look again. The replacement is atomic per file.

  It returns `None` on success and raises on failure, with an error that says which condition failed: no match, how many matches, or no change. A returned diff would only restate the `old` and `new` the model just wrote. A boolean would be `True` whenever the call returns, since every failure raises. Raising rather than returning `False` is deliberate. Unlike an MCP error or a nonzero exit status, a failed edit has no useful result for the script to inspect, and a script that ignored a `False` would carry on as if the file had changed. Raising stops the script at the failed edit, leaves earlier edits in place, and puts the reason in the tool result.

  The consequence has to be explicit to the model, and the base system prompt states it: **a failed `edit_file` ends the whole script.** Nothing after it runs. That includes later `edit_file` calls that would have succeeded, MCP tool calls, `bash` calls, file writes, and prints. Nothing before it is undone, because StarlarkX has no transactions, and edits, tool calls, and commands that already ran keep their effects. For example, if a script edits three files and the second edit fails, the first file is changed, the second and third are not, and any MCP call the script makes after the edits never happens. The tool result shows the output printed before the failure, followed by the error, so the model can see where the script stopped. Two habits follow, and the prompt suggests both:
  - Put edits before side effects that depend on them, such as an MCP call that opens a pull request.
  - Don't combine independent edits into one script when a single failure shouldn't block the others.
- `print(*args)`: writes text to the tool result.
- `search_tools`, `describe_tool`, `describe_server`, `parallel`, and `tools`: described in [Orchestrating MCP tools](#orchestrating-mcp-tools).
- `json`: a preloaded module with StarlarkX's `json.encode`, `json.decode`, and `json.indent`. It is predeclared rather than loaded because there are no other modules to load. Requiring `load("json.star", "json")` in every script would add ceremony and give the model another way to make mistakes.
- `compact(summary: str)`: replaces the conversation history with the model's summary. Compaction is a builtin rather than a third tool, so the model-facing surface stays at exactly two tools. Being a builtin also lets the model build the summary with code. It can read the files it changed, the plan it wrote to disk, or the output of `git diff --stat` through `bash`, then assemble those into the summary string instead of reconstructing them from memory.

The environment is plain StarlarkX: its interpreter and its `json` module, with no embedding framework or host standard library on top. There is no `load`. Python-style modules such as `os`, `subprocess`, and `requests` are deliberately absent. Partial reimplementations would be worse on every axis: the model would assume Python behavior they don't have, and the system prompt would have to document every gap. Directory listing, HTTP, and every other process-level operation go through bash, using programs the model already knows (`ls`, `find`, `curl`).

#### Running bash from a script

Scripts still need command output. A script might paginate through an MCP tool, then run `git log` for each result, or run tests and file an issue for each failure. If commands could run only in the `bash` tool, every such workflow would need a round trip through the model just to hand data from one tool to the other.

The `bash` builtin closes that gap without adding a second way to run processes, an approach taken from pi's code mode, where scripts call the agent's own `bash` tool. It is the bash tool, invoked from inside a script: one execution path, one set of semantics to document. It returns a `BashResult`:

- `output: str`: stdout and stderr interleaved, as the tool returns them.
- `exit_code: int`: the exit status. A process killed by a signal reports 128 plus the signal number.
- `timed_out: bool`
- `truncated: bool` and `full_output_path: str | None`: as with the tool, see [Overflow tool result](#overflow-tool-result).

A nonzero exit status does not raise. Many commands use exit codes as answers rather than failures (`grep` finding nothing, `git diff --quiet`), and StarlarkX cannot catch an error to recover from a raised one. The script checks `exit_code`, the same way it checks `is_error` on MCP results.

The script is meant to process the output rather than pass it straight to the model, so its limit is larger than the tool's: scripts receive the last 1 MiB, with the full output on disk. A bound is still needed, because the CPE process serves every session and a runaway command must not exhaust its memory through a script.

#### Media the provider rejects

Whether media is acceptable depends on the provider and the model: formats, dimensions, byte sizes, page counts, and audio or video support all vary. CPE does not maintain its own copy of those rules. They change often, and a stale copy would reject media a provider would accept or allow media it would refuse. Instead, CPE relies on the errors that come back.

The difficulty is timing. `view` succeeds when the script runs, but the provider sees the media only in the *next* model request. If that request fails, the tool result containing the media is already in the conversation, and every later request would fail the same way. CPE therefore handles a rejection itself instead of surfacing it as a failed turn:

1. When a model request fails with an invalid-request error, and the most recent tool results contain media attached with `view`, CPE writes each of those media blocks to a file in the session's temporary directory (the same one used for [overflow](#overflow-tool-wrapper)), named with an extension matching its MIME type. It then replaces each block with a text notice containing the provider's error message, the media's MIME type and size, and the file path.
2. CPE retries the request once. The model then sees, in the tool result, that its media was rejected and why, and it has a file to work on, for example resizing an image with `bash` and viewing it again, splitting a PDF, or extracting audio as text.
3. If the retry fails as well, the error was not about the media, and the turn fails normally.

Saving the media matters most when it came from an MCP tool result. The model may have passed `block.data` straight to `view` without ever writing it to disk, and once the script ends, those bytes exist only in the conversation, out of reach of any script or command. Without the file, the model could only call the tool again, which may be slow, costly, or not repeatable. The media is written when a rejection is handled, not on every `view` call. The bytes are already held in the tool result, so the result is the same, and successful views never touch the disk.

`gai` determines how precise this can be. It reports media an adapter cannot represent at all, such as audio for a provider without audio input, as a distinct error (`UnsupportedInputModalityErr`) before anything is sent. In that case CPE knows the exact block and modality responsible. Errors from the provider itself arrive as an `ApiErr` with a coarse kind (`invalid_request` or `request_too_large`), an HTTP status, and the provider's message. `gai` does not say which block caused the error, so CPE has to attribute it to the most recent media, and the retry confirms or refutes that attribution. If the provider's message is clear, as with an image too large or an unsupported type, the model receives exactly the actionable text it needs. If not, the model still learns that the media was rejected. Classifying media errors more precisely in `gai` would let CPE skip the retry, but it is not required for this design.

The dialect enables `while` loops, top-level control flow, and global reassignment, and disables recursion:

- `while` is needed for pagination and polling, such as calling a list tool until its cursor is empty. Plain Starlark can only express that with an arbitrary bounded `for` loop.
- Top-level control flow and reassignment let a script read like an ordinary Python script rather than a module of function definitions.
- Recursion adds little for orchestration scripts, and leaving it out removes one way for a script to run unbounded.


### System instruction construction

The system prompt is assembled from several layers. They are ordered from the most stable to the most session-specific, which also places the content most likely to be shared between sessions at the front of the cacheable prefix:

1. **Base prompt** (embedded in the binary): who CPE is, what the two tools are for and when to prefer each, the StarlarkX differences from Python that the model is most likely to trip over (no classes, no exceptions, no recursion, no `load`, string iteration via `.elems()`), and a reference for the builtins. The tool descriptions stay short, so this reference lives here rather than in the descriptions.
2. **Project instructions**: the `AGENTS.md` files that apply to the session's directory.
3. **Available skills**: standalone skills, those installed directly rather than as part of a plugin. Each entry has the skill's name, description, and path, without its body. The model reads a skill with bash or `read_file` when it is relevant.
4. **Available plugins**: one entry per installed plugin, with the plugin's name and description, its skills (listed like standalone skills), and its MCP servers with their tools.
5. **MCP servers**: servers from `settings.json` and from the ACP session, each with its callable prefix, description, and tools.

Plugins get their own section because they are remarkably similar to skills: a plugin is a named, described bundle the model decides is relevant to a task, and grouping its skills and servers under it says that they belong together. A plugin's skills appear only under the plugin, not in the standalone list, so each skill is listed exactly once.

Each server, in the plugin section and the server section alike, is listed with its tools: each tool's callable name and the first line of its description, capped at 500 characters. The cap matters because some descriptions are a single very long line. Input schemas are not included. They are the bulk of a tool definition, and `describe_tool` returns them when the model is about to call a tool. Names and one-line descriptions are enough for the model to know what exists and decide what to look up.

Because every server is connected at setup, its description can come from the configuration or from the server itself. The first available source is used:

1. The `description` field from `settings.json`.
2. The server's `serverInfo.description` (protocol revision 2025-11-25 and later).
3. The first line of the server's initialization `instructions`.

Plugin MCP configuration and ACP's MCP server configuration have no description field, so their servers start at step 2. This precedence applies only to the prompt; `search_tools` searches all three sources. The configured description exists so that the user can override what a server says about itself, or supply a description for servers that predate 2025-11-25.

#### Example

Here are layers 3 through 5 for a session with two standalone skills and one plugin, which bundles a skill and the GitHub MCP server. The session also has a `linear` server from `settings.json` and two servers supplied by the ACP client, `ide` and `notes`. The base prompt explains `tools`, `describe_tool`, and `describe_server`, so these sections only list what exists. The exact wording will be settled when the base prompt is written.

```text
# Available skills

Read a skill's SKILL.md when it is relevant to the task. Paths in a skill are relative to its SKILL.md.

- release-notes: Draft release notes from merged pull requests and the changelog.
  file: /home/user/.agents/skills/release-notes/SKILL.md
- pdf: Extract text, tables, and images from PDFs, and fill PDF forms.
  file: /home/user/dev/cpe/.agents/skills/pdf/SKILL.md

# Available plugins

## github-workflow
Triage issues and review pull requests on GitHub.

Skills:
- github-workflow:triage: Label and prioritize new issues using the repository's conventions.
  file: /home/user/.agents/plugins/github-workflow/skills/triage/SKILL.md

MCP server github_workflow_github: GitHub's official MCP Server.
- github_workflow_github__create_issue: Create a new issue in a GitHub repository.
- github_workflow_github__get_issue: Get details of a specific issue in a GitHub repository.
- github_workflow_github__list_pull_requests: List pull requests in a GitHub repository.
- github_workflow_github__search_code: Fast and precise code search across ALL GitHub repositories using GitHub's native search engine. Best for finding exact symbols, functions, classes, or specific code patterns. Supports qualifiers such as language:, repo:, org:, path:, and extension:, which can be combined with boolean operators AND, OR, and NOT to narrow results; when searching for an exact phrase, wrap it in quotes, and when searching within a specific repository always include the repo: qualifier because unscoped searches acro…

# MCP servers

## linear
Issue tracker for the CPE project. Use for sprint planning and anything filed as a CPE-### ticket.
- linear__create_issue: Create a new Linear issue.
- linear__get_issue: Retrieve a Linear issue by its identifier.
- linear__search_issues: Search Linear issues by text, state, assignee, or label.

## ide
Editor integration for the user's open workspace.
- ide__get_diagnostics: Return compiler and linter diagnostics for a file or the whole workspace.
- ide__get_selection: Return the user's current selection and the file it is in.
- ide__open_file: Open a file in the editor, optionally at a line.

## notes
- notes__append: Append text to today's note.
- notes__search: Search all notes by keyword.
```

What the example shows:

- **Where each description comes from.** `linear`'s is the `description` from `settings.json`. `github_workflow_github` uses its `serverInfo.description`, because plugin configuration has no description field. `ide` predates 2025-11-25, so it uses the first line of its initialization instructions. `notes` provides neither, so it is listed by name with no description.
- **Headings are server prefixes.** Each server is introduced by its callable prefix, which is also the name `describe_server` and the `server` argument of `search_tools` take. That way the model never has to translate between a display name and a callable name. A plugin server's prefix includes the plugin name, which makes it long, but it is unambiguous.
- **Tool names are complete.** Each tool is listed by its full name under `tools`, including the server prefix and any collision suffix, so the model can copy it directly into `tools.<name>(...)` or `describe_tool(...)`.
- **Long descriptions are cut.** The fourth GitHub tool shows the cap: its description is a single line longer than 500 characters, so it is cut at 500 characters and ends with `…`. Descriptions that span several lines are cut at the first line break, so most tools show only their opening summary.
- **Plugin skills.** Plugin skills use the `plugin:skill` name and appear only under their plugin.

#### Caching

The two tool definitions never change, and the system prompt should stay byte-identical for the life of a session, so provider prompt caching can reuse the whole prefix on every turn. The design supports this in three ways:

- The prompt is built once, from the catalog fixed at session setup, so no change on a server's side can alter it mid-session.
- The order of skills, plugins, and servers is deterministic, sorted by name, so the same configuration always produces the same bytes.

### Overflow tool result

Both tools return text, and both apply the same limit: the model sees at most the **last 2000 lines or 50 KB** of output, whichever limit is hit first. When output exceeds either limit, the complete output is saved to a temporary file, and the result begins with a notice naming the file:

```text
[Output truncated: showing lines 8121-10120 of 10120 (50.0 KB of 1.4 MB). Full output: /tmp/cpe-3f2a.../call_01J....txt]
```

The model inspects the full output with ordinary tools, such as `rg pattern file`, `sed -n '100,200p' file`, or `read_file`, and does not need a CPE-specific retrieval API.

**Why the tail.** For commands, the end of the output is usually what matters: the failing test, the compiler error, the summary line, the exit status. The head is often only progress output. For `starlark_exec`, output is whatever the script chose to print, which is usually small. When it isn't, the error that stopped the script is at the end.

**Diagnostics are never truncated.** The bash exit status, a timeout notice, and a StarlarkX error with its backtrace are appended after truncation, outside the 50 KB budget. A result can therefore exceed 50 KB by a small amount, but the model always learns why an execution failed, even when the output before the failure was huge.

**Very long lines.** If the last line alone exceeds 50 KB, such as a minified bundle or a single-line JSON response, the model receives the last 50 KB of that line, cut at a UTF-8 character boundary, and the notice says the first line shown is partial.

**Media** attached with `view` is not part of the text budget.

**Result shape.** A `bash` result contains stdout and stderr interleaved in the order they were written. Separating the streams loses that ordering, which is often what explains an error. A nonzero exit status is reported after the output and marks the tool result as an error. A `starlark_exec` result contains printed text, followed by attached media, and is marked as an error if the script raised. A script that finishes without printing anything returns a short notice that it completed, so a silent script, such as one that only edits files, still confirms that it succeeded. In both tools, a timeout stops execution: bash's process group is killed, and the StarlarkX thread is cancelled along with any in-flight MCP calls and `bash` builtin calls. The result includes whatever output was produced before the timeout, followed by a notice of the timeout.

**File lifetime.** Overflow files outlive the tool call, and CPE does not delete them when the session closes. The conversation history refers to their paths, and a model resuming the session may want to read them. Because they are in the system temporary directory, the operating system can remove them, for example at reboot. A restored session may refer to a file that no longer exists, and the model then has to rerun the command. Storing overflow with the session would make it survive restoration, but CPE would then own cleanup and have to decide what forking a session does with it. Overflow is a convenience for inspecting output from recent commands, not a durable record, so the system temporary directory is the right home and losing old overflow output is acceptable.

## Implementation details

The runtime is made of a small number of parts: two tool handlers, a handful of StarlarkX builtins, an output sink shared by both tools, and a session-scoped view of MCP servers backed by a process-wide connection pool. The existing implementation is not a constraint. Existing code is worth keeping only where it already matches this design.

The responsibilities divide as follows:

- `config` loads the `mcpServers` settings.
- `agent` merges the three MCP sources into one list of server specifications at session setup and builds the system prompt from the layers above. It does not open connections.
- `mcpclient` owns connections, shared across sessions (see [Connection re-use](#connection-re-use)), and per-session views of them.
- `cli` gains the `cpe mcp` commands, built on the same `mcpclient` connection and naming code as sessions.
- `toolruntime` owns the two tools, the StarlarkX environment, and output handling. Naming lives in `mcpclient` so the `cpe mcp` commands and sessions name tools identically.
- Sessions have no key-value store at all. Neither scripts nor session records carry one, so checkpoints and restored sessions consist of the conversation and its options alone. Everything a model wants to keep goes to the filesystem.

The guides in `docs/` and the README's configuration section describe tool execution, MCP, limits, and persistence. They will be rewritten to match this design in the same change.

### Types and Functions

These are sketches to anchor the review. They describe which concepts exist and who owns them, not final signatures.

**Server specifications.** Session setup produces one list from all three sources. A spec is plain data and can be built without connecting:

```go
// mcpclient
type Source int // SourceSettings, SourcePlugin, SourceClient

type ServerSpec struct {
	ID          ServerID           // unique within the session, e.g. "github" or "myplugin:github"
	Prefix      string             // normalized callable prefix, e.g. "github" or "myplugin_github"
	Source      Source
	Description string             // what the system prompt shows; may be empty
	Transport   TransportConfig    // stdio command/args/env/cwd, or HTTP URL/headers
}
```

**Connections.** A process-wide pool owns the actual MCP client sessions. A session holds references to the ones it uses:

```go
// Pool shares connections between sessions in one CPE process.
type Pool struct{ ... }

// Servers is one session's view: every spec is connected at setup, failures
// are skipped, and references are released when the session closes.
type Servers struct{ ... }

func (s *Servers) Connected() []ServerSpec // servers that connected; failures are omitted
func (s *Servers) Tools(id ServerID) []*mcp.Tool                                // listed at setup, fixed for the session
func (s *Servers) Info(id ServerID) ServerInfo                                 // serverInfo and instructions
func (s *Servers) Call(ctx context.Context, id ServerID, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
func (s *Servers) Close() error
```

**The `tools` value.** `tools` is a StarlarkX struct whose attributes are the callable tools in the session's catalog. `dir(tools)` lists them all. The catalog is fixed for the session, so the struct can be built once and frozen. A frozen value is safe to share across concurrent invocations.

**Invocation.** Each `starlark_exec` call creates a fresh thread and predeclared environment. That environment is the same for every invocation:

```go
// toolruntime
type Runtime interface {
	Tools() []gaiagent.Tool // bash, starlark_exec; fixed for the runtime's life
	Instructions() string   // server lines for the system prompt; fixed at setup
	Close(context.Context) error
}
```

Nothing in the runtime changes after setup, so concurrent invocations need no runtime-wide lock.

**ToolCallOutput** is a StarlarkX struct built from `mcp.CallToolResult`, as described in [Calling tools](#calling-tools). Content blocks are converted eagerly, and base64 image and audio data is decoded to `bytes` once.

**Output sink**, shared by both tools; see [Overflow tool wrapper](#overflow-tool-wrapper):

```go
type outputSink struct{ ... } // io.Writer keeping a bounded tail, spilling to a file past the limit

func (s *outputSink) Write(p []byte) (int, error)
func (s *outputSink) Result() (tail string, truncated bool, path string, err error)
```

### Connection re-use

MCP connections are not free. A stdio server is a child process, often a Node or Python runtime that takes seconds to start, and some servers launch heavier resources, such as a browser. A streamable HTTP connection carries an `Mcp-Session-Id` that the server may associate with state. If every session opened its own connections, a user with several sessions open in one ACP client would run several copies of each stdio server.

Every connection lives in a process-wide pool, keyed by connection identity, and every server is shared. The key is the transport configuration after placeholder expansion: command, args, env, and cwd for stdio, and URL and headers for streamable HTTP and SSE. Sharing applies to every transport and every source, including servers from settings, plugins, and the ACP client, and there is no setting to opt out. Two sessions whose configuration produces the same key share one connection. Different keys, for example ACP servers carrying per-session credentials in their headers, naturally get separate connections, so no special case is needed for them.

Sessions acquire a reference to each server during setup and release it when they close. When the last reference is released, the pool keeps the connection open for a short grace period, so closing one session and opening another does not restart every server.

The cost of always sharing is that some servers keep state per connection. A browser-automation server shares its tabs, and a server with a "current project" setting shares that setting. Two sessions using one connection can see each other's state. This is the same situation as two terminal windows talking to one local daemon, and most servers keep no such state. A single, predictable rule is worth more than a per-server switch that users would have to discover and reason about. If a server turns out to need isolation, an opt-out can be added later.

Server-initiated requests are the other complication. During a tool call, a server can ask the client for elicitation (user input) or sampling (a model call). With a shared connection, the pool has to route such a request to the session whose call is in flight. CPE does not support elicitation or sampling yet; both are listed as future directions in the ideas issue. Until it does, CPE does not declare these capabilities, and servers do not send these requests. That needs revisiting when elicitation lands.

**Concurrent calls.** JSON-RPC multiplexes requests over one connection, and the Go SDK's client session supports concurrent calls. The pool should lock only around connection setup, never around a call. That is what allows `parallel` and concurrent tool calls to overlap.

**Broken connections.** If a stdio process exits or an HTTP session expires, the pool marks the connection broken. The in-flight call fails with a transport error, and the next use reconnects. The failed call is not retried; see [Calling tools](#calling-tools).

Sharing does not extend across CPE processes. Each ACP client launches its own CPE process, and sharing across processes would need a daemon, which is out of scope.

### MCP name conflict

Conflicts can occur at two levels.

**Server level**, detectable at session setup without connecting:

- Plugin servers are identified by plugin and server name together (`plugin:server`, normalized to the `plugin_server` prefix). They cannot collide with each other as long as plugin names are unique within a session, which discovery should require, and they are unlikely to collide with settings or ACP servers.
- An ACP server can have the same name as a settings or plugin server. For example, the user configures `github` in `settings.json`, and their ACP client also supplies a `github` server. **This is an error: session setup fails, and the ACP request (new, load, resume, or fork) returns an error naming the server and both of its sources.** Silently preferring either side would hide from the user that their client supplies servers that overlap their own configuration. Keeping both under source-suffixed names would leave the model to guess which one the user meant. Failing makes the user aware of the overlap, and they resolve it by renaming or disabling one of the two.

**Normalization collisions** occur when different names map to the same identifier, for example `my-server` and `my.server`, or tools `get-user` and `get_user` on one server. Each colliding name gets a short suffix derived from a hash of its original, pre-normalization names: the server name for a server prefix, and the server name and tool name together for a tool. For example, `get-user` and `get_user` on server `accounts` become `tools.accounts__get_user__3f9a1c` and `tools.accounts__get_user__b20e47`. Hashing the original names rather than the normalized ones is what tells the colliding names apart, and including the server keeps the suffix unique across servers. This makes the result deterministic and independent of discovery order, so a name the model used earlier in the conversation, or in a restored session, still resolves to the same tool. Collisions are rare, and when they happen a hash suffix is readable enough, because the model copies names from discovery output rather than typing them from memory.

**Separator ambiguity.** `tools.a__b__c` could mean server `a` with tool `b__c`, or server `a__b` with tool `c`. Normalization collapses repeated underscores in server prefixes, so a prefix never contains `__`, and the first `__` in a name always ends the server part. Tool names keep their underscores, since everything after the separator belongs to the tool.

An alternative is nested values, `tools.github.create_issue`, which avoids the separator entirely and gives each server its own namespace. It reads well, but it adds a level that `search_tools` results and `dir()` have to explain. The flat form matches how the plan names tools above.

### Overflow tool wrapper

The obvious design is a wrapper around each tool handler that takes the finished result text, truncates it, and saves the full text to a file. That is simple, but it means first holding the complete output in memory. For `starlark_exec` this is fine, since printed output is chosen by the script. For `bash` it is not, because a stray `cat` of a large log, or a verbose build, can produce gigabytes, and CPE serves every session from one process.

Instead, both tools write their output into a shared sink that never holds more than the visible tail:

1. The sink keeps a rolling tail bounded by both limits (2000 lines and 50 KB), and counts the total lines and bytes.
2. When the total first exceeds either limit, the sink creates the overflow file, writes everything seen so far to it, and from then on writes all further output to it as well. Small outputs, the common case, never touch the disk.
3. When execution ends, the wrapper builds the tool result: the truncation notice if needed, the tail, then the diagnostics (exit status, timeout, StarlarkX error) outside the budget, then any images.

For bash, the sink is connected directly to the process: stdout and stderr share one pipe into it, which is also what preserves their interleaving. For `starlark_exec`, `print` writes to it. The wrapper is the one place that knows the limits and the notice format, so the two tools cannot drift apart.

Overflow files go in one temporary directory per session (for example `$TMPDIR/cpe-session-<random>/`), named by tool-call ID, so related files are grouped and easy to find. If the overflow file cannot be created, for example because the disk is full, the result still includes the tail. The notice then states that the full output could not be saved instead of naming a path that does not exist.

## Deferred

- **Telling the model about harness changes mid-session.** Two situations need it. One is a server's tool list changing (`notifications/tools/list_changed`). The other is a server that was skipped at setup coming back, or a connected server breaking. Under this plan the model learns neither: the catalog is fixed at setup, and a skipped server simply isn't there, so the model cannot tell "not configured" from "configured but broken". The intended mechanism is a system message inserted mid-conversation, which many providers now accept. It tells the model about the change without rewriting the cached system prompt. Whether `gai` and CPE's providers can carry such messages needs checking before this is designed.
- **Runaway scripts.** With `while` enabled, StarlarkX termination is not guaranteed, and neither tool has a default timeout, so ACP cancellation is the backstop. A script that allocates without bound grows the shared CPE process, whereas a runaway bash command grows only its own process. Step and memory limits for `starlark_exec` are left for later.

[^1]: Yes. Revision 2025-11-25 added an optional `description` to the `Implementation` type, which servers send as `serverInfo` during initialization. The Go SDK exposes it as `mcp.Implementation.Description`. Servers on 2025-06-18 do not send it, and some newer servers leave it empty, so a configured `description` in `settings.json` is still useful as an override or fallback. ACP's MCP server configuration has no description field at all.
