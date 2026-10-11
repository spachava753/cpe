# CPE

CPE is a terminal programming agent in development. Its CLI currently supports
managing agent skills, installing Agent Plugins, and signing in to model providers.
Model execution is not yet available in the CLI.

## Installation

On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/spachava753/cpe/main/install.sh | sh
```

The installer puts `cpe` in `~/.local/bin`; make sure that directory is on your
`PATH`. You can also download a binary from
[GitHub releases](https://github.com/spachava753/cpe/releases).
Git must be on your `PATH` to install or update bundles from repositories.

Use `cpe --help` to see commands and `cpe skills install --help`, for example,
to see a command's options.

## Authentication

Sign in with a ChatGPT subscription or save an
[OpenCode Go](https://opencode.ai/docs/go) API key:

```sh
cpe auth login openai
cpe auth login opencode
cpe auth logout openai
cpe auth logout opencode
```

For OpenAI, follow the printed browser link. For SSH or a browser on another
machine, use `cpe auth login openai --device` and enter the displayed code at
OpenAI's sign-in page. Device-code authorization may need to be enabled in your
ChatGPT security settings. Ctrl+C cancels login.

For OpenCode Go, paste your API key when prompted. Login saves the key but does
not verify your subscription.

Credentials are stored in `~/.cpe/auth.json` as plaintext. Do not share or commit
this file. On Windows, restrict access to the directory to your user account.
Logout removes the selected provider's local credentials; it does not revoke
remote sessions or API keys.

## Skills

Install skills from a GitHub repository using `owner/repo`, or use a full Git URL:

```sh
cpe skills install mattpocock/skills --skill writing-great-skills --scope project
cpe skills list
cpe skills update writing-great-skills --scope project
cpe skills remove writing-great-skills --scope project
```

Install requires `--scope project` or `--scope global`. Project skills go in
`./.agents/skills`; global skills go in `~/.agents/skills`. With project scope,
`--dir PATH` selects an exact custom directory instead.

Without `--skill` or `--path`, install includes all skills found in the repository.
Repeat `--skill NAME` to select skills by name, or use `--path DIR` for one exact
source-relative skill directory. These options cannot be combined. Existing
bundles are preserved unless `--override` is set.

Run `cpe skills update` to update all installed, tracked skills in both scopes.
Use `--scope` to restrict updates to one scope. When a named skill exists in both
scopes, specify its scope to update or remove it. Missing, untracked, and
unsupported skills are skipped during updates with a warning. Updates follow the
recorded Git repository's default branch.

Update skips bundles with modified or unverifiable contents. Use `--override`
only when you want to replace them from the source:

```sh
cpe skills update writing-great-skills --scope project --override
```

Replacement removes local changes and obsolete files. Skills installed by another
tool may also need `--override` on their first CPE update. Listing and removal work
for manually copied skills too.

CPE records project skill sources in `skills-lock.json`, or inside the selected
custom directory when using `--dir`. Keep this file for updates. Run skill
installation, update, and removal commands one at a time.

## Agent Plugins

Install native [Agent Plugins](https://agent-plugins.org/specification) packages
from Git repositories:

```sh
cpe plugins install stripe/ai --path providers/agent-plugins/plugin --scope project
cpe plugins list --scope project
cpe plugins remove stripe --scope project
```

Install requires `--scope project` or `--scope global`. Project plugins go in
`./.agents/plugins`; global plugins go in `~/.agents/plugins`. With project scope,
`--dir PATH` selects an exact custom directory instead.

Use `--plugin NAME` to select by manifest name or `--path DIR` for one exact
source-relative package directory. These options cannot be combined. `--override`
replaces an existing bundle, removing local changes and obsolete files.

Installation copies the bundle without running its code, connecting to MCP
servers, or signing in to services. Component problems are reported as warnings.

## Output

Skills and plugin commands print tables on a terminal and JSON arrays when stdout
is redirected. Use `--json` to request JSON on a terminal. Warnings go to stderr.
