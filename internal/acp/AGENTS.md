# AGENTS.md

Package-specific guidance for `internal/acp`.

- Treat ACP as bidirectional and concurrent. The client can make RPC calls while the agent sends notifications, and multiple calls may target the same session. Do not assume request serialization; mutate active session state through the existing `sync.Guard` pattern.
- Keep file ownership narrow: session lifecycle in `session.go`, session config in `session_config.go`, skill slash-command ACP adaptation in `skill_commands.go`, ACP/GAI conversions in `gai.go`, and model/tool turn execution in `loop.go`.
- Validate `session/load` and `session/resume` request working directories against persisted session metadata before reusing or creating active state. The match is exact because ACP session `cwd` is immutable.
- Advance a session using the last message observed before generation as the optimistic concurrency expectation. If storage returns `ErrSessionConflict`, panic because more than one ACP process owns the same session; do not treat it as a recoverable prompt result or clean up the losing branch during prompt handling.
- Keep `gai.go` as the translation boundary for ACP content blocks, `gai` blocks, tool calls/results, and session updates. Do not bury protocol conversion inside prompt/session logic unless it genuinely needs session state.
- When ACP protocol docs and current behavior disagree, prefer tests and code that match the protocol. The protocol can require behavior that is not obvious from the current implementation.
- Defer expensive or session-derived setup until the client has an active session ID. Runtime creation is lazy on first valid `session/prompt`. Skill slash-command discovery/publication is refreshed and sent from `session/prompt` and `session/set_config_option`, before the prompt turn or config mutation runs. Do not publish skill commands from `session/new`, `session/fork`, `session/load`, or `session/resume`.
- Prefer testing through the ACP client/server connection for RPC behavior. Direct `Agent` calls are only for branches that cannot be observed through notification/transport semantics.
- In tests, keep setup explicit and auditable. Group scenarios with `t.Run` when they exercise the same public method, but avoid clever shared fixtures that hide what each scenario is doing.
- For understanding the update-to-date ACP documentation, you peruse https://agentclientprotocol.com/llms.txt
