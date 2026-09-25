// Package copilot connects CPE to GitHub.com Copilot subscriptions. Login uses
// GitHub's device flow with the public Copilot client ID, then exchanges the
// GitHub token for a short-lived Copilot token. UI callbacks carry the device
// code only; credentials and login instructions never enter model/session data.
// Cancellation stops polling and response reads. OAuth errors omit response
// bodies and tokens while preserving cancellation/deadline identity.
//
// CPE owns the github-copilot entry in ~/.cpe/auth.json (version-1 payload).
// authstore provides atomic mode-0600 writes and a shared auth.json.lock. Missing
// entries migrate from copilot.json after parsing; the legacy file stays as an
// untouched backup. Existing entries always take precedence. CPE does not
// read credentials from GitHub CLI, Copilot CLI, editors, or environment variables.
// Each inference reloads credentials under a stable OS file lock and exchanges
// expiring Copilot tokens before sending the request. Revoked GitHub tokens
// require /login copilot again. Credential symlinks and redirects are rejected.
// GitHub tokens go only to GitHub's token exchange; Copilot tokens go only to
// allowlisted GitHub.com Copilot API hosts and known catalog/inference paths.
// Enterprise accounts hosted on github.com are supported; custom GHE hosts are not.
//
// Login discovers the account's /models catalog, importing picker-enabled,
// tool-capable models with supported endpoints and usable limits. Protocol comes
// from supported_endpoints, with Chat Completions as the legacy default when
// omitted, never the vendor name or a third-party catalog.
// Profiles use credential=github-copilot and openai, responses, or anthropic.
// copilot_streaming preserves the catalog capability in configuration; false
// or absent uses nonstreaming generation, including after configuration reload.
// Imports preserve existing profiles/defaults. Discovery/import failures leave
// credentials unchanged; a subsequent credential-save failure can leave imported
// profiles requiring another login. Login never silently changes the active model.
// Budgets start at at most 128k input and 16k output, with output room reserved.
// Reasoning and dollar prices are omitted; subscription usage is not a USD rate.
//
// Requests identify CPE and share the durable session ID. The agent explicitly
// marks the primary response to a user prompt with WithUserInitiation; summaries
// and tool continuations remain agent-initiated even after compaction replaces
// their dialog with a synthetic user message. Images enable the vision header.
// API errors name github-copilot and retain classification, status, retry timing,
// and original causes. Neither inference nor token exchange is retried. Streaming,
// tool results, usage, and provider-specific thinking stay with the wire adapters.
package copilot
