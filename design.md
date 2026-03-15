# CPE Design

This document describes the design of CPE (Chat-based Programming Editor): a CLI that connects local developer workflows to multiple model providers, MCP servers, conversation persistence, and code execution.

This document should be read as the canonical explanation of the codebase's current architectural decisions, package boundaries, and major trade-offs. It explains why the system is organized the way it is, what constraints it is optimizing for, and what shape new code should generally follow.

Package-level `doc.go` files and exported symbol comments remain the source of truth for package-local behavior and detailed contracts. This document operates at the repo design level: it explains the structure of the system and the rationale behind that structure.

## Similarities and differences with typical Cobra CLIs

Many Go CLIs built on Cobra place most runtime logic directly in `cmd/` packages. That is a reasonable default for smaller tools. CPE intentionally diverges.

In CPE:

- `main` is only the executable entry point.
- `cmd` owns Cobra structure, flags, help text, and `RunE` wiring.
- `internal/commands` owns framework-agnostic command orchestration.
- `internal/agent` owns model runtime assembly and execution.
- support packages (`config`, `mcp`, `storage`, `codemode`, `render`, `prompt`, `input`, etc.) implement focused pieces of the system.

This extra layer is not free. It adds packages and more explicit option structs. We keep it because CPE benefits from it in three ways:

1. it keeps the CLI framework replaceable;
2. it makes command behavior easier to test without Cobra-specific machinery;
3. it gives the codebase an explicit dependency flow rather than letting package responsibilities blur over time.

## Requirements

CPE should aim to be:

- **CLI-first**: the primary UX is a local terminal tool used interactively and in scripts.
- **layered**: package boundaries should reflect ownership and keep dependencies flowing inward toward runtime policy rather than outward toward framework details.
- **provider-agnostic**: support for model providers should share a common runtime model where possible.
- **tool-composable**: MCP tools and built-in tools should be usable both directly and through higher-level compositions such as code mode and subagents.
- **local-first**: conversation storage, configuration, and most execution state should remain local by default.
- **privacy-conscious**: users must be able to run without persistence when needed.
- **testable**: command orchestration and runtime assembly should be testable with narrow interfaces and explicit dependencies.
- **observable**: failures in complex flows, especially subagents and model/tool interactions, should be diagnosable.
- **maintainable**: new features should have an obvious home rather than being bolted onto whichever package is nearby.
- **simple by default**: when forced to choose, prefer clearer and smaller architecture over backward-compatibility shims or speculative abstraction.

## Non-goals

This design intentionally does not optimize for the following:

- **framework neutrality at every layer**: only the CLI edge is kept framework-specific. Internally, CPE is free to use whatever implementation patterns best fit the runtime.
- **backward compatibility by default**: CPE prefers a clearer structure over preserving old package shapes, shims, or command internals unless compatibility is itself the explicit goal.
- **perfect abstraction of provider differences**: the system aims for a common runtime model, not for pretending all provider capabilities and quirks are identical.
- **deeply generic plugin architecture**: CPE supports extension through MCP, code mode, and subagents, but does not attempt to turn every internal behavior into a general-purpose extension point.
- **maximal configurability of every policy**: some choices are intentionally opinionated because the maintenance cost of making every behavior configurable would outweigh the benefit.
- **transporting persistence-specific types through the whole runtime**: the design favors a small set of shared runtime types, even when that means using metadata fields rather than introducing richer storage-specific wrapper objects everywhere.

## Design

The sections below describe the intended architecture of the codebase as it exists today.

## Foundations

### Package layout and dependency flow

The intended top-level dependency flow is:

```text
main -> cmd -> internal/commands -> internal/agent -> support packages
```

There is one deliberate exception: `cmd` may also import `internal/version` for process-level version reporting.

This is the central architectural rule of the codebase.

Its purpose is not aesthetic. It encodes two important ideas:

