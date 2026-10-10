# CPE

CPE is a small terminal programming agent built with Bubble Tea, gai, Starlarkx,
and Dyson. The model has one tool: a persistent `starlark_repl`. It uses Starlark
functions to read and write files, run commands, call HTTP services, and invoke
application-supplied tools or MCP servers.

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

These commands establish authentication storage. Model execution and automatic
token refresh will be wired separately.

## Skills

Install skills from a Git repository and update them from their recorded source:

```sh
cpe skills install mattpocock/skills --skill writing-great-skills --scope project
cpe skills update writing-great-skills --scope project
cpe skills update
cpe skills list
cpe skills remove writing-great-skills --scope project
```

Project skills live in `./.agents/skills` and are tracked in `./skills-lock.json`
(version 1). Global skills live in `~/.agents/skills`; their lock is
`$XDG_STATE_HOME/skills/.skill-lock.json` when `XDG_STATE_HOME` is set, otherwise
`~/.agents/.skill-lock.json` (version 3). These are the lock formats used by
[the skills CLI](https://github.com/vercel-labs/skills). Project records use its
folder-content SHA-256 algorithm; global GitHub records use Git tree hashes.
With `--dir`, CPE uses that exact skill directory and keeps a `skills-lock.json`
inside it, separate from the default project inventory.

`update` without names updates both scopes; `--scope` restricts it to one.
A named skill installed in both scopes is skipped with a warning unless a scope
is explicit. Missing and untracked skills are also warned about and skipped.
CPE updates GitHub and Git sources at their default checkout; records using other
source types or explicit refs are skipped. Updates replace local edits and remove
obsolete files, just like installing with `--override`.

Listing includes untracked skills. Removal also works for untracked skills and
removes the lock record when one exists. A malformed or unsupported lock file
blocks changes to its inventory rather than being discarded. A lock changed by
another writer during acquisition causes the operation to abort; retry to use its
new contents. Bundle changes and lock changes are published together with
rollback on failure; an error reports
any committed results or recovery files left by failed cleanup or rollback.
Results are JSON arrays when stdout is redirected; `--json` forces JSON on a
terminal. Warnings go to stderr.

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
