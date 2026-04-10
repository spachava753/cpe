/*
Package agent assembles and decorates model generators used by CPE.

It is the runtime orchestration layer between resolved configuration, model
providers, MCP tools, dialog mutation, and user-facing output rendering.

Major responsibilities:
  - construct provider-specific generators (OpenAI, Anthropic, Gemini, etc.)
    with API key or OAuth authentication;
  - apply middleware wrappers for panic recovery, persistence, block filtering,
    tool-result printing, response printing, and token/cost reporting;
  - register built-in and MCP tools, including code-mode integration via
    execute_go_code and conversation compaction tooling;
  - orchestrate generator lifecycle concerns such as dialog restart into fresh
    branches, configurable compaction restart caps, and compaction threshold
    warnings carried through tool results.

Related packages:
  - internal/input handles prompt/file/URL block construction;
  - internal/prompt handles system prompt template rendering and skill helpers;
  - internal/render handles terminal markdown/plain-text renderer setup.

Behavioral notes:
  - saving middleware persists dialogs incrementally so message IDs are
    available to printers during execution;
  - thinking filters preserve only provider-compatible thinking blocks when a
    conversation crosses model providers;
  - execute_go_code formatting helpers live in internal/codemode so both agent
    runtime printers and command-side conversation formatting can share them.
*/
package agent
