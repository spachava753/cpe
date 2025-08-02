# CPE (Chat-based Programming Editor)

CPE is a Go CLI that lets developers use LLMs to analyze, edit, and manage codebases directly from the terminal. It
supports multiple providers (OpenAI, Anthropic, Google Gemini), conversation persistence/branching, MCP client
capabilities, file operations, and shell execution.

Primary packages live under `cmd/` (Cobra CLI) and `internal/` (agent, storage, token utilities, MCP, etc.). See
@README.md for end-user usage and flag reference.

---

## Project structure and organization

- Root
  - `main.go` -> entrypoint invoking `cmd.Execute()` and initializes global slog JSON logger to `./.cpe.log`
  - `go.mod`, `go.sum` -> module and dependencies
  - `gen.go` -> sqlc codegen directive for DB queries
  - `README.md` -> user guide and command reference
  - `ROADMAP.md` -> future plans
  - `AGENT.md` -> this file
  - `.cpeconvo` -> SQLite conversation DB (runtime artifact)
  - `.cpe.log` -> structured JSON log file (runtime artifact; MCP stderr + internal debug)
- CLI (`cmd/`)
  - `root.go` -> root command, flags, execution pipeline, stdin/files/URLs ingestion, stream/no-stream logic
  - `conversation.go`, `conversation_tree.go` -> conversation helpers
  - `model.go` -> model listing/info
  - `tools.go` -> developer tools (overview, token count, related files, etc.)
  - `env.go` -> environment inspection command
  - `mcp.go` -> MCP related commands (init/list/info/call-tool)
  - `integration_test.go`, `conversation_tree_test.go` -> tests
- Internal packages (`internal/`)
  - `agent/` -> system prompt assembly, generator wrappers, streaming printers, input modality detection
  - `storage/` -> dialog (conversation) persistence using SQLite via `sqlc` generated queries; schema and queries in
    `schema.sql`, `queries.sql`, generated `queries.sql.go`
  - `mcp/` -> MCP client lifecycle, config parsing and helpers
  - `cliopts/` -> shared option utilities
  - `token/` -> token counting utilities (`builder/`, `tree/`)
  - `urlhandler/` -> HTTP(S) downloads and content typing
  - `version/` -> version getter
- Scripts (`scripts/`)
  - Utilities like `process_pr_comment.go` for CI/tooling
- Examples (`example_prompts/`) and prompt files (`prompt_1.txt`, `prompt_wk.txt`)

Conventions:

- All end-user commands are exposed via Cobra commands in `cmd/`.
- Core logic that should not be part of the public CLI surface resides in `internal/`.
- SQLite database file `.cpeconvo` is always used in the CWD.

---

## Build, test, and development commands

Prerequisites: Go 1.23+

Build/install:

- Build binary: `go build ./...`
- Install CLI: `go install github.com/spachava753/cpe@latest`

Run:

- From source: `go run . --help`
- Example: `CPE_MODEL=claude-3-5-sonnet go run . "List supported models"`

Tests:

- Run all tests: `go test ./...`
- Run with race: `go test -race ./...`
- Specific package: `go test ./internal/storage -v`

Codegen:

- SQLC: `go generate ./...` (see `gen.go`) -> requires `sqlc` v1.28.0

Linting/formatting (suggested):

- `gofmt -s -w .`
- `go vet ./...`
- Optional: `golangci-lint run`

Release (manual example):

- Tag and build via standard Go tooling or your CI. No dedicated release scripts in-repo.

---

## Code style and conventions

General Go practices:

- Keep public CLI-facing code in `cmd/` minimal; move logic to `internal/`.
- Prefer small, composable packages and functions; keep side effects explicit.
- Use context for cancellation/timeouts; propagate `ctx` from Cobra handlers.
- Handle errors explicitly; wrap with `fmt.Errorf("...: %w", err)` for context.
- Avoid panics in library code; return errors.
- Keep functions pure where possible; separate I/O from computation for testability.
- Keep CLI flags defined in `init()` of the corresponding command file; document defaults.
- For streaming vs. non-streaming, route through the generator wrappers in `internal/agent`.

Formatting/naming:

- `gofmt`/`goimports` enforced; idiomatic Go naming (ExportedNames for public, lowerCamel for internal).
- 100-120 char soft line limit.
- For initialisms, follow Go style: `ID`, `URL`, `API` (e.g., `MessageID`).

Documentation:

- Package docs in `doc.go` where needed; function comments in GoDoc style.
- For user docs, keep @README.md as the source of truth; reference it from commands where helpful.

Dependencies:

- Keep third-party usage minimal; prefer stdlib where practical.
- Pin with `go.mod`; update regularly and audit for security.

---

## Architecture and design patterns

High-level flow (root command):

