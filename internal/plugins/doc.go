// Package plugins loads and manages native Agent Plugins 1.0.0 packages,
// including skills and stdio, Streamable HTTP, and SSE MCP configuration.
// Invalid manifests are fatal; component failures become Plugin.Diagnostics.
//
// Manager returns parsed results; Install, List, and Remove also resolve command
// scope and write terminal or JSON summaries. Runtime activation, placeholder
// expansion, authentication, and persistent plugin data are the launcher's responsibility.
package plugins
