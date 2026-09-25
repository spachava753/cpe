// Package cli owns the small terminal command surface. With no flags CPE opens
// a fresh durable session. --resume continues one, --continue selects the newest
// session in the current directory, and --sessions lists matching session files.
// --init creates starter configuration, --model selects a JSON profile, and
// --prompt runs a single non-interactive turn using the same durable agent.
// Configured MCP servers connect before opening the agent and close after it.
// Their functions are injected into the REPL; startup discovery is bounded by
// agent.tool_timeout. Neither CLI mode exposes MCP tools directly to the model.
// Provider construction binds the session's root entry ID, preserving OpenCode
// Go's routing/cache identity on reopen, branch, model switching, and compaction.
// TUI login supports Codex OAuth or OpenCode Go API-key storage/model import.
// Startup discovers skills in ~/.agents/skills and ./agents/skills (relative to
// the working directory), with project skills taking precedence. Diagnostics go
// to stderr without blocking other skills. The same catalog supplies model
// metadata, TUI completion, and /skill:NAME expansion in --prompt mode.
package cli
