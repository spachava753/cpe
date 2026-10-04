# CPE

## Project layout (depth 1)

```text
.github/      CI, releases, and issue templates
build/        Goyek developer tasks and repository-specific linting
internal/     Business logic
main.go       CLI entry point
go.mod        Go version, dependencies, and developer tools
README.md     User workflows and verification instructions
```

## Design

- **Greenfield:** CPE has no existing users or deployed state. Do not add
  migrations, backward-compatibility layers, or legacy fallbacks unless explicitly
  requested. Design for the current requirements rather than hypothetical old data.
- Model domain concepts and state transitions explicitly with types. Prefer
  meaningful domain values over loose maps, strings, and boolean combinations.
- **Parse, don't validate:** convert configuration and protocol input into domain
  values at boundaries. Establish invariants there instead of repeatedly checking
  partially valid data downstream.
- Keep changes scoped and simple; avoid speculative abstractions and performance work unless
  a demonstrated need or the task calls for it.

## Go formatting

- Write nonempty composite literals across multiple lines, with one field or
  element per line and no fields or elements on the opening or closing brace
  lines. This includes structs, maps, slices, arrays, and nested test-table entries.
- Write function and method calls with two or more arguments across multiple
  lines, with one argument per line and the closing parenthesis on its own line.
  This includes builtins and variadic calls.
- Empty literals and zero/one-argument calls may stay compact. Apply these rules
  recursively to nested literals and calls, and run `gofmt` after wrapping.

## Tests

- Prefer table-driven unit tests with named `t.Run` cases. For the same function
  or method, extend its existing test table instead of creating another top-level
  test for each scenario.
- Keep setup and assertions explicit. Use helpers for infrastructure, not merely
  to remove repetition or hide expected behavior. Use temporary fixtures and fakes.

## Repository workflows

- `go run ./build` lists tasks; `go run ./build lint` runs the repository's combined
  analyzers. Add developer tasks as `build/*_task.go` using `goyek.Define`.
- Prefer targeted `go doc` lookups for Go APIs; avoid dumping entire packages.
- Use Conventional Commits. `.gitignore` is an allowlist: do not edit it or
  force-add ignored files without explicit permission.
