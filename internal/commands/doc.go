/*
Package commands contains framework-agnostic command helpers for CPE's local
inspection and account-management commands.

The internal/cmd package builds Cobra flags and delegates command execution to
this package for deterministic testing. The ACP server runtime lives in
internal/acp; this package remains focused on command-line utilities that are
useful around that runtime.

Feature areas include:
  - account authentication and usage management commands;
  - model profile inspection commands;
  - system prompt rendering for a selected model profile;
  - MCP client inspection commands (inspect servers/tools, call tools, code mode help).

Cross-package contract:
commands functions receive explicit option structs and interfaces rather than
reading global state directly. This keeps command handlers deterministic and
test-friendly while leaving ACP session behavior to internal/acp and model/tool
runtime assembly to internal/agent.
*/
package commands
