// Package responses bridges gai's Responses adapter to the SDK's complete usage
// fields. The pinned gai version omits cache writes; this wrapper preserves them
// for both API-key Responses and Codex. Each request has a separate collector.
// Terminal failure usage is forwarded before its error so billed work is retained.
// Only present, non-null counters are reported. An explicit zero is known usage;
// absent input/output counters remain unknown rather than becoming free requests.
package responses
