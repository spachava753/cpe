// Package repl implements a durable Starlarkx REPL with Dyson capabilities.
// It journals host boundary arguments, return values, and error identities before
// exposing them to interpreted code. Replay runs the same source against the
// journal and never invokes the live host closures. Missing, extra, or reordered
// calls are fatal replay errors. File handles and HTTP objects are reconstructed
// by Dyson from recorded capability results, preserving native Starlark types.
//
// Successful chunks commit interpreter state. Ordinary failures, timeouts, and
// cancellation roll it back while retaining their audit trail and external effects.
// Cancellation caused by ErrSteered instead commits the partial globals/mutations,
// output, images, and an exact per-evaluation instruction count and stop phase.
// CPE owns the interpreter thread; context cancellation is observed synchronously
// before opcodes, or by context-aware host capabilities. Noncancellable builtins
// settle before the next opcode. Builtin-specific deadlines remain effective.
// Replay stops before the recorded opcode or lets its recorded host error return
// during that opcode. Counts, phase, host records, output, and images must match.
// Subprocess terminal builtin outcomes are journaled after its timeout check,
// separately from the host runner result. Replay omits that fresh timer, rebuilds
// native values through Dyson, and supplies the recorded builtin error/success.
// No wall-clock timing controls replay's stop.
// Captured subprocess stdout/stderr also enter the result when steered.
// An unfinished chunk after a crash has no exact cutoff: it is marked interrupted
// and rolled back, never executed or inferred from a pending steering request.
// Supported modules are os, glob, json, re, requests, subprocess, and time.
// Environment mutation, process signaling, tempfile, pwd, and grp are excluded.
// Replay reconstructs file handles without opening files. On a new host call,
// surviving handles reattach by path at their saved offset, with create/exclusive/
// truncate flags removed. Reattachment observes the current filesystem; it does
// not resurrect deleted files or retain inode identity across process restarts.
//
// Injected tool parameters use json.Number recursively, preserving large integer
// arguments through schema checks, journaling, and callbacks. Each new eval_start
// records toolArguments=1. Missing/zero replays the original float64 argument
// encoding and Google schema validator of older sessions; version 1 validates
// exact numbers with the pinned jsonschema/v6 validator and no external schema
// loader. Unknown versions fail closed. Subsequent live
// evaluations always use the current format, including after rollback or branching.
//
// repl.star provides emit_image(image) for MCP image dictionaries and
// emit_image(data, mime_type="image/png") for base64 strings or Starlark bytes.
// PNG, JPEG, GIF, and WebP headers must match their MIME type. At most 20 MiB of
// decoded image bytes may be emitted per evaluation, separately from text limits.
// Images are ordered, saved in eval_end, and compared during committed replay.
// Like printed output, images emitted before a failed chunk remain in its result;
// neither rollback nor a later evaluation inherits the pending image list.
// Emission is pure: it performs no file/network I/O and needs no host-call record.
package repl