- **framework details stay at the edge**: Cobra belongs in `cmd`, not in use-case logic.
- **runtime policy lives below the CLI**: command behavior, dependency resolution, and orchestration belong in `internal/commands` and below.

The supporting package roles are intentionally narrow:

- `internal/config`: file schema plus effective runtime configuration resolution.
- `internal/mcpconfig`: dependency-neutral MCP server config schema shared by config loading and runtime code.
- `internal/mcp`: MCP client/server runtime integration.
- `internal/storage`: conversation persistence contracts plus SQLite and in-memory implementations.
- `internal/codemode`: execute-go-code prompt generation, harness wiring, and execution.
- `internal/input`: prompt/file/URL input loading into model blocks.
- `internal/prompt`: system prompt templating and skill discovery helpers.
- `internal/render`: TTY-aware markdown/plain-text rendering.
- `internal/auth`: OAuth and credential transport helpers.
- `internal/subagentlog`: subagent event streaming and rendering.
- `internal/ports`: small shared interfaces used to decouple packages.

Several of these packages exist to keep ownership clear and package responsibilities narrow:

- `internal/input`, `internal/prompt`, and `internal/render` exist so `internal/agent` does not own unrelated concerns.
- `internal/mcpconfig` exists so `internal/config` does not depend on MCP runtime code.
- `internal/ports` exists as the shared place for small decoupling interfaces rather than allowing packages to depend on a vague bucket of shared concrete types.

### Architectural enforcement

CPE uses architecture linting to keep these boundaries from regressing.

The current enforced rules include:

- `cmd` may import only `internal/commands` and `internal/version` from this module.
- `internal/commands` may not import Cobra or pflag.
- `internal/config` may not import `internal/mcp`.
- `internal/commands` may import `internal/agent` only from the runtime orchestration files that actually assemble generation behavior.
- `internal/agent` may not import `internal/input` or `internal/prompt`.

This is intentionally opinionated. The goal is not to lint style; it is to protect the package map and ownership boundaries described in this document.

### Dependency flow versus data flow

A clean architecture requires distinguishing dependency direction from runtime data flow.

Dependencies point inward:

```text
cmd depends on commands
commands depends on agent
agent depends on config/mcp/storage/render/etc.
```

Runtime data often moves the other way:

- CLI flags and stdin enter through `cmd`.
- `internal/commands` resolves config, storage, and inputs.
- `internal/agent` consumes those resolved dependencies to execute generation.
- responses, tool outputs, and persisted messages flow back outward to `commands`, then to `cmd`, then to the terminal.

CPE is designed so the outer layers know about the inner layers, but the inner layers do not know about Cobra, process-global flags, or terminal command trees.

### Configuration model

CPE separates configuration into two layers:

- `RawConfig`: the file-level YAML/JSON schema.
- `Config`: the effective runtime configuration for one selected model execution.

This split exists because loading a config file and running a model are not the same operation.

`RawConfig` is useful for commands that inspect configuration as written, such as listing models or inspecting MCP servers. `Config` is useful for commands that execute behavior and need resolved precedence, normalized paths, and a chosen model.

This separation makes several design choices explicit:

- model selection is a runtime concern, not a file-loading concern;
- path normalization belongs in config resolution;
- commands that do not need a model should not be forced through model resolution.

Resolution precedence is intentionally simple and explicit:

- model selection: runtime override, then `defaults.model`;
- generation options: runtime overrides, then model defaults, then global defaults;
- system prompt path: model override, then global default;
- timeout: runtime override, then config default, then built-in default;
- `codeMode`, `flightRecorder`, and `compaction` use whole-object model overrides rather than field-level deep merges.

The last choice is important. Deep merging nested config objects is flexible, but it becomes difficult to explain and easy to misread. CPE prefers object replacement for these advanced runtime features because it is easier to reason about and simpler to document.

