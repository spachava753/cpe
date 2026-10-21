# Models and reasoning

[All guides](README.md)

A session has one selected model and a reasoning setting. You can change them
between turns while keeping the conversation and tool resources open.

CPE reads the model catalog at process startup. Each entry describes the provider
endpoint, model identifier, supported media, context limit, output limit, and
available reasoning levels. The [main README](../README.md#acp-server) has a
configuration example. Use the provider's actual capabilities: CPE relies on
these settings when preparing requests and reporting context usage.

## How a new session chooses its model

```text
 New session
      |
 Read remembered model
      |
      +-- configured --------------------> use it
      |
      +-- absent or no longer configured -> use first model in catalog

 Load / resume / fork
      |
 Use the session's saved selection
      |
      +-- model missing from catalog ----> report an error
```

The remembered model is shared across launches in `~/.cpe/last-model.json`.
Selecting a model through ACP or starting an accepted prompt remembers it for
future sessions. Creating or restoring a session alone leaves the preference
unchanged, and remembering a model leaves other live sessions unchanged.

An unreadable or malformed preference file prevents new-session creation.
Restoration uses the saved selection independently of that preference. A failure
to write the preference is reported on stderr while the successful model
selection or prompt continues.

New sessions use the configured initial reasoning effort from `--reasoning`,
or the provider default when none was supplied. The remembered model preference
does not remember the previous session's reasoning effort. Restored and forked
sessions keep their own saved effort.

## Switching during a conversation

Model and reasoning changes are available while a session is idle. A model
selection resets reasoning to the new provider's default because effort names
and capabilities differ between providers. You can then choose one of that
model's supported efforts.

The session keeps its transcript, compaction state, durable script data, and
MCP connections. Future requests are adapted for the selected model:

- Earlier assistant reasoning from other models is omitted from the request,
  along with model-specific replay metadata.
- Media the model cannot accept is replaced with a text notice.
- The original reasoning, metadata, and media stay in saved history.

This allows a conversation to move between models without discarding its
record. It also means the next model may have less information than the client
can display. For example, a text-only model can see a notice where a screenshot
appeared, but cannot inspect the image itself.

The new model's context window may be smaller. Changing models leaves the
working conversation intact, so consider [compaction](compaction.md) before
moving a long conversation to a smaller window.

## Provider requests

A turn can contain many model requests as the agent works through tools. CPE
retries eligible interrupted responses within a turn, while preserving completed
tool results. [Failures and cancellation](failures.md) explains those boundaries.

Model keys configured directly take precedence over stored login credentials.
For supported subscription endpoints, CPE can use locally stored credentials
and renew ChatGPT access tokens as needed. See [Credentials](credentials.md).

Provider-reported counts drive both the context meter and saved consumption
totals. Their meanings differ, as explained in
[Usage and context limits](usage.md).
