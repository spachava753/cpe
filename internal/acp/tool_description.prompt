Execute Starlark code in a session-scoped REPL.

Submit a Starlark chunk containing statements and expressions, not a complete Go or Python program. Globals, functions, loaded modules, and mutable values remain available to later `starlark_repl` calls in this conversation. Conversation compaction starts a fresh REPL, so define state again after `compact_conversation` runs.

Starlark is Python-like but is not Python. There are no Python imports, classes, exceptions, or arbitrary Python packages. Use `print(...)` for textual output. Top-level control flow, `while`, sets, and global reassignment are enabled.

Dyson provides host-backed standard-library compatibility modules and globals. Load modules with explicit symbols:

```python
load("os.star", "os")
load("glob.star", "glob")
load("json.star", "json")
load("re.star", "re")
load("requests.star", "requests")
load("shutil.star", "shutil")
load("subprocess.star", "subprocess")
load("tempfile.star", "tempfile")
load("time.star", "time")
```

The `grp.star`, `pwd.star`, and `signal.star` modules are also available. JSON values can be encoded with `json.dumps(obj)`. Because Starlark reserves `load` as a keyword, decode a file with `getattr(json, "load")(file)` rather than `json.load(file)`. Module coverage is intentionally partial; an unavailable member produces a Starlark error. Filesystem, environment, process, and HTTP operations run with the CPE process's normal permissions.

CPE provides a read-only `acp.star` module for inspecting persisted ACP sessions:

```python
load("acp.star", "acp")
current = acp.get_session()
ids = acp.list_sessions()
other_ids = acp.list_sessions(cwd="/absolute/path/to/another/project")
```

`acp.get_session(id=None)` returns an `acp.Session` with `id`, `cwd`, `title`, `last_message_id`, and `messages` attributes. Omitting `id` reads the current session through the assistant message containing the executing tool call. Passing another ID reads that session's persisted head. `messages` is a chronological tuple of `acp.Message` values spanning all conversation compactions; each message exposes `id`, `parent_id`, `compaction_parent_id`, `created_at`, `role`, `tool_result_error`, and `blocks`. Each `acp.Block` exposes `id`, `kind`, `modality`, `mime_type`, `content`, parsed tool-call `name` and `arguments`, and `filename`.

`acp.list_sessions(cwd=None)` returns session IDs for the current ACP working directory, newest activity first. An explicit `cwd` uses an exact persisted-directory match and may inspect another project. Session history is not truncated, so search or filter it in Starlark and print only relevant excerpts.

The global `open(...)` builtin provides Python-style text and binary file reads. Native bytes returned by file, HTTP, subprocess, and related operations support `decode(...)`.

```python
print(open("README.md").read())

load("requests.star", "requests")
response = requests.get("https://example.com")
print(response.status_code)
```

A global `view_file(path, mime_type="")` builtin adds a binary artifact to the tool result so the model can inspect it. Relative paths are resolved against the ACP session working directory. MIME type is inferred from the extension or contents, or may be supplied explicitly. Images, PDFs, audio, and video are supported.

```python
view_file("diagram.png")
view_file("recording.bin", mime_type="audio/wav")
```

Each call must provide an execution timeout. When that timeout is reached, CPE cancels the active `starlark.Thread`. Runtime and syntax errors are returned as tool results so you can correct the next chunk.
