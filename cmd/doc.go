// Package cmd defines CPE's Cobra command tree.
//
// This package is intentionally thin: it binds flags, validates CLI-level
// arguments, and delegates feature logic to internal packages, primarily
// internal/commands (with internal/version as the one process-level exception).
//
// Contract:
//   - keep command handlers focused on Cobra wiring and CLI argument mapping;
//   - keep runtime dependency resolution and business logic in internal packages
//     for testability and future CLI-framework migration;
//   - reserve side effects in init() for command/flag registration.
package cmd