Relative filesystem paths are interpreted relative to the config file location when possible. This applies to important user-facing settings such as system prompt paths, conversation storage paths, local module paths for code mode, and subagent output schema paths.

## Root CLI execution

The main interactive execution path is intentionally split between layers.

### `main`

`main` is responsible only for process entry and top-level lifecycle concerns such as signal-aware context setup.

### `cmd`

`cmd` owns:

- Cobra command structure;
- flag registration and parsing;
- help text and command hierarchy;
- mapping Cobra inputs to explicit option structs.

`cmd` should not own business logic or runtime assembly.

### `internal/commands`

`internal/commands` owns command use-case orchestration. For the root execution path, that includes:

- resolving effective config;
- opening storage when persistence is enabled;
- resolving continuation history;
- loading user input blocks from prompt, files, URLs, and stdin;
- loading and rendering the system prompt;
- creating any subagent logging bridge needed for child runs;
- delegating to `internal/agent` for actual model execution.

This layer exists so the command behavior is testable and not coupled to Cobra.

### `internal/agent`

`internal/agent` is where the runtime model pipeline is built. It owns:

- provider-specific generator creation;
- middleware composition;
- tool registration;
- code-mode tool registration;
- persistence middleware;
- output printing and token usage reporting;
- flight-recorder capture on terminal errors;
- compaction orchestration and restart caps.

This package is intentionally the deepest orchestrator in the normal CLI path. It should know how to run the model runtime, but not how Cobra parsed the request.

## Conversation model and persistence

CPE stores conversations as message trees rather than flat transcripts.

That choice supports the actual CLI UX:

- continue from the latest point;
- continue from an arbitrary historical message;
- branch a conversation from an earlier point;
- preserve lineage during compaction and subagent traces.

A linear append-only transcript would be simpler, but it would not fit the branching behavior the CLI exposes.

### Storage interfaces

`internal/storage` defines narrow interfaces such as `DialogSaver`, `MessagesGetter`, `MessagesLister`, and `MessagesDeleter`.

This keeps consumers from depending directly on SQLite details and allows tests to use `MemDB` instead of a real database.

### SQLite plus metadata in messages

The production storage backend is SQLite. Returned `gai.Message` values include storage metadata in `ExtraFields`, such as message ID, parent ID, creation time, compaction parent, and subagent markers.

This is a deliberate trade-off.

A richer wrapper type around stored messages would be more explicit, but it would also force many parts of the runtime to learn storage-specific types. CPE instead keeps the primary runtime model centered on `gai.Message` and threads persistence metadata through `ExtraFields` where needed.

This is less type-strict than a dedicated storage message type, but it keeps storage concerns from dominating the rest of the codebase.

### Incognito mode

Incognito mode means no conversation persistence for the root run. The runtime should execute without opening or mutating conversation storage.

This design is intentionally strict because privacy features should fail closed rather than partially work.

## Model runtime and middleware

The model runtime is assembled as a decorated generator pipeline.

The design uses middleware-style wrappers because CPE needs to layer behavior orthogonally:

- panic recovery;
- persistence;
- response printing;
- tool-result printing;
- token usage reporting;
- thinking-block filtering across providers;
- flight recorder capture;
- retries;
- compaction-related logic.

A monolithic generator implementation would quickly become difficult to reason about. Wrappers allow these concerns to stay separate while still composing into a single runtime pipeline.

### Middleware ordering

Ordering matters.

For example, persistence must be outside retry logic so messages are not re-saved on every retry, and saving must be arranged so message IDs propagate correctly through later runtime steps. Thinking-block filtering must preserve only provider-compatible reasoning blocks when conversations cross model families.

CPE keeps this ordering inside `internal/agent`, where the runtime invariants are understood, rather than spreading wrapper assembly throughout the codebase.

### Provider-specific differences

CPE presents a unified CLI and unified generator pipeline, but it does not pretend all providers behave identically.

