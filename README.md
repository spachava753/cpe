# CPE

CPE is a small terminal programming agent built with Bubble Tea, gai, Starlarkx,
and Dyson. The model has one tool: a persistent `starlark_repl`. It uses Starlark
functions to read and write files, run commands, call HTTP services, and invoke
application-supplied tools or MCP servers.

This is a replacement for the previous ACP application. The old ACP server,
SQLite sessions, YAML configuration, the old OAuth commands, the previous MCP configuration format, and
Harbor/Pier adapters have been removed. Existing SQLite sessions are not migrated.

## Run

Requires Go 1.26.6 or newer.

```sh
go build -o cpe .
./cpe --init
./cpe
```

The starter configuration selects Codex. Use `/login` to choose a login provider,
or `/login openai` to open the browser sign-in flow. CPE validates the OAuth callback with PKCE and state, then
saves its own credentials to `~/.cpe/auth.json`. Use `/login openai device` for a remote
terminal or a machine without a browser: open the displayed URL and enter the
code. Device login may need to be enabled in your ChatGPT account settings.
Escape or Ctrl+C cancels either flow. Login instructions stay out of model
context and session history. The TUI starts without credentials so you can sign
in, and the selected model is ready to use as soon as login succeeds.
The old `/login codex` and `/login device` commands remain compatibility aliases.

Configuration always lives in `~/.cpe`, including on Linux and macOS. Edit
`config.json` for settings and `system.md` for system instructions, then restart
CPE to apply changes. CPE does not look in the current directory or
`$XDG_CONFIG_HOME`. `--init` only creates missing files.

```json
{
  "default_model": "gpt",
  "tui": {"submit_key": "enter"},
  "models": {
    "gpt": {
      "provider": "codex",
      "id": "gpt-6-astra",
      "reasoning_effort": "low"
    }
  },
  "agent": {
    "tool_timeout": "1m",
    "output_limit": 32000,
    "max_rounds": 50
  },
  "compaction": {
    "max_characters": 0,
    "prompt": "Summarize goals, decisions, work completed, verification, and remaining work. Persistent Starlark state survives compaction."
  }
}
```

JSON must contain one object. Field names use their documented, case-sensitive
spelling (profile names remain case-sensitive map keys). Unknown fields, duplicate
keys, trailing data, and
invalid settings are errors. If models exist but `default_model` is missing or
empty, CPE selects the first profile declared in JSON and persists that choice.
An empty catalog has no default; add a model profile before starting a conversation.
Examples are in [examples/](examples/README.md).

Supported API-key providers are `openai` (Chat Completions, including compatible
servers), `responses`, `anthropic`, and `gemini`. Ordinary API-key profiles require
`api_key_env` and may set `base_url`, `max_output_tokens`, and `temperature`.
`reasoning_effort` and `/reasoning` are available for every provider, including
Chat Completions profiles using compatible endpoints. Labels are `none`, `minimal`,
`low`, `medium`, `high`, `xhigh`, `max`, `adaptive`, and `disabled`. Gai translates
the setting for each provider; supported labels depend on the adapter/model.
Anthropic supports `adaptive` and `disabled` thinking modes. An empty setting
omits the option. Codex uses its fixed endpoint and rejects the API-key, base URL,
output-token limit, and temperature settings.

CPE stores all subscription credentials in `~/.cpe/auth.json`, keyed by service:
`openai-codex` for OpenAI OAuth, `opencode-go` for its API key, and
`github-copilot` for GitHub/Copilot tokens. Credentials are plaintext protected by
file permissions, not encrypted. Ordinary API-key profiles still use environment
variables. CPE does not read or modify Pi's or other applications' logins.

Requests reload saved credentials and refresh expiring tokens. Login and refresh
update `auth.json` atomically with mode 0600, preserving other service
entries. The shared `auth.json.lock` coordinates CPE processes and releases on
exit or crash. The lock file remains on disk; do not remove it while CPE is
running. Credential symlinks and malformed/duplicate-key documents are rejected
without overwriting them. Tokens never enter config or conversation files.
Canceled saves leave the file unchanged, except that a completed token rotation
is committed before returning cancellation so its replacement cannot be lost.

Both replies and compaction use streaming Codex requests. Truncated streams fail
without committing partial output. Model requests, token exchanges, and refreshes
are not automatically retried. Device authorization is polled until approved,
canceled, denied, or expired. If credentials are rejected, use `/login` again.

## GitHub Copilot

Use `/login` → **GitHub Copilot · device code**, or `/login copilot`. Open the
GitHub URL shown, enter the device code, and approve GitHub Copilot access.
Escape cancels. CPE stores the `github-copilot` entry privately in `~/.cpe/auth.json` and
renews short-lived Copilot tokens before requests. Login instructions and tokens
never enter the conversation. No Copilot CLI, editor login, or API key is required.

Login queries your account's Copilot model catalog and adds
`github-copilot/<model-id>` profiles. Choose one with `/model`; your active model
and existing default stay unchanged until you select a model. Existing profiles
and settings are preserved. Repeat login to discover newly available models.
Only selectable, enabled models supporting tools, a supported API endpoint, and
usable context limits are imported. Organization policies and subscription access
still apply; CPE does not enable restricted models automatically.
Older catalog entries without endpoint metadata use Chat Completions. The
imported `copilot_streaming` capability enables streaming only when the catalog
supports it; other Copilot models return their response when generation completes.

Profiles use `"credential": "github-copilot"` with `openai`, `responses`, or
`anthropic` as the wire protocol, as reported by Copilot. Omit `api_key_env` and
`base_url`: CPE uses the saved Copilot credential and GitHub's subscription API
host. Initial budgets are capped at 128,000 input and 16,384 output tokens with
output room reserved; reasoning defaults and dollar prices are left unset.
GitHub.com accounts are supported, including organization subscriptions hosted
there. Custom GitHub Enterprise hosts are not yet supported.

