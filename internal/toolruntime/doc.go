// Package toolruntime provides the model's two tools, bash and starlark_exec,
// along with their StarlarkX builtins, MCP tool calls and discovery, and output
// truncation and overflow. Compaction requests are handed to the caller; the
// runtime does not own conversation records or decide checkpoint boundaries.
package toolruntime
