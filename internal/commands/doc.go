/*
Package commands contains CLI business logic independent of Cobra wiring.

The cmd package builds options and dependencies, then delegates command
behavior to this package for execution and testing.

Feature areas include:
  - root generation flow orchestration;
  - model and configuration management commands;
  - conversation persistence operations (list, print, delete);
  - MCP client commands (inspect servers/tools, call tools, code mode help);
  - MCP server mode and subagent execution.

Cross-package contract:
commands functions receive explicit option structs and interfaces rather than
reading global state directly. This keeps command handlers deterministic and
test-friendly.
*/
package commands
