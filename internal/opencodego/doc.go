// Package opencodego connects CPE's providers to the OpenCode Go subscription.
// Login accepts a pasted API key; there is no OAuth exchange. It fetches public
// model IDs from Go and protocol/limit metadata from models.dev's opencode-go
// section of the catalog. Import requires an ID in both the Go endpoint and
// that section, with tool support, usable limits, and no deprecated status.
// Other providers, including OpenCode Zen, cannot supply missing Go metadata or
// establish Go availability, even for identical model IDs. Go-specific endpoint
// corrections for exact documented IDs take precedence over catalog adapters;
// otherwise per-model adapters override the Go provider's default. Vendor-name
// prefixes never determine routing. Unknown or unsupported protocols are skipped.
//
// Profiles are named opencode-go/<id>, with credential=opencode-go and provider
// openai (Chat Completions), responses, or anthropic. Config imports only add
// missing profiles: customizations, existing profiles, and default_model remain
// intact. Repeating login discovers new models without deleting old profiles;
// this import filter does not prune profiles already saved in config.json.
// Working budgets start at at most 128,000 input and 16,384 output tokens, bounded
// by published limits and with output room reserved. Reasoning is left to the
// provider default. Budgets and effort remain editable. Prices are omitted:
// subscription quota rates need not represent the user's actual dollar bill.
//
// The API key is saved atomically with mode 0600 in ~/.cpe/opencode-go.json,
// separate from Codex OAuth. Credential symlinks are rejected. Each complete
// login replaces this single credential; simultaneous logins use the last saved
// key. Public discovery never receives the key and cannot authenticate it; the
// first inference request checks subscription and key validity. Discovery and
// configuration errors leave the previous key untouched. A key-save failure can
// leave imported profiles signed out; retrying login is safe.
//
// Client is lazy, reloads the key on each request, disables redirects, and limits
// credentials to fixed Go inference endpoints. It sends cpe/<version> as user
// agent and x-opencode-session from the durable conversation root ID. Both normal
// turns and compaction share that ID across model switches, branches, and reopen.
// Credentials and login display content must never enter model/session data.
package opencodego
