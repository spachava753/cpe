# MCP, plugins, and skills

[All guides](README.md)

CPE can add capabilities through skills and MCP servers. A skill provides
instructions and resources for a kind of task. A server using the Model Context
Protocol (MCP) exposes callable tools. A plugin bundles skills and MCP servers;
there is no separate step of loading a plugin, so an installed plugin's skills
and servers are part of every session.

## How extensions reach a session

```text
 settings.json      Installed plugins            ACP client
 MCP servers        skills + MCP servers         MCP servers
      |                    |                         |
      +--------------------+-------------------------+
                           |
                     Session setup
                           |
          +----------------+-----------------+
          |                                  |
       Skills                    Connect every MCP server and
  names, descriptions, paths     list its tools; skip failures
          |                                  |
          +----------------+-----------------+
                           |
         System prompt lists skills, plugins, servers,
         and each server's tools; scripts call the tools
```

CPE searches `~/.agents/skills` and `~/.agents/plugins`, plus the session's
`.agents/skills` and `.agents/plugins`. Launch options can add roots. Relative
extra-root paths are resolved against the directory where CPE was launched.

Project skills override global skills with the same name. Duplicate plugin
names are errors. Discovery happens when creating, loading, resuming, or forking
a session. Installing or removing a plugin changes what those later session
setups discover.

## Skills stay available without filling the prompt

The model receives skill names, descriptions, and file paths. It reads a relevant
`SKILL.md` and its resources with bash or `read_file` when the task calls for
them. This keeps unrelated skill bodies out of the initial context. A plugin's
skills are listed under the plugin rather than with standalone skills.

Skills also appear as slash commands. A recognized `/skill-name arguments`
command gives the model a reference to that skill and the supplied arguments.
Plugin skills use `/plugin:skill`. Unknown slash commands pass through as
ordinary prompt text. `/compact` is reserved for
[compaction](compaction.md), even if a skill has that name.

## Where MCP servers come from

CPE accepts servers from three sources:

1. **`~/.cpe/settings.json`**, under `mcpServers`.
2. **Installed plugins**, through their MCP configuration. A plugin's server is
   named `plugin:server`.
3. **The ACP client**, which can supply servers for one session.

A settings server uses stdio when it has a `command` and streamable HTTP when it
has a `url`:

```jsonc
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
      "headers": {"Authorization": "Bearer …"}
    },
    "legacy": {
      "type": "sse",
      "url": "http://localhost:9000/sse",
      "enabled": false
    }
  }
}
```

`type` is `stdio`, `http`, or `sse`, and is inferred when omitted. A stdio
server's optional `cwd` must be absolute; without it, the server starts in the
directory CPE was launched from. `"enabled": false` removes a server from every
session. `description` replaces what the server says about itself in the system
prompt. Unknown fields in `settings.json` are errors, so a misspelled setting
fails loudly. A settings entry that is otherwise invalid, such as one with
neither `command` nor `url`, is skipped like a server that failed to connect.

Plugin servers are always enabled. Plugin MCP configuration can refer to its
bundle through `${PLUGIN_ROOT}`; `${PLUGIN_DATA}` is unsupported, and a server
that uses it is skipped. A plugin stdio server starts in its plugin directory
unless it sets `cwd`.

If a server the ACP client supplies has the same name as a settings or plugin
server, opening the session fails with an error naming both sources. Rename or
disable one of them; CPE does not silently prefer either.

## Connecting at session setup

Session setup connects to every enabled server concurrently and lists its
tools. A server that fails to connect, fails to list its tools, or takes longer
than the MCP timeout is **skipped**: it does not appear in the system prompt,
discovery, or `tools`, and the session opens without it. CPE writes a line to
stderr naming the server and the error. The timeout is **1 minute** per server
by default; set it with `--mcp-timeout`. The whole setup is still bounded by
`--setup-timeout`.

Because sessions skip failing servers instead of reporting them, use the
`cpe mcp` commands to find and debug them:

- `cpe mcp list` connects to every server a session started in the current
  directory would use and reports each one's source, status (connected,
  disabled, or failed with the error), description, and tool names. It exits
  nonzero if any enabled server failed.
- `cpe mcp show NAME` connects to one server, named by its configured name or
  callable prefix, and prints its server information, instructions, and tools.
  `--schemas` adds input and output schemas. When connecting fails, the output
  includes the stdio server's stderr, which usually shows the real cause.

