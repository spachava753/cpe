/*
Package agent assembles provider generators and shared model-runtime helpers used
by CPE's ACP server.

It is the runtime assembly layer between resolved configuration, model providers,
MCP/built-in tools, and ACP session execution. The ACP protocol loop itself lives
in internal/acp; this package owns provider initialization and reusable generator
wrappers.

Major responsibilities:
  - construct provider-specific generators (OpenAI, Anthropic, Gemini, etc.)
    with API key or OAuth authentication;
  - provide generator wrappers such as provider-specific block filtering and
    Responses API request normalization;
  - expose shared model/type helpers used when ACP sessions register built-in,
    MCP, code-mode, and compaction tools.

Related packages:
  - internal/acp owns ACP session lifecycle, prompt execution, persistence,
    session updates, and skill slash commands;
  - internal/config owns configuration loading, model profile inspection helpers,
    and system prompt template rendering;
  - internal/mcp owns MCP runtime integration and MCP inspection helpers;
  - internal/skills handles skill discovery and prompt metadata;
  - internal/codemode owns the starlark_repl tool and Dyson integration.

Behavioral notes:
  - model HTTP transports and provider SDKs make one request attempt; a generator
    wrapper owns transient provider retries, using jittered exponential delays
    capped at two minutes for up to twelve hours; provider reset times exposed
    by gai API errors may schedule a later retry within that overall budget;
  - provider block filtering preserves only provider-compatible thinking blocks
    when a session crosses model providers;
  - starlark_repl tool-description helpers live in internal/codemode so ACP
    runtime callbacks and command-side inspection output share one contract.
*/
package agent
