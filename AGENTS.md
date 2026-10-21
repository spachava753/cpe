# CPE

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

## Agent package boundaries

- `internal/agent` owns ACP translation, live sessions, and turn orchestration.
- `internal/conversation` owns saved records, compaction invariants, token accounting,
  and checkpoint storage.
- `internal/providers` owns model adapters and credential-refresh workers.
- `internal/mcpclient` owns MCP transports and session connections.
- `internal/toolruntime` owns Dyson/bash execution, tool discovery, and output conversion.
- The CLI assembles these packages. Extracted packages must not import `agent` or ACP.
  See `internal/agent/README.md` for resource ownership and compaction handoff.

## Engineering discipline

- Implement only the behavior the user requested. Ask before adding anything
  beyond that scope. If you believe another capability, interface, dependency, or
  abstraction is necessary, explain why and get approval before adding it.
  Convenience, symmetry, and possible future use are not authorization.
- Resolve consequential ambiguity with the user before coding. Identify the
  requested behavior and how to verify it; do not invent adjacent requirements
  or reinterpret a specific request as permission to build a broader system.
- Keep the user-facing interface minimal. Give each requested operation one clear
  entry point. Do not expose lower-level implementation operations as additional
  APIs, tools, commands, or configuration options without approval.
- Prefer the smallest cohesive implementation that satisfies the requirement.
  Reuse existing domain concepts and established patterns. Do not add extension
  points, wrappers, or generic frameworks without a concrete, approved need.
- When an unwanted addition is rejected, remove its implementation, wiring,
  documentation, and unnecessary tests. Do not retain aliases, compatibility
  paths, or tests that memorialize the rejected interface.
- Review the complete diff against the request, not just for code correctness.
  Every addition must have a purpose within the approved scope. Independent
  review and passing checks do not authorize extra behavior.
- Verify observable outcomes with relevant tests and repository checks. Report
  what changed, which checks ran, and any remaining uncertainty. Do not equate
  more code, more tests, or more abstractions with better engineering.

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

## Embedded text

- Store substantial non-test text (roughly five lines or more when formatted
  naturally) in separate files and include it with `go:embed` when shipped with
  or used by the binary. This includes prompts, help text, and templates,
  including Go templates.
- Use an appropriate file format, such as Markdown for prompts. Keep loading,
  parsing, and rendering logic in Go, not the text itself. Do not flatten long
  content into a single-line literal to avoid this rule.
- Short literals may remain inline. Text in tests is exempt.

## Exported API documentation

- Write comments for readers who have only the symbol's signature and documentation.
  State its purpose clearly at a glance, following Go standard-library style.
- Prefer one or two lines. Add explanation only for non-obvious inputs, return values,
  ownership, side effects, or error contracts.
- Use concrete wording. Avoid implementation narration, redundant qualifiers, and
  "does this, not that" boilerplate.

## Tests

- Test requested behavior, domain invariants, and concrete failure modes. Prefer
  observable outcomes over implementation details, symbol names, or duplicated
  implementation logic. For bug fixes, reproduce the failure and verify the
  corrected behavior.
- Do not add tests merely asserting that a removed or never-requested API is
  unavailable. Remove obsolete tests along with the unwanted feature. Negative
  tests should exercise meaningful supported behavior, such as invalid inputs,
  cancellation, or failure handling.
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
