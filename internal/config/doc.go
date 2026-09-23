// Package config loads ~/.cpe/config.json and ~/.cpe/system.md. The directory
// is fixed on Unix (including macOS) and Windows; XDG and project configuration
// are not consulted. JSON must contain one object and rejects unknown fields,
// duplicate keys, and trailing data. Init creates private missing starter files
// without overwriting existing configuration; the starter profile uses Codex.
//
// API providers read named environment variables. Codex instead uses CPE's own
// ~/.cpe/auth.json with TUI /login and automatic refresh. There is no credential
// path setting or fallback to Pi. Codex uses a fixed endpoint and rejects API-key,
// output-limit, and temperature options. Responses and Codex accept
// reasoning_effort; allowed labels ultimately depend on the selected model.
// Interactive reasoning changes use the same validation as file configuration.
//
// Each profile may set context_window, a preferred input-token budget (zero
// disables it). It takes precedence over the legacy compaction.max_characters
// trigger; automatic compaction starts at 90% of the budget. Context estimates
// do not change provider limits or guarantee a pricing tier. Leave output room
// below the provider's physical context limit when choosing an input budget.
//
// cost optionally supplies input, output, cache_read, and cache_write rates in
// USD per million tokens. All four must be explicit, finite, and nonnegative.
// Omitted cost means unknown, not free. cost.long_context optionally supplies
// all four replacement rates and above_input_tokens, an exclusive threshold
// based on total request input (including caches). Rates apply to the full call.
package config
