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
  - internal/commands handles local inspection commands around model profiles and
    MCP servers;
  - internal/prompt handles system prompt template rendering;
  - internal/skills handles skill discovery and prompt metadata;
  - internal/codemode owns the execute_go_code tool and sandbox execution.

Behavioral notes:
  - provider block filtering preserves only provider-compatible thinking blocks
    when a session crosses model providers;
  - execute_go_code formatting helpers live in internal/codemode so ACP runtime
    callbacks and command-side inspection output can share them.
*/
package agent