Provider-specific initialization still exists, and provider-specific quirks such as reasoning block behavior, auth mechanisms, or API surfaces are handled in the runtime layer.

The design goal is not to erase differences entirely. It is to isolate them so the rest of the system does not need provider-specific branches everywhere.

## MCP client integration

CPE acts as an MCP client during normal runs.

The client integration is responsible for:

- creating transports for `stdio`, `http`, and `sse` servers;
- applying per-server timeouts;
- applying static headers or environment variables where appropriate;
- connecting to configured servers;
- listing and filtering tools;
- rejecting duplicate tool names across servers;
- adapting tool metadata and tool calls into the model runtime.

### Why `internal/mcpconfig` exists

MCP server configuration is shared by config loading and MCP runtime code. Those are different concerns.

If `internal/config` imported `internal/mcp`, the config layer would depend on runtime transport implementation details. That was part of the original architectural muddiness. `internal/mcpconfig` exists to keep the shared schema neutral while allowing `internal/mcp` to own actual transport behavior.

### Tool filtering

Per-server tool filtering uses `enabledTools` or `disabledTools`.

This is a practical control surface rather than a sophisticated policy engine. The aim is to let users shape the exposed tool surface without adding a second policy language.

### Duplicate tool names fail fast

CPE rejects duplicate tool names across servers after filtering.

This is intentionally strict. Silent shadowing or last-wins behavior would make tool behavior ambiguous and difficult to debug, especially once code mode and subagents are involved.

## Code mode

Code mode is CPE's answer to multi-step tool orchestration.

Instead of exposing every tool call as a separate conversational round trip, CPE can expose selected tools as strongly typed Go functions and ask the model to write a complete Go program implementing:

```go
Run(ctx context.Context) ([]mcp.Content, error)
```

That program is compiled and executed in a temporary sandbox module.

### Why Go

Go was chosen because it provides:

- straightforward static typing for tool schemas;
- good support for control flow and composition;
- easy use of the standard library for I/O, parsing, and HTTP;
- fast enough compile/run behavior for an interactive CLI tool;
- a runtime model that is predictable and easy to constrain.

CPE is already a Go project, so using Go as the code-mode language also keeps the harness and generated bindings in one ecosystem.

### Recoverable versus fatal failures

Code mode distinguishes between failures the model can recover from and failures that should stop execution entirely.

Recoverable failures include compile errors, runtime panics, or timeouts in generated code; these are returned as tool results so the model can iterate. Fatal harness failures remain hard errors.

This distinction exists because code mode is meant to be iterative rather than all-or-nothing.

### Partitioning tools

Not every MCP tool should become a code-mode function. CPE partitions tools into code-mode and normal tools using configuration such as `excludedTools`.

This preserves a small, intentional code execution surface and prevents code mode from becoming a grab bag of every tool in the system.

## MCP server mode and subagents

CPE can also act as an MCP server. In this mode, one config file exposes one subagent as one MCP tool over stdio.

This is intentionally constrained.

A more general server mode could expose many tools or many agents from one process, but the one-subagent-per-config design keeps the model simple:

- each config describes one tool identity;
- each subagent has one prompt, one model policy, and optional one output schema;
- parent agents compose subagents explicitly rather than implicitly.

### Why stdout is reserved

In MCP server mode, stdout is reserved for protocol traffic. Human-visible diagnostics and streaming observability must therefore go to stderr.

This separation is fundamental to reliable stdio-based protocol behavior.

### Subagent observability

Subagents emit structured events back to the root process through `internal/subagentlog` when a logging address is supplied.

Event emission failure aborts the subagent run.

This is a strong choice, but it is deliberate: silent observability failure makes nested agent execution too hard to diagnose. CPE treats subagent visibility as part of correctness, not as an optional accessory.

### Output schemas

Subagent output schemas are loaded and validated at startup rather than on first call.

This front-loads failure and ensures the exported MCP tool contract is stable for the lifetime of the server.

## Rendering and terminal output

