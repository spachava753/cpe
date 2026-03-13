/*
Package commands contains framework-agnostic command orchestration and wiring.

The cmd package builds Cobra flags, resolves process-level dependencies, and
then delegates command execution to this package for deterministic testing.
This package keeps entrypoint flows thin and focused on composition rather than
owning agent feature business logic.

Feature areas include:
  - root generation flow orchestration;
  - account authentication and usage management commands;
  - model and configuration management commands;
  - conversation persistence operations (list, print, delete);
  - MCP client commands (inspect servers/tools, call tools, code mode help);
  - MCP server mode and subagent execution.

Cross-package contract:
commands functions receive explicit option structs and interfaces rather than
reading global state directly. This keeps command handlers deterministic and
test-friendly while leaving runtime agent behavior to internal/agent.
*/
package commands
