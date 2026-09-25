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
package authstore
