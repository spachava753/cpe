## Project Overview

CPE (Chat-based Programming Editor) is a CLI that connects local developer workflows to multiple AI model providers. It analyzes, edits, and creates code via natural-language prompts, with optional MCP tool integration and persistent conversation storage. Link: https://github.com/spachava753/cpe. To learn more, read the README.md

## Documentation

- Package-level `doc.go` files under the relevant `internal/` subpackages, and `build/` are the canonical feature and behavior specs.
- `design.md` defines codebase design decisions, goals and non-goals, and project structure. It is required to read the design doc before starting to implement any code.
- Exported symbols used across packages should have Go doc comments that describe behavior and contracts.
- `examples/` is a folder that holds example yaml configuration for configuring CPE, as well example system prompt templates

## Teck stack

Golang, see go.mod for specific version.

Formatting, vetting, testing:

```bash
go fmt ./...
go vet ./...
go test ./...

# Lint (golangci-lint plus repo-specific architecture linters)
go run ./build lint

# Lint with auto-fix for formatting issues
go run ./build -lint-fix lint
```

Schema and configuration:

```bash
# Generate JSON Schema for config
go generate ./internal/config/

# Validate configuration
./cpe config lint ./examples/cpe.yaml
```

## Performance considerations

CPE is a CLI tool and MCP client where execution time is dominated by network calls to AI model APIs. Performance optimizations are typically not a concern unless specifically requested by the user. Focus on correctness, maintainability, iodmatic Golang, and user experience over micro-optimizations.

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
