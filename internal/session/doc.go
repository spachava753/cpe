// Package session stores an append-only JSONL conversation and execution tree.
// Every entry has an ID and parent ID. A branch appends a head entry whose parent
// is an earlier checkpoint; history is never overwritten. The last entry is the
// active head. Files are exclusively locked for the lifetime of an open session.
// Appends are fsynced before being acknowledged. An unterminated final line is
// discarded on open; malformed complete records fail closed.
// Resume requires an existing nonempty file. List reads bounded discovery
// metadata for the current directory without writer locks, repair, or replay;
// Open/Resume remain authoritative when a listed entry is selected.
//
// REPL inputs, host call intents, host results, and evaluation outcomes share the
// conversation tree. A host intent is synced before execution and its outcome
// before returning to Starlark. A crash between them means an uncertain external
// effect, never permission to retry it. Replay uses only completed evaluations.
// Ordinary failed or unfinished evaluations roll back state, not host effects.
// A completed eval_end may instead commit a steering interruption: its saved
// instruction count and stop phase reconstruct the partial interpreter state.
// An absent eval_end never implies a known cutoff or permission to retry effects.
// request_start and usage entries record model invocation accounting separately
// from messages. Totals use all entries, including branches and pre-compaction
// history; an unmatched request_start means consumption is unknown after a crash.
package session
