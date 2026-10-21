// Package toolruntime owns invocation-scoped Dyson execution, session data, subprocesses,
// MCP tool discovery, and model-facing output. Compaction requests are handed to
// the caller; the runtime does not own conversation records or decide checkpoint boundaries.
package toolruntime
