# CLI

## File organization

- Group production code by top-level command: `agent.go` (`acp`), `auth.go`,
  `mcp.go`, `plugins.go`, and `skills.go`. Keep each group's registration,
  subcommand builders, flags, help, and action bindings together.
- Add subcommands to their existing group file. Do not create one file per
  subcommand.
- Keep root command construction and group wiring in `root.go`.
- Separate helpers only when they serve multiple command groups, as `scope.go`
  does, or form a self-contained concern with embedded text, as
  `system_prompt.go` does for `acp`. Keep other group-specific helpers in the
  group's file.

## Command adapters

- Keep this package focused on Cobra: parse arguments and flags, build operation
  options, and delegate to the relevant internal package. Filesystem operations,
  source acquisition, and other domain behavior belong in those packages.
- Forward the command context and input, output, and error streams explicitly.
- Keep group action bindings private and injectable through the group's
  `*CmdWith*` helper, such as `skillsCmdWithActions` or `mcpCmdWithConnector`,
  so adapter tests do not run domain operations.

## Tests

- Test only command plumbing: registration, help, aliases, argument and flag
  validation and translation, dependency/context/stream forwarding, and error
  propagation.
- Inject fakes through the `*CmdWith*` helpers. Do not run real package-specific
  command functions, acquire sources, start external MCP servers, or mutate
  installations or credentials in CLI tests.
- Keep adapter and help tests in `<group>_test.go`; extend the existing test tables
  for new subcommands, flags, and invalid invocations.
- Test domain behavior in its owning package (`skills`, `plugins`, or `auth`).
  Do not add CLI integration or lifecycle tests.
