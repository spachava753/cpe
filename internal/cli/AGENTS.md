# CLI

## File organization

- Group production code by top-level command: `auth.go`, `plugins.go`, and
  `skills.go`. Keep each group's registration, subcommand builders, flags, help,
  and action bindings together.
- Add subcommands to their existing group file. Do not create one file per
  subcommand.
- Keep root command construction and group wiring in `root.go`.
- Separate helpers only when they serve multiple command groups, as `scope.go`
  does. Keep group-specific helpers in the group's file.

## Command adapters

- Keep this package focused on Cobra: parse arguments and flags, build operation
  options, and delegate to the relevant internal package. Filesystem operations,
  source acquisition, and other domain behavior belong in those packages.
- Forward the command context and input, output, and error streams explicitly.
- Keep group action bindings private and injectable through the existing
  `*CmdWithActions` helpers so adapter tests do not run domain operations.

## Tests

- Keep adapter and help tests in `<group>_test.go`; extend the existing test tables
  for new subcommands, flags, and invalid invocations.
- Keep real command lifecycle tests in `<group>_integration_test.go`, using
  temporary fixtures and the real domain operations.
