// Package responses bridges gai's Responses adapter to the SDK's complete usage
// fields. The pinned gai version omits cache writes; this wrapper preserves them
// for both API-key Responses and Codex. Each request has a separate collector.
// Terminal failure usage is forwarded before its error so billed work is retained.
package responses
