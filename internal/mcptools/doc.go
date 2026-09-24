// Package mcptools connects MCP servers to the durable Starlark REPL. Connect
// initializes a client and discovers all pages of tools. The caller owns Close;
// the resulting tools are injected through agent.Options.Tools, never exposed
// directly to the model. Tool catalogs are snapshots taken at connection time.
//
// Functions are named mcp_<server>__<tool> in tools.star. Server names must be
// ASCII identifiers; punctuation in tool names becomes underscores. Ambiguous
// names are rejected rather than silently replacing a tool. Descriptions carry
// the original MCP names and the result envelope contract.
//
// Calls preserve the MCP result envelope: content (text, image, audio, resources),
// structuredContent, isError, and metadata. Binary content is base64 in Starlark
// dictionaries. Tool-level isError results remain inspectable values; protocol
// and transport errors fail the evaluation. The REPL journals calls and results,
// so restoring interpreter state never invokes CallTool. Initialization and
// discovery are connection setup, outside evaluation history. No sampling,
// elicitation, resource fetching, or automatic retries of tool calls are added.
// The SDK is pinned to 8075fb3cf313 for lossless structuredContent numbers; using
// its legacy structuredcontentfloat64 compatibility mode forfeits that guarantee.
package mcptools
