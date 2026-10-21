# MCP, plugins, and skills

[All guides](README.md)

CPE can add capabilities through skills and MCP servers. A skill provides
instructions and resources for a kind of task. A server using the Model Context
Protocol (MCP) exposes callable tools. A plugin bundles skills, MCP configuration,
and supporting resources.

## How extensions reach a session

```text
 Global roots          Extra configured roots          Project roots
       \                       |                       /
        +----------------------+----------------------+
                               |
                       Session discovery
                               |
                  +------------+-------------+
                  |                          |
               Skills                    MCP servers
                  |                          |
        names, descriptions, paths     connect and list tools
                  |                          |
        model reads a skill when       namespace summaries
        relevant; slash commands       supplied to the model
        also available                       |
                                       scripts discover
                                       and call tools
```

CPE searches `~/.agents/skills` and `~/.agents/plugins`, plus the session's
`.agents/skills` and `.agents/plugins`. Launch options can add roots. Relative
extra-root paths are resolved against the directory where CPE was launched.

Project skills override global skills with the same name. Duplicate plugin
names are errors. Discovery happens when creating, loading, resuming, or
forking a session. Installing or removing a plugin changes what those later
session setups discover.

## Skills stay available without filling the prompt

The model receives skill names, descriptions, and file paths. It reads a relevant
`SKILL.md` and its resources when the task calls for them. This keeps unrelated
skill bodies out of the initial context.

Skills also appear as slash commands. A recognized `/skill-name arguments`
command gives the model a reference to that skill and the supplied arguments.
Plugin skills use `/plugin:skill`. Unknown slash commands pass through as
ordinary prompt text. `/compact` is reserved for
[compaction](compaction.md), even if a skill has that name.

A skill changes the guidance available to the model. Any file operations or
commands requested by that guidance still run through the normal
[tool runtime](tool-runtime.md).

## Connecting MCP servers

CPE accepts servers supplied by the ACP client and servers defined by discovered
plugins. All discovered plugin MCP servers are enabled. Supported connections
are stdio, streamable HTTP, and SSE.

CPE connects and discovers tools during session setup. A connection or discovery
failure fails setup and closes connections already opened. A skill-only plugin
requires no MCP connection.

The model sees namespace names and brief descriptions in its system guidance.
Full server instructions and tool schemas remain available through discovery.
This keeps a potentially large tool catalog available without putting every
schema in every initial prompt.

Inside Starlark, the model can use:

- `search_tools(query, limit=8, namespace="")` to find tools by keywords.
- `describe_tool(name)` to read a tool's description, signature, and input schema.
- `describe_namespace(name)` to read a namespace's description, server
  instructions, and tool names.

Search returns up to **8 matches by default**. A positive `limit` argument can
request a different maximum; emitted results still share the script's output
budget. Search examines local tool metadata: names, descriptions, schema fields,
and server guidance. Discovery queries perform no remote tool actions. An
unmatched search returns an empty list; an unknown description target returns
`None`.

Discovered tools are callable Starlark builtins with names beginning `mcp__`.
Use the returned names. Names are normalized for Starlark and disambiguated when
necessary, so guessing from a server's display name can produce the wrong call.

## Tool lists can change while a session is open

Connections and cached tool lists stay with the live session. When a server
announces a tool-list change, CPE refreshes the list before the next invocation.

```text
 Invocation A begins          Server announces change       Invocation B begins
          |                            |                            |
          v                            v                            v
 catalog A stays stable        refresh becomes due          fetch changed list
 throughout this script                                     use catalog B
```

Discovery and callable tools share one catalog for the whole invocation. A
notification during execution takes effect on the next call. The remote server
can still remove a tool immediately, so an invocation using an older catalog
may receive an error from the server.

If a refresh fails, the invocation fails before its script runs. The next
invocation tries the refresh again. Closing a session releases the connections
and cached lists. Restoring or forking creates fresh ones.

## Results and errors

A remote tool returns content blocks, structured content, metadata, and an
`is_error` flag. The script can inspect all of them and decide what to print,
display, or save. Text blocks expose text; image blocks provide bytes for
`image(...)`. Other binary content remains inspectable, and resource links are
fetched only if the script explicitly does so.

An MCP tool's error flag remains part of its inspectable result. A transport or
protocol failure raises an execution error. These can mean different things:
a tool may return a useful explanation of a rejected request, while a lost
connection may leave an external action's outcome uncertain. Neither situation
causes CPE to replay the action automatically.

## Installing a plugin

Plugin installation copies the bundle and its resources. It does not run the
plugin or connect its MCP servers. Opening a session that discovers the plugin
is when its servers connect and stdio server processes can start.

Component problems can produce warnings while leaving the bundle installed.
During session discovery, nonfatal diagnostics go to stderr. Malformed manifests
and unsupported manifest schemas fail discovery. Plugin MCP configuration can
refer to its bundle through `${PLUGIN_ROOT}`; `${PLUGIN_DATA}` is unsupported.

Installation searches up to **5 directory levels** below the source root by
default. Use `--full-depth` to remove that cap or `--path` to select an exact
package directory. Plugin names have a **64-character maximum**. Skill names
over **64 Unicode characters** and descriptions over **1,024** are retained
with warnings; a skill's compatibility field over **500** makes its definition
invalid. See [Discovery limits](limits.md#discovery-and-file-editing).

See the [main README](../README.md#agent-plugins) for installation commands and
scope selection. Plugins and servers have the host access described in
[Tool execution](tool-runtime.md).
