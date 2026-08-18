// Package acpstar exposes persisted ACP session history to CPE's Starlark REPL.
//
// Module returns a read-only acp.star module whose values project storage
// messages into immutable Starlark session, message, and block types. Current
// session reads use the persisted assistant-message cutoff attached to the
// active evaluation context by the ACP loop.
package acpstar
