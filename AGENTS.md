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
- **Trusted host execution:** the CPE runtime and configured plugins/MCP servers
  run with the user's full machine access. Do not add execution approval gates,
  permission policies, or sandboxing. Cancellation, cleanup, and protocol output
  handling are lifecycle concerns, not access controls.
- Model domain concepts and state transitions explicitly with types. Prefer
  meaningful domain values over loose maps, strings, and boolean combinations.
- **Parse, don't validate:** convert configuration and protocol input into domain
  values at boundaries. Establish invariants there instead of repeatedly checking
  partially valid data downstream.
- Inject required configuration and dependencies explicitly. Do not silently fill
  missing struct fields with defaults. Unconfigured required values should return
  a clear error at the start of an operation.
- Accept filesystem paths at exported API boundaries. Keep private filesystem
  helpers independent of the host filesystem by using injected `afero.Fs` values
  or streams.
- Keep changes scoped and simple; avoid speculative abstractions and performance work unless
  a demonstrated need or the task calls for it.
- Keep errors opaque by default, especially at exported API boundaries. Do not
  introduce exported error types or sentinel errors unless a concrete API consumer
  needs to branch on that specific failure. Keep operation outcomes separate from
  error classification.

## Go formatting

- Write nonempty composite literals across multiple lines, with one field or
  element per line and no fields or elements on the opening or closing brace
  lines. This includes structs, maps, slices, arrays, and nested test-table entries.
- Write function and method calls with two or more arguments across multiple
  lines, with one argument per line and the closing parenthesis on its own line.
  This includes builtins and variadic calls.
- Empty literals and zero/one-argument calls may stay compact. Apply these rules
  recursively to nested literals and calls, and run `gofmt` after wrapping.

## Exported API documentation

- Write comments for readers who have only the symbol's signature and documentation.
  State its purpose clearly at a glance, following Go standard-library style.
- Prefer one or two lines. Add explanation only for non-obvious inputs, return values,
  ownership, side effects, or error contracts.
- Use concrete wording. Avoid implementation narration, redundant qualifiers, and
  "does this, not that" boilerplate.

## Tests

- Do not add tests in the main package. Test behavior in the relevant internal package.
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