Servers that the ACP client supplies exist only inside that client's sessions,
so these commands cannot see them.

## Shared connections

All sessions in one CPE process share MCP connections. Two sessions whose server
configurations are identical use one connection; any difference in the parts a
transport uses, such as headers carrying per-session credentials, gives a
separate connection. Each session still lists the server's tools during its own
setup. When the last session using a connection closes, CPE keeps it open for
**30 seconds**, so closing one session and opening another does not restart
every server.

Sharing means sessions also share any state a server keeps per connection. A
browser-automation server shares its tabs, for example. Most servers keep no
such state. Sharing does not extend across CPE processes.

If a connection breaks, such as a stdio server exiting, the call in flight fails
and the next call reconnects. A failed call is never retried automatically,
because its outcome is unknown.

## How the model finds and calls tools

MCP tools are called from `starlark_exec` as `tools.<name>(...)`. A name combines
the server's callable prefix and the tool's name, separated by `__`. Characters
that StarlarkX identifiers cannot contain become `_`. Plugin prefixes include
the plugin name. The `create-issue` tool of a server configured as `github.com`
becomes `tools.github_com__create_issue`, and the `search` tool of plugin
`docs`'s server `wiki` becomes `tools.docs_wiki__search`.

When two names normalize to the same identifier, each gets a short suffix
derived from the original, unnormalized names, such as
`accounts__get_user__3f9a1c`. The suffix does not depend on connection order, so
a name keeps working across turns and restored sessions. Because of this, the
model copies names from the prompt or discovery rather than deriving them.

The system prompt lists every connected server under its prefix, with each tool's
name and the first line of its description, cut at 500 characters. Plugin
servers appear under their plugin. Input schemas are left out; the model reads
them on demand. A server's description in the prompt is the first available of:
the `description` from `settings.json`, the server's self-reported description,
and the first line of its instructions.

The tool lists are those of session setup and stay fixed for the session, which
keeps the prompt cacheable. A tool a server adds later becomes available in the
next session; calling a tool the server has removed returns the server's error.

Scripts can discover tools with:

- `search_tools(query, limit=8, server=None)`: keyword search over each tool's
  name, description, and input field names and descriptions, plus all of its
  server's descriptions and instructions. Matches have `name`, `server`, and a
  one-line `description`.
- `describe_tool(name)`: the tool's full description, input and output schemas
  as dictionaries, and the server's behavior hints (`read_only`, `destructive`,
  `idempotent`, `open_world`), which are claims rather than guarantees.
- `describe_server(name)`: the server's description, full instructions, and tool
  names.

Discovery reads the session's catalog and never calls a remote tool. An unknown
tool or server name returns `None`.

## Results and errors

A tool takes its input as one dictionary or as keyword arguments, and returns a
result with these fields:

| Field | Meaning |
| --- | --- |
| `is_error` | The tool reported failure. |
| `content` | Content blocks. Text blocks have `text`; image and audio blocks have `data` bytes and `mime_type`; resource links and embedded resources expose their URI and fields. |
| `structured_content` | Decoded structured content, or `None`. |
| `text` | All text blocks joined with newlines. |
| `meta` | The result's metadata, or `None`. |

A tool's own error returns normally, so a script can read the server's
explanation and decide what to do. Check `is_error` before using a result. A
transport or protocol failure raises instead and ends the script, because the
call may or may not have taken effect. Resource links are fetched only if the
script does so explicitly.

## Installing a plugin

Plugin installation copies the bundle and its resources. It does not run the
plugin or connect its MCP servers. Opening a session that discovers the plugin
is when its servers connect and stdio server processes can start.

Component problems can produce warnings while leaving the bundle installed.
During session discovery, nonfatal diagnostics go to stderr. Malformed manifests
and unsupported manifest schemas fail discovery.

Installation searches up to **5 directory levels** below the source root by
default. Use `--full-depth` to remove that cap or `--path` to select an exact
package directory. Plugin names have a **64-character maximum**. Skill names
over **64 Unicode characters** and descriptions over **1,024** are retained
with warnings; a skill's compatibility field over **500** makes its definition
invalid. See [Discovery limits](limits.md#discovery-and-file-editing).

See the [main README](../README.md#agent-plugins) for installation commands and
scope selection. Plugins and servers have the host access described in
[Tool execution](tool-runtime.md).
