# CPE

CPE is a coding agent served over the Agent Client Protocol (ACP). `README.md`
covers configuration and user-facing behavior; `docs/` explains agent architecture
and behavior in detail.

## Project layout (depth 1)

```text
.github/      CI, releases, and issue templates
build/        Goyek developer tasks and repository-specific linting
docs/         Human-readable guides to agent architecture and behavior
internal/     Business logic
main.go       CLI entry point
go.mod        Go version, dependencies, and developer tools
README.md     User workflows and verification instructions
```

## Internal packages (depth 1)

```text
agent/         ACP serving, live sessions, and turn orchestration
auth/          Provider credential storage and authentication
bundlefs/      Transactional directory-bundle installation and recovery
cli/           CLI command assembly and dependency wiring
commandio/     Shared command streams, output formatting, and scope resolution
config/        Runtime configuration paths and settings loading
conversation/  Saved transcripts, checkpoints, compaction, and token accounting
mcpclient/     Shared MCP connections, session server setup, and tool naming
models/        Model configuration and last-used model preferences
plugins/       Agent Plugin discovery, installation, and management
providers/     Model adapters and subscription credential refresh
skills/        Agent Skill discovery, installation, and management
source/        Source-tree acquisition and checkout cleanup
terminaltext/  Sanitizing untrusted text for terminal output
toolruntime/   The bash and starlark_exec tools, their builtins, and output limits
version/       Build-time version information
```

## Maintaining this file

- Update `AGENTS.md` when the repository layout, package purposes, durable design
  constraints, conventions, or workflows change.
- Do not add implementation details, API documentation, exhaustive behavior
  descriptions, or task-specific notes. Keep those in the code, tests, README,
  or `docs/` as appropriate.

## Design constraints

- CPE is greenfield: do not add migrations, backward-compatibility layers, or
  legacy fallbacks unless requested.
- CPE and its configured plugins/MCP servers run with the user's full machine
  access. Do not add approval gates, permission policies, or sandboxing.
- Skill mutations assume a single writer. Do not add cross-process coordination or
  defenses against concurrent filesystem replacement unless requested.

## Go conventions

- Format nonempty composite literals across multiple lines, with one field or
  element per line and none on the brace lines. Format calls with two or more
  arguments the same way, including builtins and variadic calls. Empty literals
  and zero/one-argument calls may stay compact. Run `gofmt` after wrapping.
- Put substantial non-test text (roughly five lines or more) in separate files
  and use `go:embed` when it is shipped with or used by the binary. Keep loading,
  parsing, and rendering logic in Go.
- Do not add tests in the main package; test behavior in the relevant internal
  package.

## Documentation

- Keep `README.md` focused on users: setup, configuration, and usage.
- Put implementation details in code comments or relevant developer documentation.
  Do not duplicate code or configuration inventories, or add task-specific design
  explanations, to the README.
- Use `docs/` to explain current architecture and behavior for readers: lifetimes,
  guarantees, failure cases, and limitations. Avoid source-code tours, symbol
  inventories, and task histories.
- Update affected guides in the same change when documented behavior changes.
  Internal refactors alone do not require documentation changes.
- Prefer updating an existing guide over adding a new one. When adding, moving,
  or removing guides, keep the `docs/README.md` index and relative links accurate.
- Keep shared explanations and reference values in one place; link to them rather
  than copying them between guides or the README.
- Describe implemented behavior, not plans or intended behavior. Verify claims
  and examples against the code and tests.

## Repository workflow

- Run `go test ./...` for tests and `go run ./build lint` for the combined
  analyzers. `go run ./build` lists all developer tasks.
- Add developer tasks as `build/*_task.go` using `goyek.Define`.
- Use Conventional Commits. `.gitignore` is an allowlist: do not edit it or
  force-add ignored files without explicit permission.