Authentication or access failures identify `github-copilot`. Use `/login copilot`
again for revoked GitHub credentials; requests are not automatically retried.
Discovery/import failure preserves prior credentials. If the final credential
save fails after import, retry login to finish connecting the imported profiles.
The first conversational request for each prompt is marked user initiated;
summaries and subsequent tool continuations are marked agent initiated.
This integration follows [GitHub's device flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow)
and the [Copilot token exchange](https://github.com/microsoft/vscode-copilot-chat/blob/main/src/platform/authentication/node/copilotTokenManager.ts);
service compatibility is tested with local fixtures, not guaranteed by GitHub.

## OpenCode Go

Use `/login` → **OpenCode Go**, or `/login opencode-go`. Sign in at
[OpenCode](https://opencode.ai/auth), subscribe to Go, and paste the API key into
the masked input. Enter saves its `opencode-go` entry privately to `~/.cpe/auth.json`; Escape
cancels. It never enters the prompt composer, config.json, or session history.
The public model catalog cannot verify the key: the service checks it on the
first model request.

If that request returns `opencode-go authentication (401)`, use
`/login opencode-go` again with the key from your OpenCode account. Saving a key
does not establish that the service accepts it. OpenCode profiles use OpenCode's
saved key and endpoints; `openai`, `responses`, and `anthropic` in those profiles
describe API formats, not the account supplying authentication. Older CPE versions
labeled these failures with the API format (for example, `openai authentication`).

Login adds `opencode-go/<model-id>` profiles to config.json. Select one with
`/model`; no restart is needed. Your existing profiles, custom settings, active
model, and default model stay unchanged. Repeat login to import newly available
models or replace the saved key. A failed catalog fetch preserves your setup;
if saving the key fails after import, the added profiles remain signed out and
you can retry.

CPE starts from the [Go model endpoint](https://opencode.ai/zen/go/v1/models) and
looks up metadata exclusively in the `opencode-go` section of
[models.dev](https://github.com/anomalyco/models.dev). The full catalog covers many
providers; entries under Zen (`opencode`), OpenAI, or other providers never qualify
a model for Go. Deprecated models are excluded even if the Go endpoint still
lists them. This filter affects new imports; existing saved profiles are not pruned.

Go's documented endpoint corrections take precedence over catalog protocol
metadata. Chat Completions maps to `openai`, Responses to `responses`, and Messages
to `anthropic`. Unknown protocols, missing Go metadata, and models without tool
support are skipped. Imported profiles share
`"credential": "opencode-go"`; omit `api_key_env` and `base_url`, since CPE uses
fixed Go endpoints. You can add a profile manually using the protocol listed
in [OpenCode Go's documentation](https://opencode.ai/docs/go/#endpoints).

New profiles start with up to 128,000 input tokens and 16,384 output tokens,
bounded by model limits and with room for output. Adjust `context_window`,
`max_output_tokens`, and `reasoning_effort` in config.json as desired. Pricing is
left unset because subscription quota rates are not necessarily your bill; you
can configure cost estimates explicitly. CPE sends its own user agent and a
stable `x-opencode-session` header for replies and compaction, including after
resuming, branching, or switching profiles.

## Terminal controls

The default composer bindings are listed below. Set `"tui": {"submit_key":
"shift+enter"}` in `~/.cpe/config.json` and restart to submit with Shift+Enter
and insert newlines with Enter instead. `"enter"` (or omitting the setting)
uses the defaults. Ctrl+J always inserts a newline. The chosen submit key also
runs slash completions; pickers and private API-key input always use Enter to
confirm. Choose `"enter"` if your terminal cannot distinguish Shift+Enter.

| Key or command | Action |
| --- | --- |
| Enter | Send the prompt when idle |
| Shift+Enter or Ctrl+J | Insert a newline |
| Shift+arrows / Ctrl+G | Select composer text / select all |
| PgUp / PgDn | Scroll the conversation |
| F4 | Toggle normal mouse selection / CPE mouse-wheel scrolling |
| Esc or Ctrl+C from the conversation while busy | Cancel the current operation and wait for cleanup |
| Ctrl+C while idle, `/quit` | Exit |
| `/help` | Show commands |
| `/login` | Choose a login provider |
| `/login openai` | Sign in to OpenAI in the browser |
| `/login opencode-go` | Save a Go API key and import model profiles |
| `/login openai device` | Sign in to OpenAI with a device code |
| `/login copilot` | Sign in to GitHub Copilot and import account models |
| `/model` or `/model NAME` | Pick or switch to a profile from config.json |
| `/reasoning` or `/reasoning LEVEL` | Pick or set reasoning effort for the active provider |
| Ctrl+S in `/model` or `/reasoning` | Apply the highlighted choice and save it as the default |
| `/reasoning provider` | Omit reasoning effort and let the provider choose |
| `/reasoning default` | Restore the active profile's configured effort |
| `/theme` or `/theme NAME` | Pick or switch themes and save the selection |
| `/usage` | Show exact session token totals, estimated cost, and context budget; Escape returns to the conversation |
| `/skill:NAME [arguments]` | Invoke an installed skill |
| `/session` | Show the JSONL path |
| `/session new` | Start a new conversation with saved defaults |
| `/session list` | Pick a saved session in this directory to resume |
| `/session rename TITLE` | Rename the current session |
| `/tree` | List saved checkpoints |
| `/branch ID` | Continue from a checkpoint, preserving other branches |
| `/compact` | Summarize model context while retaining REPL state |
| `/queue` / `/queue clear` | Inspect pending prompts/commands and failures / discard pending work without stopping active work |
| `/queue steer` | Pick a queued prompt to send as steering now |
| `/prompt-mode` or `/prompt-mode queue\|steer` | Choose how prompts submitted during work are delivered |
| Ctrl+S in `/prompt-mode` | Apply the highlighted mode and save it for future conversations |

Shift+Enter requires a terminal that reports modified keys (such as Kitty,
Ghostty, or iTerm2 with its extended keyboard protocol enabled). CPE requests
this support automatically. If your terminal sends Shift+Enter as plain Enter,
use Ctrl+J to insert a newline instead.
Selected composer text uses the theme's selection colors. Typing, pasting, or
inserting a newline replaces the selection, including while drafting during work.

To copy conversation text, **click and drag normally**, then use your terminal's
Copy action; use its Paste action to paste into the composer. CPE leaves mouse
selection to the terminal by default. Press **F4** to enable CPE's mouse-wheel
scrolling, and F4 again to return to normal selection. While mouse capture is on,
use your terminal's selection override (commonly Shift+drag). The footer names
the action F4 will enable, where space permits.

To start with mouse-wheel scrolling enabled, set `"mouse_mode": "scroll"` inside
the `"tui"` object in `~/.cpe/config.json`. `"select"` is the default. F4 changes
only the running TUI and works during generation, pickers, and login. In selection
mode, wheel behavior is controlled by your terminal; use PgUp/PgDn to scroll CPE.

The conversation displays streamed text provisionally. Output follows the bottom
while you are there. Scroll up with PgUp (or the wheel in mouse-scroll mode) to read earlier
messages without being pulled back by new output, completion, or cancellation.
Scroll back to the bottom to resume following. Submitting a new prompt shows the
latest conversation. Transcript tabs display as four spaces so wrapping and
scrolling match the rendered rows; saved text retains its original tabs.
Completed assistant
messages and tool results are saved before the next operation. You can type,
edit, paste, and use the configured newline key to draft your next message while
the agent works.
Pressing the configured submit key during work **queues the prompt by default**.
You can submit several prompts; they run in order after the active operation.
Typing alone never sends or queues a draft.

Use `/prompt-mode` to pick **queue** or **steer**. Enter applies the choice to this
conversation; Ctrl+S also saves `tui.prompt_mode` in `~/.cpe/config.json` for future
conversations. `/prompt-mode steer` and `/prompt-mode queue` change only the
current conversation. This does not change your submit/newline bindings: with
`"submit_key": "shift+enter"`, Shift+Enter queues or steers and Enter adds a newline.

In steer mode, submitting cancels the active generation or tool execution, waits
for it to settle, and sends your new direction. `/queue steer` lets you choose a
specific queued prompt to promote immediately; the others retain their order.
Several steering prompts submitted while cancellation settles are sent together.
Incomplete streamed assistant text is marked **interrupted by steering (not saved)**
in the TUI; it is excluded from the journal and subsequent requests. Accepted tool
calls remain saved, with partial output/images and cancellation results reconciled
before steering input is appended. Partial Starlark state is preserved exactly
as described below. Login and branch restoration settle before steering runs. Slash completion offers the same
commands and user-invocable skills during work as when idle. Help, usage, the
session path, completed checkpoints, and theme selection work immediately. Model
and reasoning choices take effect at the next generation boundary.

Skills follow the selected prompt mode. Explicit `/compact`, `/branch ID`, and
login flows wait in the FIFO queue until the current operation settles. The status shows the
queue count and next command. `/queue` lists pending commands and pauses automatic
dispatch while you inspect them, even if the active operation finishes. Leaving
the queue view resumes FIFO dispatch; closing a popup or picker over it keeps the
queue paused. Choosing a prompt with `/queue steer` leaves the view and sends that
prompt first. New queue-mode prompts join the held queue; explicit steer-mode
submissions also leave the view and steer immediately. `/queue clear`
discards pending commands without canceling active work. Canceling active work with Esc/Ctrl+C
also clears the queue. `/quit` discards the queue and
exits after cleanup. Provider failures do not retry commands or discard later
explicitly queued commands. Pending commands live only in memory until executed;
exiting does not save them. Queued skills are resolved when they run.
Escape closes `/usage`, `/tree`, or `/queue` and returns to your conversation
reading position, preserving your draft and any running or queued work. If a
completion popup or picker is open, Escape dismisses that first. Ctrl+C still
cancels active work (or quits when idle); pending saves and private login input
retain their own Escape behavior.

Failed operations leave a persistent error indicator, with full errors in `/queue`,
even after later commands succeed. These errors stay in the TUI until exit;
`/queue clear` clears pending commands only. They are not added to model context.
Default-save errors also remain inspectable, distinguishing a failed save from
a saved setting that could not be applied. Canceling a save before it commits
does not add an error.

Pickers open immediately; queued operations wait while a picker or private login
has focus, or a default save is pending. A selected login flow starts after agent
work ends, and private credential input keeps its separate routing. New drafts
survive queued work, completion, errors, and cancellation. `/tree` uses the latest
completed-operation snapshot; `/usage` receives live usage updates.
Canceling private key entry also discards the remaining command queue.

Starlark results show the first 20 terminal rows per result, followed by a count
of hidden rows. The limit includes wrapped lines and image placeholders across
all blocks. The model and JSONL session keep the complete tool result (subject to
`agent.output_limit`); `/session` shows its file path. Assistant replies and
Starlark source code are displayed in full.

Start a draft with `/` to see a small command popup above the composer. Keep
typing to filter, use Up/Down to select, Tab to complete without running, or
the configured submit key to run the selection. `/branch` completion leaves room for a checkpoint ID.
The popup uses the active theme and shows up to five commands, scrolling as you
move through the list. It shrinks or hides in very short terminals.

While idle, Escape closes the popup without clearing your text and keeps it dismissed for
that draft. Remove the leading slash or send/clear the draft to enable it again.
After dismissal, an unrecognized slash prefix such as `/tmp` can be sent as
ordinary prompt text; recognized commands still work. Slashes inside prose or
paths, multiline input, and command arguments do not trigger the popup.
During agent work, completion offers the full catalog. In the conversation,
Escape or Ctrl+C cancels work and keeps the draft. Over a read-only panel such as
`/usage`, Escape dismisses completion first, then closes the panel. Inside the model picker, Escape closes the picker;
Ctrl+C cancels active work (or quits when idle). The footer shows the current controls.

## Skills

CPE discovers [Agent Skills](https://agentskills.io/specification) at startup in
`~/.agents/skills` and `./agents/skills`, relative to the current working directory.
Each immediate child directory contains a `SKILL.md`; symlinked directories work.
Project skills override global skills with the same name. Invalid files produce
warnings and are skipped; an invalid project override also hides its global copy.
Restart CPE after changing skill metadata or installing a skill.

For example, `./agents/skills/review/SKILL.md`:

```markdown
---
name: review
description: Review code changes for correctness and missing tests.
---
Read the requested diff, inspect relevant code and tests, and report actionable
findings. Resolve any supporting references relative to this skill directory.
```

Type `/skill:` to browse installed skills, then filter by name. Tab completes the
command so you can add arguments, such as `/skill:review staged changes`; the configured
submit key invokes it. This also works with `cpe --prompt '/skill:review staged changes'`.

By default both you and the model can invoke a skill. CPE supports these
[invocation-control extensions](https://code.claude.com/docs/en/skills#control-who-invokes-a-skill)
as top-level YAML booleans:

| Frontmatter | Slash command | Automatic model discovery |
| --- | --- | --- |
| Neither flag | Available | Available |
| `disable-model-invocation: true` | Available | Hidden |
| `user-invocable: false` | Hidden and rejected | Available |
| Both restrictions | Hidden and rejected | Hidden |

The model sees only names, descriptions, and paths for model-invocable skills.
An explicit command records your input and instructions to read the selected
`SKILL.md` through `starlark_repl`. The model reads references and runs scripts
through the same REPL as needed. Bodies are not added to the initial system
prompt, and no extra model tool is registered. Existing host-call durability
covers those reads and executions, so restoring a session does not repeat them.

Invocation controls govern discovery and explicit commands; they are not file
access restrictions. Arguments remain ordinary user text. CPE does not implement
template substitution, inline shell expansion, `allowed-tools` enforcement, or
client-specific execution modes from other skill hosts.

## Models and usage

Model and reasoning pickers use Up/Down, Enter to apply, and Esc to cancel.
In `/model`, type or paste into the **Filter** field to fuzzy-match profile names,
providers, and model IDs. Matches are ranked as you type; Enter applies the
highlighted match immediately. Backspace edits the query and Ctrl+U clears it.
Ctrl+S saves the highlighted match as the default. With no matches, neither key
selects a model. The query stays out of your prompt draft and conversation history,
and starts empty each time you open the picker.
For example, `/model sol` switches to your `sol` profile and `/reasoning high`
changes its reasoning effort for subsequent replies and compaction. The header
shows the active profile and effort. Recognized levels are `none`, `minimal`,
`low`, `medium`, `high`, `xhigh`, `max`, `adaptive`, and `disabled`; model support varies.
`/reasoning default` restores the profile's configured value, or lets the provider
choose if no value is configured. `/reasoning provider` always omits the option,
even when the profile has a saved effort. This differs from the explicit `none` level.
Switching to a different profile uses that profile's settings. A failed switch
keeps the active profile. Conversation and Starlark state are preserved.
Slash selections apply only to the current conversation; config.json is unchanged.
Restart/resume uses the configured default or `--model`.

You can also submit `/model` or `/model NAME` while the agent is working. All slash
commands remain available during work; unsubmitted drafts remain intact. The
current generation finishes and any returned tool calls execute once. The next
generation receives their results using the selected model/provider. A queued
choice appears in status; a later choice replaces it. If no further generation
is needed, the choice applies when work finishes. Escape closes an open picker;
Ctrl+C cancels the active operation. In the conversation, Escape still cancels work; read-only panels close first.

Thinking, signatures, and other assistant replay fields are attributed to the
exact provider, model ID, and configured service. Requests filter incompatible
fields without changing saved history, so A→B→A restores A's compatible thinking.
Older records with no origin metadata keep their public content but omit
unattributed replay fields. Tool results and interpreter state remain intact.

Anthropic's [preserved-thinking rules](https://platform.claude.com/docs/en/build-with-claude/preserved-thinking)
do not impose a blanket one-way switch ban: incompatible model thinking is dropped,
while a changed earlier prefix can invalidate otherwise compatible signatures.
CPE also checks the original system/tools/history prefix before forwarding its own
Anthropic thinking, including after restart or compaction. Gemini's GenerateContent
adapter supplies Google's [documented imported-call marker](https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures)
when a transferred tool call lacks a signature. Model-specific modality/context
limits still apply; a smaller configured context budget may require compaction or
a larger-budget selection.

In the **`/model` picker**, highlight a profile and press **Ctrl+S** to save it as
the default model and apply its configured settings to the current conversation.
In the **`/reasoning` picker**, **Ctrl+S** saves and applies the highlighted effort
for the selected model (including a pending model switch); it does **not** change
which model is the default.
**Enter** in either picker applies the choice only to the current conversation.
Ctrl+S in the composer has no action.
The **provider** choice removes `reasoning_effort`, allowing the provider to choose.
New starter profiles omit this option. Escape closes either picker without changes,
and the current draft is preserved. You can save a default model during agent work;
the conversation switch waits for the next generation boundary, as with Enter.
Reasoning selection also works during generation and takes effect at the next
generation boundary. When a model switch is pending, reasoning edits target that
selected profile. Ctrl+S in its reasoning picker saves that profile's effort
without changing the default model.
During a save, Escape or Ctrl+C cancels the save if it has not committed yet,
preserving the draft and letting any agent work continue. A completed config write is retained; CPE reports whether
its selection was applied. Terminal shutdown cancels and joins pending saves.
Writes preserve unrelated settings, file permissions, and config symlinks, and
serialize with model imports. A write failure leaves the active selection alone;
if provider setup fails after a successful save, CPE reports that the default was
saved but could not be applied.

Each profile can also configure a working input-token budget and prices in USD
per million tokens. For example, these settings use illustrative rates:

```json
{
  "context_window": 272000,
  "cost": {
    "input": 2,
    "output": 10,
    "cache_read": 0.2,
    "cache_write": 2.5,
    "long_context": {
      "above_input_tokens": 272000,
      "input": 4,
      "output": 15,
      "cache_read": 0.4,
      "cache_write": 5
    }
  }
}
```

Add those fields to a model profile. `context_window` is the preferred **input**
budget, which can be lower than the provider's actual context limit to control
cost. Leave room for output when choosing it. CPE compacts at approximately 90%
of this budget (244,800 for 272,000). Estimates include system instructions,
tools, and dialog; they use UTF-8 bytes/3 plus framing, calibrated upward from
the last reported input count. They are not exact provider tokenizer counts or
a guarantee against a pricing cutoff. Requests still estimated over budget are
stopped before generation, including compaction requests. An oversized prompt or
switch to a much smaller model may require choosing a larger budget to compact.

`cost` is optional. When supplied, all four rates are required; zero explicitly
means free. `long_context` is optional and replaces all rates for a request whose
**total input**, including cached tokens, exceeds `above_input_tokens`. Rates are
saved with each request, so later config changes do not reprice history. These
are local estimates, including API-equivalent estimates for subscription logins;
they do not query your account bill or model catalog.

The TUI shows uncached input, output, cache reads, cache writes, and their sum.
Cached tokens are counted once per request; repeated use on later requests still
counts as consumption. Provider-reported output includes reasoning where exposed
by the adapter. The pinned Gemini streaming adapter reports candidate output
without a separate thought-token count. The context estimate describes the active
working context, while token totals sum consumption across every call, including
compaction and inactive branches. Totals survive resume and model changes.
`/usage` shows exact numbers; the footer abbreviates large counts. `+` means some
token usage is unavailable, and `+?` marks incomplete cost estimates. Old sessions
have no historical usage to recover; accounting starts with new requests.

```sh
cpe --model default
cpe --sessions
cpe --continue
cpe --resume 20260923T020000_SESSIONID
cpe --resume /absolute/path/session.jsonl --branch CHECKPOINT_ID
cpe --prompt 'Inspect the README and describe the project'
```

An ordinary `cpe` launch always starts a **new session**. Inside the TUI,
`/session new` starts another, and `/session list` opens a picker of sessions for
the current working directory, newest first. Entries show the session title
(or first prompt label if untitled), activity time, and filename. Use arrows and Enter to resume;
Escape closes the picker. `/session` alone still shows the active file path.

The session title appears in the header and picker. On the first accepted prompt,
CPE asks the active model to generate a short title alongside the conversation
response. This is a separate, tool-free request using that profile's settings;
its reported tokens and cost are included in `/usage`. It receives at most the
first 4,000 characters of your initial input, without conversation instructions.
The request has a 30-second timeout and settles before the turn finishes.
If it fails or is canceled, the conversation remains usable; title generation
is not retried automatically. Use `/session rename Investigate Copilot auth` to
name or rename it manually (1–120 characters, one line). A rename submitted
during work waits in the operation queue, so it takes precedence over the initial
generated title. Renaming before the first prompt skips automatic generation.
Titles survive restart, compaction, and branching and never enter model history.

Filenames use a UTC creation timestamp followed by a cryptographically random
base32 identifier (at least 128 bits), for example `20260926T153000_<ID>.jsonl`.
This keeps file identities unique; the identifier is not derived from your text.
Titles are saved as JSONL metadata. Renaming leaves the filename and session ID
unchanged.


Session changes wait for active and earlier queued work. Prompts queued after a
switch run in the destination session. Creating or selecting a session leaves
queue inspection. The destination's conversation, usage, and Starlark state are
restored without repeating host effects; a failed resume leaves the current
session usable. Selecting the current file does nothing. New and resumed sessions
use saved model/reasoning and prompt-mode defaults; unsent drafts, theme, terminal
preferences, MCP connections, and the startup skill catalog stay available.

Explicit `--continue` selects the most recently modified session in the current directory.
`--resume` requires an existing file or filename without its `.jsonl` suffix.
A session's working directory is fixed; start CPE in that directory to resume it.

## Themes

Appearance lives separately in `~/.cpe/themes.json`. `cpe --init` creates it
without overwriting existing files. These themes ship inside CPE:

| Theme | Appearance |
| --- | --- |
| `desktop` | Derives light/dark colors and an accent from the OS appearance |
| `terminal` | Inherits the terminal's foreground, background, and ANSI palette |
| `light` | Neutral light palette |
| `dark` | Neutral dark palette |
| `nord` | Nord's cool blue and gray palette |
| `dracula` | Dracula's purple, pink, and cyan palette |
| `gruvbox` | Gruvbox's warm dark palette |

Use `/theme` to open the picker (Up/Down, Enter to apply, Esc to cancel), or
switch directly with `/theme nord`. The picker includes custom themes and marks
the current one. Selections apply immediately and are saved in `themes.json`
for future sessions. Invalid selections or save failures retain the current theme.

You can also select a built-in by editing the file without copying its colors:

```json
{
  "active": "nord"
}
```

Changes apply live within a second. The fixed palettes are CPE role mappings
based on [Nord](https://www.nordtheme.com/docs/colors-and-palettes/),
[Dracula](https://spec.draculatheme.com/), and
[Gruvbox](https://github.com/morhetz/gruvbox). No downloads are needed.

A missing file defaults to `desktop`, using `source: "system"`. Linux reads
light/dark preference and an optional accent from the standard
[XDG Settings portal](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Settings.html).
This is the same API on Ubuntu, Debian, Fedora, and other distributions; it needs
a running portal backend, not a particular distribution or theme manager.
macOS reads AppKit's [effective appearance](https://developer.apple.com/documentation/appkit/nsapplication/effectiveappearance)
and [accent color](https://developer.apple.com/documentation/appkit/nscolor/controlaccentcolor)
through the built-in `osascript` bridge, without CGO or a developer SDK.

These are OS preferences, not a complete shared desktop palette. CPE derives its
neutral light/dark surfaces from the mode and uses the accent for headings and
selection, adjusting contrast for readability. A missing accent uses the default
accent. Missing services, no mode preference, unsupported OSes, and SSH sessions
fall back to terminal inheritance. Probes are bounded and run off the UI loop;
changes are picked up by the one-second reload cycle.

Use `/theme terminal` for the emulator's full current palette independently of OS
preferences. This works across operating systems and over SSH/tmux. If the
emulator follows a custom desktop theme, this follows its colors too; whether
changes reach already-open windows depends on the emulator's theme integration.

Customize a built-in by creating a named theme with its palette as `source`:

```json
{
  "active": "work",
  "themes": {
    "work": {
      "source": "nord",
      "colors": {
        "assistant": "$accent",
        "error": "#ef6464",
        "background": "default"
      },
      "bold": true,
      "italic": false,
      "input_height": 3
    }
  }
}
```

Sources are `system`, `terminal`, `file`, `light`, `dark`, `nord`, `dracula`,
`gruvbox`, and `auto` (an alias for `system`). User definitions take precedence
over built-ins with the same name. The starter file defines `desktop` with
`source: "system"`.

For an exact externally supplied palette, use `source: "file"` and `palette_file`
pointing to a JSON object of named `#rrggbb` colors. Paths may be absolute, start
with `~/`, or be relative to `~/.cpe`; symlinks work. `foreground` and `background`
are required, with `fg`/`bg` or `color7`/`color0` accepted as aliases. See
[palette.json](examples/palette.json). CPE never runs hooks or user-supplied code.
The earlier `omarchy` source and automatic TOML lookup have been removed. Change
old definitions to `source: "system"`, or export a JSON palette and use `file`.
Only `file` accepts `palette_file`.

Color roles are `foreground`, `background`, `accent`, `muted`, `border`, `error`,
`user`, `assistant`, `tool`, `selection_foreground`, and `selection_background`.
Values accept `#rrggbb`, ANSI indices as strings (`"0"`–`"255"`), `"default"`
for terminal inheritance, or `$palette_key`. References use the source palette
before overrides. All hex keys in a palette file can be referenced; every source
provides the base semantic colors and `color0`–`color15`.
`"background": "default"` preserves the terminal background/transparency.

`bold` controls headings and selected items; `italic` controls muted text.
`input_height` sets the preferred editor height in rows (1–20), limited to fit
small terminals. Font family and size are controlled by your terminal emulator;
CPE inherits them. RGB colors are reduced to the terminal's supported color depth
when necessary; `NO_COLOR` is respected.

Reloads preserve drafts, cursor, scroll, conversation and REPL state. Invalid
edits keep the last valid theme and show a footer error; fixing the file clears
it. Startup errors fall back to terminal colors. `/theme` marks the active theme
in the picker; after a selection, the status shows its name and source.
Headless commands do not load theme files. See
[examples/themes.json](examples/themes.json) for custom and desktop integrations.

## Starlark and Dyson

Globals, functions, collections, binary data, and native Dyson response objects
persist across chunks. The supported modules are `os.star`, `glob.star`,
`json.star`, `re.star`, `requests.star`, `subprocess.star`, and `time.star`.

```python
load("os.star", "os")
load("requests.star", "requests")
load("subprocess.star", "subprocess")

f = open("README.md")
readme = f.read()
f.close()

fd = os.open("notes.txt", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o644)
os.write(fd, "Hello from Starlark\n")
os.close(fd)

response = requests.get("https://example.com")
print(response.status_code, response.text[:200])

result = subprocess.run(["git", "status", "--short"], capture_output=True, text=True)
print(result.stdout)
```

Dyson's `open` supports reading; writes use the `os` descriptor functions.
Prefer closing file handles within each chunk. Replay reconstructs handles without
opening files. On **new** host I/O, surviving handles reattach by path at their
saved position, without repeating creation or truncation. Reattachment observes
the current filesystem; it does not resurrect deleted files or preserve inode
identity across process restarts.
Filesystem paths retain normal host permissions and semantics; the working
directory is a base for relative paths, not a sandbox. Commands cannot inherit
terminal input or overwrite the TUI through inherited output streams.
Environment mutation, process signaling, and the `tempfile`, `pwd`, `grp`, and
`shutil` modules are not exposed in this first version.

## Durability

Sessions are private, exclusively locked files under `~/.cpe/sessions/`. Each JSONL
entry contains `id`, `parentId`, `type`, `timestamp`, and typed `data`. Entries form
an append-only tree inspired by Pi's session layout. The last entry is the active
head. Branching appends a head marker pointing to a checkpoint, leaving all other
branches in the file.

A successful REPL evaluation records:

1. The source and tool-call ID (`eval_start`).
2. A synced intent before each host call (`host_call`).
3. Its synced return value and error, before Starlark receives them (`host_result`).
4. The printed output and committed outcome (`eval_end`).

The journal intercepts Dyson's filesystem operations (including individual file
reads/writes), HTTP, commands, and clocks, plus injected tool calls. Values use
explicit JSON representations: binary data is base64, file metadata is typed,
and error identities such as EOF and not-found survive replay. Dyson reconstructs
its native Starlark objects from these saved host results; pure interpreter work
is reevaluated.

Restoration runs committed source against the recorded results. **It never calls
the live host closures.** Missing, extra, or reordered calls, changed arguments,
or changed output stop restoration. The interpreter/Dyson contract is versioned;
incompatible sessions are rejected. A malformed complete JSONL record is an error;
an unterminated final record is discarded as an interrupted append.

Ordinary failed, timed-out, or canceled chunks roll back interpreter state by
replaying preceding committed chunks. **Steering preserves partial state** instead:
the completed outcome records an instruction count and whether execution stopped
before an opcode or inside a host call. Replay executes to that exact boundary
using recorded host results, preserving globals and mutations without repeating
side effects. Printed output, emitted images, and captured subprocess output are
returned along with the steering-cancellation notice. Subprocess builtin outcomes
are recorded after their timeout checks, so replay cannot turn an original timeout
into success even when the host runner succeeded. Context-aware host calls
receive cancellation while retaining their own deadlines; other builtins must
finish before execution can stop at the next opcode.

External effects already performed remain in either case. After a crash, an
unfinished chunk has no exact recorded stop point: it rolls back and its tool
result is reconciled as a failure. A crash between a host effect and its recorded result leaves an uncertain
outcome; CPE reports it and **never automatically retries that call**. Check the
external state before asking the agent to repeat interrupted work.

Compaction appends a summary used for subsequent model context. It retains all
REPL source and host results needed for restoration. A positive per-model
`context_window` enables token-budget compaction. Otherwise, a positive
`compaction.max_characters` retains the legacy serialized-dialog size trigger.
With both zero, automatic compaction is disabled. Compaction runs at most once
per user turn. Manual `/compact` is always available.

## Internal SDK

`internal/agent.Open` accepts configuration, a `gai.Generator`, a session store,
and `[]repl.Tool`. Each tool has a name, description, JSON Schema, and a
context-aware Go callback returning JSON-serializable data. The callback becomes
a function loaded with `load("tools.star", "name")`; parameters can be passed as
keyword arguments or one dictionary. Both inputs and results are journaled.
Only `starlark_repl` appears in the model's tool schema.

### MCP tools and images

Add `mcp_servers` to `~/.cpe/config.json` to connect servers at startup:

```json
{
  "mcp_servers": {
    "local": {"command": "my-mcp-server", "args": ["--stdio"], "env": {"MODE": "development"}},
    "remote": {"url": "http://127.0.0.1:8000/mcp"}
  }
}
```

This is a configuration fragment; keep your model and agent settings. Each
server uses either a stdio command or a Streamable HTTP URL. Commands inherit
CPE's environment and working directory. Server names must be ASCII identifiers;
punctuation in tool names becomes underscores, and collisions are rejected.
Startup uses `agent.tool_timeout` for each connection. Legacy HTTP+SSE endpoints,
MCP OAuth, and dynamic catalog updates are not supported yet.

The agent receives descriptions and schemas for functions named
`mcp_<server>__<tool>`. Calls return the MCP envelope as a Starlark dictionary,
including `content`, optional `structuredContent`, `_meta`, and `isError`.
For example, assuming the local server advertises `search` and `screenshot`:

```python
load("tools.star", "mcp_local__search", "mcp_local__screenshot")
load("repl.star", "emit_image")

result = mcp_local__search(query="example")
if result.get("isError", False):
    print(result["content"])
else:
    print(result.get("structuredContent", result["content"]))

screen = mcp_local__screenshot()
for content in screen["content"]:
    if content["type"] == "image":
        emit_image(content)
    elif content["type"] == "text":
        print(content["text"])
```

`emit_image` attaches actual image blocks to the REPL tool result so a vision
model can inspect them. It also accepts `emit_image(data, mime_type="image/png")`
with a base64 string or raw Starlark bytes. PNG, JPEG, GIF, and WebP are supported,
with a 20 MiB total decoded-image limit per evaluation, independent of text
truncation. The TUI displays an image placeholder. Other MCP media, including audio
and resources, remain available as dictionaries; only images have a model-output
helper. Emitted images survive in the durable result even if later code fails.
Replay restores saved content without calling the server again.

SDK callers can use `internal/mcptools.Connect` with an MCP SDK transport and
pass `connection.Tools()` to `agent.Open`. Close the agent before its connections.

The SDK uses gai's `PrepareDialog`, `BeforeGeneration`, `AfterGeneration`,
`AfterTool`, and `GenerationChunk` hooks for compaction, bounds, persistence, and
streaming. Package `doc.go` files are the canonical behavior contracts.

gai is pinned to main commit `649ab82e7b0c`. Dyson is pinned to `b790356d9233` and
Starlarkx to its compatible dependency `40a94bc8c78e`; the newer Starlarkx HEAD
changes interfaces that Dyson does not yet implement.

## Verification

```sh
go fmt ./...
go vet ./...
go test ./...
go test -race ./internal/...
go run ./build lint
CPE_RUN_INTEGRATION_TESTS=1 go test ./internal/repl
CPE_RUN_INTEGRATION_TESTS=1 go test ./internal/mcptools -run TestConnect
```

The tests cover restart without host access, binary and large-integer values,
file/HTTP/command effects occurring once, interrupted calls, replay divergence,
failed-chunk rollback, tree branching, compaction, provider metadata, locking,
and torn JSONL writes. Dummy MCP servers cover discovery/pagination, no-argument
and parameterized tools, chained calls, text/structured/multimedia results,
schema/tool/protocol errors, and replay after disconnect. Responses wire tests
verify image delivery with and without streaming. Bubble Tea tests run the real event loop with keyboard
input and verify cancellation, resizing, multiline input, and persisted turns.

For a deterministic TUI in a real terminal without credentials:

```sh
go test -c -o /tmp/cpe-tui.test ./internal/tui
mkdir -p /tmp/cpe-tui-session
CPE_RUN_INTEGRATION_TESTS=1 CPE_TUI_TEST_DIR=/tmp/cpe-tui-session \
  /tmp/cpe-tui.test -test.run=TestTerminalHarness
```

Use `/login openai device` for a simulated login (completes after eight seconds), or
`/model api-fixture` to use the fixture without login. Test `/model` and `/reasoning`
pickers, or `/model alternate` and `/reasoning high` for direct changes. Ctrl+S
inside either picker saves the highlighted default to the fixture directory;
Enter applies it only to the conversation. Restarting the harness uses saved defaults. Send
`handoff`, select another model while it prepares its tool call, and verify the
final “Handled by” model and the journal: one old-model generation, one tool
execution, then a new-model generation.
Use `/login` → OpenCode Go (or `/login opencode-go`) and the dummy key
`fixture-go-key` to exercise masked entry and model import. This fixture takes
two seconds, supports cancellation, and never contacts OpenCode or saves a real key.

Use `/login copilot` for a simulated GitHub device flow. It displays `COPILOT-TEST`,
completes after three seconds, and imports `github-copilot/fixture` into the isolated
test configuration. Escape cancels; no GitHub request or credential storage occurs.
Select the fixture with `/model github-copilot/fixture`, then send `compute` to test
the normal REPL workflow after login.
Send `verbose` for 60 lines of Starlark output: the TUI previews 20 rows, while
the active file under `CPE_TUI_TEST_DIR/sessions/` retains all 60.
Send `scroll` for a long streamed response: scroll up while it runs, verify the
reading position stays put through completion, then scroll down to follow again.
Use PgUp/PgDn, or press F4 to capture the mouse wheel. Verify F4 releases capture
again so ordinary dragging selects text in the terminal, with the draft intact.
Send `compute`: the header should become `Testing persistent Starlark state`.
Run `/session rename My test session` and verify the header and `/session list`
picker show that title. Resume it and confirm the title survives without another
title request. Send `/session new`: the transcript and usage should clear and the
header should read `New session`.
Use `/session list` to resume the `compute` session, then send `restore` to verify
the saved variable. Restart the harness to verify it starts fresh; set
`CPE_TUI_TEST_SESSION` to a saved JSONL path to exercise explicit startup resume.
Send `slow` for a three-second REPL call and draft another message
while it runs; the draft stays unsent after completion. Send `wait`, type a draft,
and press Esc to verify cancellation preserves it. Set `CPE_TUI_TEST_SUBMIT_KEY=shift+enter` to exercise the alternate composer
binding. This harness also works inside tmux, where `capture-pane` provides the actual rendered terminal grid.
The harness creates isolated `review`, `publish` (user-only), and `background`
(model-only) skills beneath `CPE_TUI_TEST_DIR/agents/skills`. Type `/skill:` to test
completion, `/skill:review staged changes` to read a skill through the REPL, and
`/skill:background` to verify that direct user invocation is rejected.
During `handoff` or `slow`, submit `/skill:review first` and `/skill:publish second`;
verify both complete in order after the original turn and a new draft stays unsent.
During `wait`, verify all slash suggestions appear, `/usage` and `/theme` work,
and `/queue clear` drops pending commands without stopping the response. Queue a
skill again and press Ctrl+C: the active turn and pending skill should be canceled,
even while the queue panel is open. Escape closes the panel without canceling work.
Queue `/branch missing` followed by a skill during `handoff`; verify the skill runs
and `/queue` retains the branch failure. Queue a Go login followed by an OpenAI
login, then cancel the private key input; the second login must not start.
To check prompt delivery, submit several ordinary prompts during `handoff` and
inspect `/queue`. Let the active response finish: queued prompts should stay put
until you leave the view. Open and cancel `/queue steer` to verify they stay held,
then reopen it and promote the middle one; it should run first,
with the remaining prompts in order. Use `/prompt-mode` and Ctrl+S to save steer,
restart, then submit `wait` followed by `compute`: the partial stream is marked
interrupted and absent from the session JSONL. Send `steer-tool` and steer it with
`restore-partial`; it should print `[1, 7]`, including after restart. The fixture
writes one `steering-effect` line before interruption; replay must not add another.
The harness also reads `themes.json` from `CPE_TUI_TEST_DIR`; use an explicit JSON `palette_file`
to point at fixture palettes and test live edits without changing the desktop.

An optional real-provider smoke test uses the default profile in `~/.cpe`:

```sh
CPE_RUN_LIVE_TESTS=1 go test ./internal/agent -run TestLiveConfiguredAgent -v
```
