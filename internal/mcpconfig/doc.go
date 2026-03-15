/*
Package mcpconfig defines configuration schema types shared between config
loading and MCP runtime packages.

It exists to keep internal/config dependency-neutral with respect to MCP runtime
implementation details while preserving a single source of truth for MCP server
connection settings.
*/
package mcpconfig
