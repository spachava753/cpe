// Package repl implements a durable Starlarkx REPL with Dyson capabilities.
// It journals host boundary arguments, return values, and error identities before
// exposing them to interpreted code. Replay runs the same source against the
// journal and never invokes the live host closures. Missing, extra, or reordered
// calls are fatal replay errors. File handles and HTTP objects are reconstructed
// by Dyson from recorded capability results, preserving native Starlark types.
//
// Only successful chunks commit interpreter state. Failed/canceled chunks retain
// their audit trail and external effects but rebuild the previous interpreter
// state. An unfinished chunk on restart is marked interrupted, never executed.
// Supported modules are os, glob, json, re, requests, subprocess, and time.
// Environment mutation, process signaling, tempfile, pwd, and grp are excluded.
// Replay reconstructs file handles without opening files. On a new host call,
// surviving handles reattach by path at their saved offset, with create/exclusive/
// truncate flags removed. Reattachment observes the current filesystem; it does
// not resurrect deleted files or retain inode identity across process restarts.
package repl
