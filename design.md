# CPE Design

This document describes the current design of CPE (Chat-based Programming Editor): a local Agent Client Protocol (ACP) server that connects ACP clients to model providers, MCP tools, generated Go code execution, and local session persistence.

Package-level `doc.go` files remain the source of truth for package-local behavior and detailed contracts. This document explains repo-level architecture, package boundaries, and the trade-offs that shape new code.

## Product Shape

CPE is ACP-first. The primary user experience is an editor or other ACP-compatible client launching `cpe acp serve` and communicating with it over stdio JSON-RPC.

The executable uses Cobra for its server/control command surface. It starts the ACP server and provides local inspection commands for model profiles, MCP servers, provider accounts, and shell completion.

CPE still provides useful local inspection commands:

- `cpe model ...` inspects configured model profiles and rendered system prompts.
- `cpe mcp ...` inspects and tests MCP servers attached to a selected model profile.
- `cpe account ...` manages provider account credentials and usage lookups.
- `cpe acp serve` starts the actual ACP server.

## Requirements

CPE should aim to be:

- **ACP-first**: the primary interaction loop is hosted by an ACP client, not by CPE's terminal process.
- **client-portable**: CPE should work with ACP clients such as Zed without baking in editor-specific behavior unless the protocol requires it.
- **provider-agnostic**: model providers should share a common runtime model where possible while still allowing provider-specific behavior.
- **tool-composable**: built-in tools, MCP tools, client-provided MCP servers, code mode, and compaction should compose predictably inside ACP sessions.
- **local-first**: configuration, credentials, session metadata, and message history stay local by default.
- **testable**: ACP behavior, local inspection commands, storage, and model-runtime assembly should be independently testable.
- **observable**: protocol traffic, model/tool failures, and persistence errors should be diagnosable.
- **maintainable**: package ownership should remain explicit, and compatibility code should have a clear current purpose.

## Non-goals

This design intentionally does not optimize for:

- **framework neutrality at every layer**: Cobra is isolated at the process edge, but internal packages use the patterns that best fit their runtime responsibilities.
- **perfect provider abstraction**: provider APIs differ; CPE isolates differences rather than pretending they do not exist.
- **generic plugin hosting**: extension happens through ACP, MCP, built-in tools, prompt templates, and code mode, not through a broad internal plugin system.
- **deep config merging**: model profiles are self-contained runtime profiles; YAML anchors are the preferred deduplication mechanism.

## Package Layout

The primary server dependency flow is:

```text
main -> internal/cmd -> internal/acp -> internal/agent -> support packages
```

Inspection command flow is:

```text
main -> internal/cmd -> internal/commands -> support packages
```

Important package roles:

- `main` owns process startup and logging setup.
- `internal/cmd` owns Cobra command structure, flags, help text, and argument mapping.
- `internal/acp` owns ACP protocol implementation, session lifecycle, session configuration, prompt execution, cancellation, session updates, skill slash commands, and runtime creation.
- `internal/agent` owns provider generator construction and reusable generator wrappers.
- `internal/commands` owns framework-agnostic helpers for local inspection commands.
- `internal/config` owns YAML schema, validation, and effective runtime config resolution.
- `internal/mcpconfig` owns dependency-neutral MCP server config schema.
- `internal/mcp` owns MCP client runtime integration and ACP tool-call update helpers.
- `internal/storage` owns message-tree and ACP session persistence contracts plus SQLite/in-memory implementations.
- `internal/codemode` owns execute_go_code prompt generation, tool callback behavior, harness creation, and sandbox execution.
- `internal/textedit` owns the bundled text_edit tool and ACP diff content generation.
- `internal/prompt` owns system prompt template rendering.
- `internal/skills` owns skill discovery, metadata parsing, and model-visible filtering.
- `internal/render` owns terminal rendering used by local inspection commands.
- `internal/auth` owns OAuth and credential transport helpers.

## Architectural Boundaries

The core rule is that protocol and framework boundaries stay narrow:

- `internal/cmd` should stay focused on Cobra wiring.
- `internal/acp` should own ACP protocol semantics and should not leak Cobra concepts.
- `internal/commands` should remain framework-agnostic and avoid ACP session state.
- `internal/config` should not import MCP runtime code; shared MCP schema belongs in `internal/mcpconfig`.
- `internal/agent` should not own ACP session lifecycle, storage policy, prompt loading, or command output.
- `internal/storage` should expose narrow interfaces so ACP/runtime code can depend on capabilities rather than SQLite details.

## ACP Server Execution

`cpe acp serve` loads raw config, opens the ACP session database, creates an `internal/acp.Agent`, and attaches it to an ACP agent-side stdio connection.

ACP sessions are long-lived relative to prompt turns. Session state includes:

- working directory supplied by the client;
- selected model profile ref;
- selected thinking level when configured;
- client-provided MCP servers;
- lazily-created runtime for the selected model/tool set;
- cancellation function while a prompt turn is active.

Runtime creation resolves the selected model profile, renders the system prompt, initializes a provider generator through `internal/agent`, registers built-in tools, connects configured and client-provided MCP servers, registers code mode when enabled, and registers compaction when configured.

The prompt loop persists the dialog, calls the provider generator, sends ACP session updates for assistant content/tool activity/usage, executes tool callbacks, injects compaction warnings when configured thresholds are crossed, and records the session's latest message pointer.

## Configuration Model

