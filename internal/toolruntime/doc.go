// Package toolruntime owns session-scoped Dyson and bash tools, MCP tool discovery,
// and model-facing output. Compaction requests are handed to the caller; the runtime
// does not own conversation records or decide checkpoint boundaries.
package toolruntime
