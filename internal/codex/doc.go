// Package codex adapts gai's Responses generator to ChatGPT Codex OAuth.
// CPE owns ~/.cpe/auth.json; it neither reads nor writes Pi credentials. New is
// lazy so the TUI can start while signed out. Login supports browser PKCE with a
// state-validated loopback callback, or device authorization for remote terminals.
// Display callbacks are UI-only and must never enter conversation history.
// Cancellation closes the listener and stops polling; successful login atomically
// saves credentials before returning. Initial authorization expires after 15 minutes.
//
// Each request reloads credentials under a stable OS file lock. Expiring tokens
// are refreshed once and atomically saved before use, with private permissions
// and unrelated fields preserved. The kernel releases locks on exit or crash.
// Credential symlinks are rejected. Tokens and token response bodies never enter
// model context, configuration, or session files. Errors omit sensitive bodies.
//
// Only the fixed Codex endpoint receives bearer credentials; redirects are
// disabled. Requests, token exchanges, and refreshes are not automatically retried.
// Ordinary Generate calls collect SSE streams, including for compaction. Codex's
// completion event alias is normalized and premature stream EOF is an error.
package codex
