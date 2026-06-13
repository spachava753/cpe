// Package cmd defines CPE's Cobra command tree.
//
// It intentionally lives at the import path internal/cmd because this wiring is
// process-private and not part of CPE's public import surface.
//
// This package is intentionally thin: it binds flags, validates CLI-level
// arguments, and delegates feature logic to internal packages. The primary
// runtime entrypoint is `cpe acp serve`, which starts the ACP server in
// internal/acp; the remaining commands are local inspection and account helpers.
//
// Contract:
//   - keep command handlers focused on Cobra wiring and CLI argument mapping;
//   - keep ACP runtime behavior in internal/acp and framework-agnostic command
//     helpers in internal/commands;
//   - reserve side effects in init() for command/flag registration.
package cmd
