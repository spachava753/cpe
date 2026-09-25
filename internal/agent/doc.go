// Package agent is CPE's internal SDK. Open accepts configuration, a generator,
// an owned session store, and optional JSON tools. The model sees exactly one
// tool, starlark_repl; injected tools are functions loaded from tools.star.
// MCP connections supply these same tools through package mcptools. REPL images
// become image content blocks in the tool result, with the original call ID.
// Both normal completion and interrupted-result reconciliation preserve them.
// Provider accepts an explicit credential directory and durable conversation ID;
// OpenCode Go uses them for lazy API-key loading and stable per-session headers.
// Options also accepts an immutable skills.Catalog. Model instructions disclose
// only model-invocable skill metadata. Prompt resolves /skill:NAME [arguments]
// before context admission or persistence; unknown and model-only commands fail
// without changing history. Accepted invocations record the original input and
// an absolute SKILL.md path to read through the REPL. Reopen uses these recorded
// messages and host results, without re-resolving historical skill commands.
//
// The gai/agent loop supplies generation and tool hooks. A per-generation
// router selects the current generator and forwards streaming deltas. Complete
// assistant responses are persisted before tools run; results are persisted
// before the next generation. Partial streamed output is provisional. Missing
// historical tool results are reconciled from durable evaluation outcomes, or
// recorded as interrupted failures, never executed again.
//
// Compaction replaces only model context. It appends a summary node and leaves
// all REPL inputs and host outcomes reachable for restoration. Branching selects
// a completed-turn checkpoint and rebuilds its interpreter without host effects.
// If restoration fails, the selected head remains durable and generation/model
// changes are disabled until a successful branch restoration or reopen. The
// displayed dialog follows the selected head, never the abandoned branch.
// One Agent has one operation at a time; the caller owns Store.Close separately.
// QueueModel is the sole concurrent mutation: it replaces a pending selection,
// never interrupts generation or tools, and applies before the next generation
// (including after summary generation) or when the operation finishes. The latest
// selection wins. ApplyQueuedModel drains late selections after joining work;
// SelectedModel and EventModel provide named selection snapshots for presentation.
// All other reads and mutations remain restricted to operation boundaries.
// Streaming and nonstreaming providers may be mixed within one Prompt. Request
// usage/pricing stays attached to the provider that actually generated it.
// SetModel and SetReasoningEffort change subsequent turns and compaction without
// replaying or replacing the interpreter. They must be called between operations.
// These settings are local to the Agent instance and are not stored in its tree.
//
// Generated assistant records have optional version-1 origin metadata, separate
// from gai/provider fields: exact provider/model/service identity and a hash of
// the projected instructions/tools/dialog prefix. Service identity hashes the
// endpoint, credential source, and API-key environment name, never key values.
// Outgoing copies retain only the selected origin's thinking and replay fields;
// public text, tool calls/results, and media remain. Originals stay in the tree,
// so switching A->B->A restores A's compatible thinking. Legacy records without
// an origin retain public content but send no unprovable assistant replay data.
// Unknown/malformed origin versions fail restoration before REPL replay.
// Anthropic thinking must also match its original projected prefix and earlier
// retained thinking chain. A changed prefix drops those blocks on the request
// copy; it does not forbid switching providers or rewrite saved history. Summary
// requests use their own instructions and therefore omit incompatible thinking.
// Gemini imported-call signature handling belongs in its provider adapter.
//
// Context compaction estimates the entire request, including instructions and
// tools, using UTF-8 bytes/3 plus framing overhead, calibrated upward by the last
// reported input count on the active branch for the same provider/model. A
// positive model context_window triggers compaction at 90%; requests still
// estimated over the budget fail before generation, including summary requests.
// New prompts are checked against the prospective normal or summary request
// before being persisted, so definitely oversized input can be shortened/retried.
// This is a heuristic, not a hard provider token or monetary limit. Compaction
// runs at most once per user turn. Oversized new input or a switch to a much
// smaller budget may require reducing input or selecting a larger profile first.
//
// Every model request, including summary generation, durably records its start
// and reported usage. Input, output, cache reads, and cache writes are disjoint
// buckets: gai's inclusive input count is reduced by its cached counts. Cost is
// estimated using a saved pricing snapshot for that request. Totals cover the
// entire file, including inactive branches; compaction, branch, model changes,
// price changes, and reopening cannot erase historical consumption. Failures
// retain any reported usage; missing reports and old history are flagged as
// incomplete, never guessed. Token estimates are never added to billed totals.
package agent
