/*
Package agent assembles and decorates model generators used by CPE.

It is the runtime orchestration layer between resolved configuration, model
providers, MCP tools, dialog mutation, and user-facing output rendering.

Major responsibilities:
  - construct provider-specific generators (OpenAI, Anthropic, Gemini, etc.)
    with API key or OAuth authentication;
  - apply middleware wrappers for panic recovery, persistence, block filtering,
    tool-result printing, response printing, and token/cost reporting;
  - transform user inputs (prompt text, local files, URLs) into gai blocks;
  - render system prompt templates with helper functions for file inclusion,
    command execution, and skill discovery;
  - register built-in and MCP tools, including code-mode integration via
    execute_go_code and conversation compaction tooling;
  - orchestrate generator lifecycle concerns such as dialog restart into fresh
    branches, configurable compaction restart caps, and compaction threshold
    warnings carried through tool results.

Behavioral notes:
  - saving middleware persists dialogs incrementally so message IDs are
    available to printers during execution;
  - thinking filters preserve only provider-compatible thinking blocks when a
    conversation crosses model providers;
  - code-mode formatting helpers provide stable markdown output for
    execute_go_code tool calls and results.
*/
package agent
