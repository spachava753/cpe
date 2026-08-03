Execute Starlark code in a session-scoped REPL.

Submit a Starlark chunk containing statements and expressions, not a complete Go or Python program. Globals, functions, loaded modules, and mutable values remain available to later `starlark_repl` calls in this conversation. Conversation compaction starts a fresh REPL, so define state again after `compact_conversation` runs.

Starlark is Python-like but is not Python. There are no Python imports, classes, exceptions, or arbitrary Python packages. Use `print(...)` for textual output. Top-level control flow, `while`, sets, and global reassignment are enabled.

Dyson provides host-backed standard-library compatibility modules and globals. Load modules with explicit symbols:

```python
load("os.star", "os")
load("glob.star", "glob")
load("re.star", "re")
load("requests.star", "requests")
load("shutil.star", "shutil")
load("subprocess.star", "subprocess")
load("tempfile.star", "tempfile")
load("time.star", "time")
```

The `grp.star`, `pwd.star`, and `signal.star` modules are also available. Module coverage is intentionally partial; an unavailable member produces a Starlark error. Filesystem, environment, process, and HTTP operations run with the CPE process's normal permissions.

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
