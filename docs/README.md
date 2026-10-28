# How CPE works

CPE connects a coding agent to an editor or another client through the Agent
Client Protocol (ACP). The client carries your prompts and displays the agent's
responses. CPE builds the model's context, runs the tools it requests, and saves
the conversation so you can return to it later.

These guides explain what happens while the agent works, what survives between
sessions, and how the different parts fit together. For launch commands and
configuration examples, see the [main README](../README.md).

## The whole picture

```text
 You
  |
  v
 ACP client <---------- progress, answers, tool results
  |                                  ^
  | prompts and session controls     |
  v                                  |
 CPE --------------------------------+
  |
  +-- Model provider
  |     receives instructions and working context
  |     returns text and requests to run tools
  |
  +-- Tool runtime
  |     runs bash scripts and StarlarkX scripts
  |       +-- files and commands on the host
  |       +-- tools supplied by MCP servers
  |
  +-- Saved session
        transcript, model settings, usage,
        and compaction summary
```

A prompt starts a **turn**. During a turn, the model can ask for tools, inspect
their results, and ask for more tools before it answers. Each **generation** is
one model response within that turn. Each **tool call** runs one bash or
StarlarkX script. A **session** holds the conversation across turns.

Those lifetimes explain many everyday behaviors. A script variable disappears
when its call ends. A session's saved conversation survives a process restart.
Stopping a turn leaves its completed file changes in place.

## Start with these three ideas

**The transcript and the model's working context can differ.** The transcript
keeps the accepted conversation. Compaction gives the model a shorter summary
of earlier work while preserving the transcript for later display. See
[Compaction](compaction.md).

**Restoration brings back saved information.** CPE opens fresh tool resources
and MCP connections when you return to a saved session. Files and remote
services remain in their current state; historical actions are never replayed.
See [Persistence and recovery](persistence.md).

**Tools act on the real host.** Scripts and configured MCP servers run with the
user's access. CPE provides no execution approval prompts or sandbox. A tool
error or canceled turn can leave completed effects behind. See
[Tool execution](tool-runtime.md) and [Failures and cancellation](failures.md).

## Reading by topic

| Guide | Questions it answers |
| --- | --- |
| [Sessions and turns](sessions.md) | What stays open? What happens on cancel, close, load, resume, or fork? |
| [Persistence and recovery](persistence.md) | What gets saved? How does script data survive? What happens after a crash? |
| [Compaction](compaction.md) | What does the model forget? When does a summary take effect? |
| [Tool execution](tool-runtime.md) | How do scripts, commands, edits, and output behave? |
| [Limits and defaults](limits.md) | What are the exact size limits, timeouts, retry counts, and configurable defaults? |
| [MCP, plugins, and skills](extensions.md) | How are capabilities discovered, connected, and refreshed? |
| [Instructions and context](instructions.md) | What guidance reaches the model, and when is it read? |
| [Models and reasoning](models.md) | How are defaults chosen? What changes when switching models? |
| [Credentials](credentials.md) | How does login work? What happens when a token expires? |
| [Usage and context limits](usage.md) | What do the token counts mean? When does CPE warn about context? |
| [Failures and cancellation](failures.md) | What retries? What stops? What should be checked before trying again? |

The pages describe CPE's current behavior. Client interfaces vary: a client may
expose only some of the session controls or display progress differently. CPE
currently accepts text and inline images in prompts. Audio prompts, additional
working-directory declarations, ACP-tunneled MCP, and session modes are
unsupported.