1) Parse flags/env -> validate model presence (CPE_MODEL or --model).
2) Collect user input blocks from stdin, files, URLs. Detect modality and MIME via `internal/agent` + `mimetype`.
3) Prepare system prompt via `agent.PrepareSystemPrompt`, allowing override via `--system-prompt-file`.
4) Initialize LLM generator (`agent.InitGenerator`) with timeouts and optional custom base URL.
5) Wrap generator for streaming output (`StreamingPrinterGenerator`) or non-streaming (`ResponsePrinterGenerator`).
6) Construct `gai.ToolGenerator` and wrap with `agent.NewThinkingFilterToolGenerator` to filter thinking in top-level
   dialog while preserving it for tool execution.
7) Run conversation turn; persist messages to SQLite via `internal/storage` unless incognito.
8) Print last saved message id to stderr for automation.

Key patterns:

- Cobra for CLI composition.
- Ports/adapters around model providers via `github.com/spachava753/gai` abstraction.
- Repository-like storage layer with sqlc-generated queries.
- Tooling commands in `cmd/tools.go` expose analysis helpers (overview, token counts, related files).
- MCP integration encapsulated in `internal/mcp` and surfaced via `cmd/mcp.go`.
- Centralized structured logging with `log/slog` (JSON), writing to `./.cpe.log` from `main.go`.
  - On logger initialization failure, we fall back to a discard handler and emit a one-time warning on stderr.

Data storage:

- SQLite DB `.cpeconvo` with messages, parent-child relationships enabling branching; see `internal/storage/schema.sql`.

---

## Testing guidelines

- Use `go test ./...` regularly; keep tests deterministic and stateless.
- Unit tests should isolate packages (e.g., `internal/storage`, `internal/agent`).
- For DB tests, use temporary SQLite files; tests in `internal/storage/*_test.go` illustrate patterns.
- Prefer table-driven tests and subtests; use `testing.T` helpers.
- Use `github.com/stretchr/testify` for assertions where it improves clarity; avoid over-mocking.
- Keep I/O (network, filesystem) behind small interfaces to stub in tests.
- Avoid hitting real model providers in unit tests; test adapters with fakes.
- Naming: `foo_test.go`, functions `TestXxx`; avoid "should" in test names.

Running selective tests:

- `go test ./cmd -run TestName`
- `go test ./internal/storage -run Dialog`

Coverage (optional):

- `go test ./... -cover -coverprofile=cover.out && go tool cover -func=cover.out`

---

## Security considerations

Secrets and configuration:

- Never commit API keys; rely on env vars: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`. See @README.md.
- Optional `CPE_MODEL`, `CPE_CUSTOM_URL` may influence provider selection and endpoints.

Runtime:

- Validate and sanitize all file/URL inputs. Size limit enforced at 50MB per input in `cmd/root.go`.
- Network downloads time out (30s) and use content-type checks; avoid executing downloaded content.
- Follow principle of least privilege for filesystem access. The tool only writes `.cpeconvo`, `.cpe.log`, and
  user-requested file edits.
- Handle SIGINT/SIGTERM gracefully; partial outputs are saved and reported.
- Avoid `git push --force` in scripts; prefer `--force-with-lease` when necessary.

Dependencies:

- Keep dependencies updated; run `go list -m -u all` periodically and audit CVEs.

Privacy:

- Use `-G/--incognito` to avoid saving conversations locally.
- Be mindful that prompts and files may be sent to third-party model providers per your configuration.
- `.cpe.log` contains structured diagnostic logs (including MCP server stderr lines). Review and handle accordingly
  before sharing logs.

---

## Configuration

Environment variables:

- Required: at least one of `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`.
- Optional: `CPE_MODEL` (default model), `CPE_CUSTOM_URL` (custom API base URL).
- See `cpe env` for a dump of current environment values recognized by the tool.

CLI flags (selected):

- `--model, -m` select model
- `--input, -i` add files/URLs (can repeat or comma-separate)
- `--new, -n` start new conversation; `--continue, -c` resume by message ID
- `--incognito, -G` do not persist conversation
- `--system-prompt-file, -s` custom system prompt template
- `--timeout` request timeout (default 5m)
- `--no-stream` disable streaming output
- `--mcp-config` path to MCP config file

MCP configuration:

- Configure servers via `.cpemcp.json` or `.cpemcp.yml` in repo root or CWD; see @README.md MCP section.
- MCP server stderr is captured and logged to `.cpe.log` instead of printing to CLI stderr.
- Internal MCP diagnostics (tool filtering/registration) log via `slog` at info/warn levels.

Code generation and DB:

- Run `go generate ./...` to refresh sqlc outputs after editing `internal/storage/queries.sql` or `schema.sql`.
- The SQLite DB path is fixed to `.cpeconvo` in the current working directory.

---

## Build & Commands (quick reference)

- Build: `go build ./...`
- Test: `go test ./...`
- Lint: `go vet ./...` (and/or `golangci-lint run`)
- Generate sqlc: `go generate ./...`
- Run CLI: `CPE_MODEL=claude-3-5-sonnet go run . "Your prompt"`
- List models: `go run . model list`
- Tools: `go run . tools overview .`, `go run . tools list-files`, `go run . tools token-count .`

---

## Additional references

- @README.md for end-user docs and examples
- @ROADMAP.md for planned features
- `internal/agent/agent_instructions.txt` for the system prompt template used by default
