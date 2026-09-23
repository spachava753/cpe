// Package session stores an append-only JSONL conversation and execution tree.
// Every entry has an ID and parent ID. A branch appends a head entry whose parent
// is an earlier checkpoint; history is never overwritten. The last entry is the
// active head. Files are exclusively locked for the lifetime of an open session.
// Appends are fsynced before being acknowledged. An unterminated final line is
// discarded on open; malformed complete records fail closed.
//
// REPL inputs, host call intents, host results, and evaluation outcomes share the
// conversation tree. A host intent is synced before execution and its outcome
// before returning to Starlark. A crash between them means an uncertain external
// effect, never permission to retry it. Replay uses only completed evaluations.
// Failed or interrupted evaluations roll back interpreter state, not host effects.
// request_start and usage entries record model invocation accounting separately
// from messages. Totals use all entries, including branches and pre-compaction
// history; an unmatched request_start means consumption is unknown after a crash.
package session
