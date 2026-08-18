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
    session updates, skill slash commands, and the starlark_repl code-mode tool;
  - internal/config owns configuration loading, model profile inspection helpers,
    and system prompt template rendering;
  - internal/mcp owns MCP runtime integration and MCP inspection helpers;
  - internal/skills handles skill discovery and prompt metadata;

Behavioral notes:
  - model HTTP transports and provider SDKs make one request attempt; generator
    wrappers own retries at the provider and network boundaries;
  - transient provider failures use jittered exponential delays capped at two
    minutes for up to twelve hours, and provider reset times exposed by gai API
    errors may schedule a later retry within that overall budget;
  - propagated network and HTTP disconnect errors receive up to three retries,
    each after a fixed five-second delay;
  - provider block filtering preserves only provider-compatible thinking blocks
    when a session crosses model providers;
  - starlark_repl tool-description helpers live in internal/acp so ACP runtime
    callbacks and command-side inspection output share one contract.
*/
package agent