CPE centralizes rendering in `internal/render`.

This package exists so higher-level packages can request appropriately configured renderers without taking a dependency on glamour, termenv, or TTY details.

### Per-stream rendering

CPE distinguishes between stdout and stderr output streams.

User-visible assistant content generally goes to stdout. Thinking blocks, tool-call formatting, message IDs, token usage, and related diagnostics generally go to stderr.

Renderers are selected per target stream so interactive formatting and script-friendly output can coexist more predictably.

### Fallback behavior

When rich rendering is unavailable or unsuitable, CPE falls back to plain text.

That is an intentional CLI design choice: correctness and readability matter more than decoration.

## Authentication and provider access

Authentication is split between runtime configuration and a dedicated auth package.

Most providers use API keys from environment variables. Supported OAuth-backed flows live in `internal/auth`, which owns:

- PKCE and state helpers;
- login callback handling;
- token storage and refresh;
- bearer-token HTTP transports;
- provider-specific account usage helpers.

This separation keeps OAuth concerns from leaking into the general model runtime.

## Error handling, timeouts, and reliability

CPE follows a few broad runtime rules.

### Context everywhere I/O happens

I/O and external calls should accept `context.Context` so cancellation and timeouts flow through the system.

### Contextual errors

Errors should be wrapped with enough context to explain which high-level step failed.

### Fail fast on ambiguity

Examples include duplicate MCP tool names, invalid output schema files, transport misconfiguration, or unsupported content types in places where silent degradation would hide real behavior.

### Degrade gracefully where the model can recover

Examples include code-mode compile/runtime failures returned as tool results, or tool-call parameter parsing issues surfaced in model-visible form.

CPE tries to be strict about infrastructure and explicit contracts, while still allowing model-driven workflows to self-correct when the failure is part of the model's task loop.

## Testing and enforcement strategy

The architecture is designed for testability, not just for code organization.

Key techniques include:

- explicit option structs for command functions;
- narrow interfaces for storage and related dependencies;
- `MemDB` for fast conversation and subagent tests;
- command orchestration placed below Cobra;
- architecture lint rules that prevent boundary regressions.

This makes it practical to test:

- config resolution separately from CLI parsing;
- conversation operations separately from SQLite plumbing;
- rendering and wrapper behavior separately from provider calls;
- MCP integration separately from model selection.

## Alternatives considered

This section records alternatives that are plausible for a system like CPE, but that are not the preferred design today.

### Putting business logic directly in `cmd`

A smaller CLI could reasonably keep most execution logic in Cobra handlers. CPE does not do that because command behavior here is substantial: config resolution, storage, MCP inspection, subagent serving, account flows, and runtime model assembly all benefit from framework-independent orchestration and better testability.

### Flattening `internal/commands` into `internal/agent`

This would reduce one package boundary, but it would also merge command use-case orchestration with model runtime assembly. CPE keeps them separate because they change for different reasons: command workflows are not the same thing as generator construction and tool runtime policy.

### Treating configuration as a single resolved structure only

A single config type is simpler at first, but it forces every command into runtime-resolution behavior even when the command only needs the raw file schema. CPE keeps both `RawConfig` and effective `Config` because inspection commands and runtime execution commands do not have the same needs.

### Storing conversations as flat transcripts

A flat append-only transcript would simplify persistence logic, but it would not model conversation branching naturally. CPE stores message trees because branching and continuation from arbitrary historical points are part of the intended interaction model.

### Using a richer persistence message type instead of metadata in `ExtraFields`

A dedicated stored-message type would be more explicit, but it would also push persistence-specific types through much more of the runtime. CPE keeps the runtime centered on `gai.Message` and uses well-defined metadata keys when storage lineage needs to be preserved.

### Deep-merging nested configuration objects

Deep merge behavior can be flexible, but it is harder to explain and easier to misread. CPE prefers whole-object replacement for advanced nested runtime features such as `codeMode`, `flightRecorder`, and `compaction`.

