# CPE

Interactive Go terminal agent built with Bubble Tea, gai, Starlarkx, and Dyson.
The SDK stays in `internal/`; the model sees only `starlark_repl`. JSONL session
trees preserve conversations and host-call results. Replay must restore state
from recorded results without repeating external effects.

## Project layout (depth 1)

```text
.github/      CI, releases, and issue templates
build/        Goyek developer tasks and repository-specific linting
examples/     JSON configuration, palettes, and system prompt examples
experiments/  Isolated experiments and evaluations
internal/     Agent, REPL, sessions, providers, configuration, themes, CLI, and TUI
skills/       Repository agent skills
main.go       CLI entry point
go.mod        Go version, dependencies, and developer tools
README.md     User workflows and verification instructions
```

## Design

- Read the relevant package's `doc.go` before changing behavior; these are the
  canonical contracts. Update them when the contract changes.
- Model domain concepts and state transitions explicitly with types. Prefer
  meaningful domain values over loose maps, strings, and boolean combinations.
- **Parse, don't validate:** convert configuration and protocol input into domain
  values at boundaries. Establish invariants there instead of repeatedly checking
  partially valid data downstream.
- Keep CLI/TUI focused on wiring and presentation. Conversation state belongs in
  `agent`, host-call durability in `repl`/`session`, and protocol quirks in adapters.
- Separate service credentials, model identity, and wire protocol. Establish model
  availability from service-specific sources. Login data stays outside model
  context and session history.
- Treat persisted formats and replay semantics as versioned contracts. Changes
  need compatibility tests that restore old records without repeating host effects.
- Keep changes scoped; avoid speculative abstractions and performance work unless
  a demonstrated need or the task calls for it.

## Tests

- Prefer table-driven unit tests with named `t.Run` cases. For the same function
  or method, extend its existing test table instead of creating another top-level
  test for each scenario.
- Keep setup and assertions explicit. Use helpers for infrastructure, not merely
  to remove repetition or hide expected behavior. Use temporary fixtures and fakes.
- Use `internal/testutil/testgate` for opt-in integration, live, and interactive
  tests; its `doc.go` defines the gates. Never require credentials or desktop
  services in ordinary unit tests.
- Exercise TUI changes with the deterministic terminal harness documented in
  README, using an isolated `CPE_TUI_TEST_DIR` rather than personal configuration.

## Repository workflows

- `go run ./build` lists tasks; `go run ./build lint` runs the repository's combined
  analyzers. Add developer tasks as `build/*_task.go` using `goyek.Define`.
- Prefer targeted `go doc` lookups for Go APIs; avoid dumping entire packages.
- Use Conventional Commits. `.gitignore` is an allowlist: do not edit it or
  force-add ignored files without explicit permission. Never commit `PLAN.md`.
