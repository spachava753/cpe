/*
Package mcp provides both MCP client integration (for normal CPE runs) and MCP
server integration (for subagent mode).

Client-side responsibilities:
  - build transports (stdio/http/sse) from configuration;
  - connect to configured servers and fetch tool catalogs;
  - apply per-server tool filtering (enabledTools/disabledTools);
  - detect duplicate tool names across servers;
  - adapt MCP tools and calls to gai tool registration/callbacks.

Server-side responsibilities:
  - expose a single configured subagent as an MCP tool;
  - validate subagent tool input (prompt, runId, optional inputs);
  - run subagent executors and convert results/errors to MCP responses;
  - serve over stdio for MCP protocol compatibility.

Subagent logging integration:
when a logging address is supplied, stdio child transports receive
CPE_SUBAGENT_LOGGING_ADDRESS so nested runs can stream observability events.
*/
package mcp
