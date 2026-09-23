# Configuration examples

Copy `codex.json` to `~/.cpe/config.json` and `system.md` to `~/.cpe/system.md`
for Codex OAuth, then start CPE and type `/login`. Use `/login device` for a
remote terminal. Credentials are stored separately in `~/.cpe/auth.json`.
`cpe --init` creates starter files without overwriting existing configuration.

`config.json` demonstrates an API-key profile. Set a real model ID and export
the credential environment variable named by `api_key_env`.
Its token prices are illustrative; replace them with the rates for your provider.
`context_window` is your preferred input-token budget, with auto-compaction at
90%. It may be smaller than the provider's actual window to control costs.

`cost` rates are USD per million tokens for uncached input, output, cache reads,
and cache writes. Omit the object for unknown pricing, or explicitly use zero for
free categories. `cost.long_context` can replace all rates above an input-token
threshold. The Codex example uses [GPT-6 Astra API rates](https://developers.openai.com/api/docs/models/gpt-6-astra)
checked on 2026-09-23; costs under OAuth are API-equivalent estimates, not your
subscription bill. `/usage` displays durable session totals across compactions.

Configuration is plain JSON: no comments, duplicate keys, or trailing commas.
Agent and compaction sections may be omitted to use their defaults.