### Exposing all MCP tools directly and uniformly

CPE instead distinguishes between normal tool exposure, filtered tool exposure, and code-mode exposure. This keeps the tool surface intentional and avoids turning code mode into an uncontrolled wrapper around every available tool.

### Using a scripting language instead of Go for code mode

A scripting language would reduce compile latency, but CPE values static typing, straightforward schema-to-code generation, standard library access, and predictable execution structure. Because CPE is already implemented in Go, Go is also the lowest-friction language for the harness and generated bindings.

### Treating subagent logging as best-effort

Best-effort observability would make subagent execution more permissive, but it would also make nested-agent failures much harder to diagnose. CPE treats observability failures as execution failures because visibility is part of the contract for subagent composition.

## Trade-offs

The current design makes several conscious trade-offs.

### Thin `cmd`, richer `internal/commands`

This adds an extra orchestration layer, but it preserves framework independence and testability.

### Tree-shaped conversation storage

This is more complex than a flat transcript store, but it matches CPE's branching conversation UX.

### Metadata in `ExtraFields`

This is less explicit than dedicated storage wrapper types, but it keeps persistence details from infecting the whole runtime model.

### Whole-object config overrides

This is less flexible than deep merging nested config, but much easier to explain and maintain.

### Strict MCP validation and duplicate rejection

This is less forgiving than permissive best-effort behavior, but it avoids ambiguous tool surfaces and silent no-op configuration.

### Go-based code mode

This adds harness complexity and a compile step, but it gives the model a real programming language with strong typing and standard-library access.

### Fail-closed observability for subagents

Aborting on logging failure is harsh, but nested agent systems are hard enough to debug already. CPE chooses visibility over silent degradation here.

### Simplicity over compatibility

CPE is personal software with a single primary user. The design therefore favors simplification and clearer structure over preserving old shapes by default.

## Current design invariants

The current architecture should preserve the following invariants unless there is a deliberate design change:

- `main` stays a thin executable boundary.
- `cmd` owns Cobra and only Cobra-facing concerns.
- `internal/commands` owns framework-agnostic command orchestration.
- `internal/agent` owns generator/runtime assembly.
- `internal/config` does not depend on MCP runtime packages.
- `internal/agent` does not own input loading, prompt rendering, or renderer construction details.
- conversation persistence remains tree-based.
- code mode remains a distinct runtime capability rather than being spread across unrelated packages.
- package boundaries are enforced in code, not just described in prose.

## Open questions

This design resolves the major architectural shape of the codebase, but some questions remain intentionally open:

- **how far to push framework-independence beyond the CLI edge**: today the main goal is keeping `cmd` replaceable. It is still an open design question how much additional abstraction is useful below that layer before the abstractions become heavier than the problem.
- **how broad provider parity should be**: CPE presents a unified runtime model, but provider APIs continue to diverge. The long-term balance between provider-specific features and a shared core remains an ongoing design question.
- **how opinionated code mode should remain**: Go-based code mode is intentionally strong and specific. Future changes may revisit how much of the execution model should remain fixed versus configurable.
- **how much policy should be enforced structurally versus by lint**: some boundaries are encoded in package structure, while others are guarded by architecture lint. The right division between those approaches may evolve.
- **how much subagent execution should share policy with root execution**: CPE already shares substantial runtime behavior between the two, but the exact degree of coupling between root runs and subagent runs remains a place where future design decisions may matter.

## Future evolution

This document is meant to evolve with the codebase.

When the architecture changes, the preferred pattern is:

1. keep package-local behavior documented in package `doc.go` files and exported comments;
2. update this document when a repo-level architectural decision changes;
3. add or tighten architecture lint when a boundary becomes important enough to enforce mechanically.

The purpose of this document is to make the intended structure of the codebase explicit so that future changes can be made deliberately rather than by drift.
