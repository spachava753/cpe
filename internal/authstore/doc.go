// Package authstore owns CPE's shared credential document. auth.json is an
// object keyed by service (openai-codex, opencode-go, github-copilot), independent
// of model identity and wire protocol. Service adapters parse their own entries
// and own authorization/refresh; this package owns persistence only.
//
// Every transaction reloads the document under auth.json.lock, including token
// refresh. Writes preserve other entries, use private temporary files, sync, and
// atomically replace the document. Locks remain on disk and release on process
// exit. Cancellation before replacement leaves ordinary updates unchanged. Once
// a token refresh returns a validated replacement, Refresh commits it even if the
// caller canceled, since the remote service may have invalidated the old token.
// Availability checks use a read-only snapshot without waiting on refresh. Credential
// symlinks, non-object documents, and duplicate keys are rejected; parse errors
// never include credential contents.
//
// LegacyPath enables lazy migration when the service entry is absent. The
// adapter must accept the legacy value before it is persisted. Existing entries
// always win, including invalid entries: stale legacy tokens cannot silently
// replace them. Legacy files remain untouched as backups and are not updated.
// Stop older CPE versions before migrating; they do not share this storage for
// OpenCode Go or Copilot. New logins omit LegacyPath and can replace a broken
// service entry without trusting its legacy file. The original Codex entry and
// Copilot version-1 payload retain their existing formats.
package authstore
