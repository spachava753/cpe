## Project Overview

CPE is an interactive terminal programming agent built with Bubble Tea, gai, Starlarkx, and Dyson. The model uses one persistent Starlark REPL tool; JSONL session trees durably record conversations and host boundary results for replay without external effects. Link: https://github.com/spachava753/cpe. To learn more, read README.md

## Documentation

- Package-level `doc.go` files under the relevant `internal/` subpackages, and `build/` are the canonical feature and behavior specs.
- Exported symbols used across packages should have Go doc comments that describe behavior and contracts.
- `examples/` is a folder that holds example JSON configuration for configuring CPE, as well example system prompt templates
- `internal/testutil/testgate/doc.go` defines the canonical pattern for opt-in integration, live, and interactive tests. Prefer it over ad hoc env-var checks or unconditional live tests.

## Teck stack

Golang, see go.mod for specific version.

Formatting, vetting, testing:

```bash
go fmt ./...
go vet ./...
go test ./...

# Lint (golangci-lint, modernize, unnecessary-export analysis, and deadcode)
go run ./build lint

# Lint with auto-fix for formatting issues
go run ./build -lint-fix lint
```

CLI and configuration:

```bash
go run . --help
# Explicitly create starter config files in ~/.cpe without overwriting existing files
go run . --init
```

## Performance considerations

CPE is an interactive terminal agent where execution time is dominated by network calls to AI model APIs and MCP servers. Performance optimizations are typically not a concern unless specifically requested by the user. Focus on correctness, maintainability, idiomatic Golang, protocol behavior, and user experience over micro-optimizations.

## Testing style

Tests should optimize for explicitness and auditability over deduplication. Prefer local setup, direct assertions, and copy-pasteable subtests. Do not introduce helpers or abstractions just to reduce repeated test code; add helpers only when they hide unavoidable infrastructure mechanics or make behavior materially clearer.

## Documentation for Go Symbols

When gathering context about symbols like types, global variables, constants, functions and methods, prefer to use
`go doc` command. You may use
`go doc github.com/example/pkg.Type` to get documentation about a specific symbol. Avoid using
`go doc -all` as it may overwhelm your context window. Instead, if you need to perform a search or fuzzy search for a symbol, feed the output of
`go doc -all` into a cli like `rg`, `fzf`, etc.

## Scripts

The `build/` folder contains development utility scripts managed via [Goyek](https://github.com/goyek/goyek), a Go-based task runner. Tasks are defined as Go functions and invoked with flags. Running with no arguments defaults to the `list` task, which prints all available tasks.

Adding new tasks:

1. Create a new `*_task.go` file in `build/`
2. Define flags in `main.go` if the task needs arguments
3. Use `goyek.Define(goyek.Task{...})` to register the task

## Git

Use Conventional Commits for all commit messages: `type(scope): description`,
with an optional scope. For example, `fix(config): propagate prompt cancellation`
or `refactor: resolve lint violations`.

The tracked files commited in this repo are controlled by the `.gitignore`, which is configured more as an allowlist. By default, all files are not tracked or commited, unless explicitly allowed by the `.gitignore`. Never modify `.gitignore`, unless explicitly asked by the user. If you find that a file you are working on is not being tracked, you can ask the user as to whether this file should be tracked or not.

`PLAN.md` files should never be tracked, they are transient files that will be deleted after implementation completion.
