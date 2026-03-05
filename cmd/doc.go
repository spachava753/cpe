// Package cmd defines CPE's Cobra command tree.
//
// This package is intentionally thin: it binds flags, validates CLI-level
// arguments, and delegates feature logic to internal packages (primarily
// internal/commands, internal/config, and internal/storage).
//
// Contract:
//   - keep command handlers focused on wiring and dependency construction;
//   - keep business logic in internal packages for testability;
//   - reserve side effects in init() for command/flag registration.
package cmd