CPE separates configuration into two layers:

- `RawConfig`: file-level YAML schema.
- `Config`: effective runtime settings for one selected model profile.

Every `models` entry is a self-contained runtime profile. CPE resolves the selected profile as written and does not infer shared fields from other profiles.

Selection and override rules:

- ACP sessions store the selected model profile ref and thinking level.
- Local inspection commands use `--model` or `CPE_MODEL` only when they need to resolve profile-specific data, such as a rendered system prompt or MCP server list.
- Runtime generation overrides, when supplied, layer over the profile's generation parameters.
- Runtime timeout override, when supplied, wins over the profile timeout, then the built-in default.

Relative `systemPromptPath` values resolve relative to the config file location when possible. ACP session storage is not part of YAML config; it is selected with `cpe acp serve --db-path` or `CPE_DB_PATH`.

## Session and Message Persistence

CPE stores ACP sessions separately from messages. An ACP session points at its latest message ID, and messages are stored as parent-linked trees.

This supports ACP session behavior:

- loading a session reconstructs and replays its dialog as ACP session updates;
- resuming a session continues from its latest message;
- forking a session creates a new ACP session pointing at the same latest message, so future messages diverge as separate branches;
- deleting a session removes only messages that are not reachable from other ACP sessions.

Returned `gai.Message` values include storage metadata in `ExtraFields`, such as message ID, parent ID, creation time, compaction parent, and model usage metadata. This keeps the runtime centered on `gai.Message` rather than introducing storage wrapper types throughout the codebase.

## Model Runtime

`internal/agent` constructs provider-specific generators and provides reusable wrappers. ACP owns the session loop and persistence policy; agent owns provider setup and shared provider behavior.

Provider-specific behavior is isolated in the runtime layer. Examples include OAuth-backed transports, Responses API request shaping, thinking-block filtering, and provider-specific generator creation.

The runtime does not pretend all providers have identical capabilities. Instead, CPE exposes a common enough model for ACP sessions and local inspection commands while keeping provider branches near generator initialization.

## MCP Integration

CPE acts as an MCP client inside ACP sessions and in MCP inspection commands.

The MCP runtime is responsible for:

- creating transports for `stdio`, `http`, and `sse` servers;
- applying per-server timeouts;
- applying static headers and environment variables;
- listing and filtering tools;
- rejecting duplicate tool names across registered tools;
- converting MCP tool metadata and tool calls into `gai` tools and callbacks;
- sending ACP tool-call updates while MCP tools execute.

Configured MCP servers from the selected model profile are merged with MCP servers supplied by the ACP client. Duplicate server names fail fast to avoid ambiguous tool behavior.

## Built-in Tools

CPE registers some tools directly rather than through MCP.

The bundled `text_edit` tool allows file creation and exact text replacement, and emits ACP diff content so clients can display file changes. A model profile can set `disable_edit_tool: true` to omit it.

Code mode registers `execute_go_code` when enabled. Compaction registers `compact_conversation` when configured.

## Code Mode

Code mode asks the model to write a complete Go source file implementing:

```go
Run(ctx context.Context) ([]mcp.Content, error)
```

CPE compiles and runs the generated file in a temporary harness with timeout and output limits. Recoverable failures such as compile errors, runtime panics, and execution timeouts are returned as tool results so the model can iterate. Fatal harness failures stop execution.

MCP tools are intentionally not exposed as generated Go bindings. They remain conversational tools registered in the ACP session runtime.

## Rendering and Local Commands

Terminal rendering is now limited to local inspection commands such as MCP tool inspection and code mode description output. ACP session content is delivered to clients as protocol updates rather than rendered terminal markdown.

`internal/render` still centralizes TTY-aware markdown/plain-text rendering for command output.

## Authentication

Most providers use API keys from environment variables chosen by each model profile's `api_key_env` field. OAuth-backed flows live in `internal/auth` and are exposed through `cpe account` commands where supported.

The account commands are local helpers. ACP clients still launch the same CPE process, so required credentials must be visible to that process.

## Testing Strategy

Tests should exercise behavior at the narrowest useful boundary:

- ACP RPC behavior through an ACP client/server connection when notification and transport semantics matter.
- Direct `Agent` or `Loop` calls for branches that cannot be observed through RPC alone.
- Command helpers in `internal/commands` without Cobra where possible.
- Config resolution separately from command parsing.
- Storage behavior with both SQLite and `MemDB` where appropriate.
- MCP and provider integration tests behind the opt-in gates documented in `internal/testutil/testgate/doc.go`.

## Current Invariants

- `cpe acp serve` is the primary runtime entrypoint.
- `internal/cmd` remains Cobra-only process-edge wiring.
- `internal/acp` owns session lifecycle and prompt execution.
- `internal/agent` owns provider generator construction and shared provider wrappers.
- Model profiles are self-contained runtime profiles; CPE resolves the selected profile as written.
- ACP session persistence remains SQLite-backed by default and message-tree-based.
- MCP tools, built-in tools, code mode, and compaction are registered per session runtime.
- Duplicate MCP server names or tool names fail fast.

## Open Questions

- How much CPE should expose editor-specific affordances beyond the ACP protocol surface.
- Whether MCP server connections should be pooled across active ACP sessions instead of initialized per session runtime.
- How broadly to expose model capability metadata through ACP session updates.
- How much terminal-oriented helper functionality should remain as the ACP client ecosystem matures.
