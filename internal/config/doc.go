// Package config loads ~/.cpe/config.json and ~/.cpe/system.md. The directory
// is fixed on Unix (including macOS) and Windows; XDG and project configuration
// are not consulted. JSON must contain one object and rejects unknown fields,
// duplicate keys, and trailing data. Init creates private missing starter files
// without overwriting existing configuration; the starter profile uses Codex.
// Init also creates themes.json. Theme loading is independent of model config
// and is handled by package theme only for the interactive TUI.
// tui.submit_key is enter (the default) or shift+enter. The other key inserts
// a newline; Ctrl+J remains a newline fallback. This preference is read at
// startup and only controls the composer, not pickers or private login input.
//
// API providers read named environment variables. Codex instead uses CPE's own
// ~/.cpe/auth.json with TUI /login and automatic refresh. There is no credential
// path setting or fallback to Pi. Codex uses a fixed endpoint and rejects API-key,
// output-limit, and temperature options. Every provider accepts reasoning_effort,
// including Chat Completions profiles pointing at compatible endpoints. CPE
// recognizes common effort labels plus adaptive/disabled thinking modes; gai
// translates the setting and the adapter/model determines supported values.
// Interactive reasoning changes use the same validation as file configuration.
// credential=opencode-go selects a saved Go API key with an openai, responses, or
// anthropic protocol. Such profiles omit api_key_env/base_url; CPE fixes endpoints.
// AddModels parses the merged configuration and atomically adds missing profiles
// without changing existing names or defaults. Imports follow config symlinks.
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
//
// mcp_servers maps server identifiers to either command/args/env (stdio) or url
// (Streamable HTTP). The CLI connects and discovers tools at startup, with
// agent.tool_timeout bounding each connection. Commands inherit the environment
// and working directory; env entries override inherited values. Exactly one
// transport is required. HTTP URLs must omit userinfo/fragments. MCP OAuth,
// legacy HTTP+SSE endpoints, and dynamic catalog updates are not configured here.
package config
