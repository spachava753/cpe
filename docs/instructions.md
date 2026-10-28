# Instructions and context

[All guides](README.md)

Every model request combines system guidance with the conversation's current
working context. Some guidance is loaded when CPE starts, some when a session
opens, and some when the model reads it during a task.

Understanding those times helps explain why editing an instruction file may
have no immediate effect on a live conversation.

## What reaches the model

```text
 System guidance
   base prompt
   appended personal guidance
   compaction guidance
   project instructions and nested-file index
   skill metadata
   plugins, MCP servers, and their tools
             |
             +-----------------------+
                                     |
 Working conversation                v
   full accepted history ------> Model request
           OR                        ^
   compaction summary                |
   + later messages -----------------+
```

The system guidance remains available after
[compaction](compaction.md). It lists each MCP tool's name and the first line of
its description; [MCP, plugins, and skills](extensions.md#how-the-model-finds-and-calls-tools)
shows how. Skill bodies, nested project instructions, and full MCP tool
descriptions and schemas enter the conversation when a tool call reads them and
returns their contents. Compaction can remove those earlier results from
working context, so the model may need to read them again.

## Personal system guidance

CPE ships a base system prompt. Files in `~/.cpe` let you customize it:

| File | Effect |
| --- | --- |
| `SYSTEM.md` | Replaces the built-in base prompt, including its host-tool guidance. |
| `APPEND-SYSTEM.md` | Adds guidance after whichever base prompt is selected. |
| `COMPACTION.md` | Replaces the summary-writing guidance independently of the base prompt. |

These files are read when `cpe acp` starts. Restart the process after changing
them. A missing file is optional; other read failures stop startup. An empty
`SYSTEM.md` selects an empty base prompt. Empty append or compaction files add
no guidance for their respective part.

Project instructions, skill metadata, and the list of plugins and MCP servers
with their tools are still supplied with a custom base prompt. Replacing the base therefore changes the
agent's general guidance while preserving its awareness of the session's
project and available extensions.

## Project instructions follow the session directory

CPE treats the working directory supplied by the client as the project root for
instruction discovery. It includes that directory's `AGENTS.md` contents and
lists paths to nested `AGENTS.md` files. The model is instructed to read the
applicable nested files before working in those directories, from outermost to
innermost, with more specific guidance taking precedence.

```text
 Session directory
   AGENTS.md                  included in system guidance
   component/
     AGENTS.md                path listed; model reads when relevant
     subcomponent/
       AGENTS.md              path listed; more specific guidance
```

Discovery stays below the supplied directory. It does not search ancestors or
infer a Git root. If a client opens a subdirectory as the session root, guidance
above it is outside this automatic discovery.

The scan skips `.git` directories and directory symlinks. Hidden and Git-ignored
directories can still contribute instruction files. File symlinks named
`AGENTS.md` are included. Missing files are optional; directory-scan and required
read errors fail session setup.

The root contents and nested-file index are loaded when creating, loading,
resuming, or forking a session. They stay fixed across turns in that live
session. Nested file bodies are read by the model when needed, so those reads
see the file at that time.

## When changes become visible

| Change | When CPE picks it up |
| --- | --- |
| Base, appended, or compaction guidance | Process startup. |
| Root project instructions and nested paths | Session creation, load, resume, or fork. |
| Skill metadata and installed plugin discovery | Session creation, load, resume, or fork. |
| A skill body or nested instruction body | When the model reads the file. |
| MCP servers and their tool lists | Session creation, load, resume, or fork. Tool lists stay fixed for the rest of the session. |

[MCP, plugins, and skills](extensions.md) covers extension discovery in more
detail. [Models and reasoning](models.md) explains how model-specific reasoning
and media support affect the context sent to a provider.
