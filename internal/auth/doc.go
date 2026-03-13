/*
Package auth implements OAuth support and credential storage for providers that
CPE can authenticate without API keys (currently Anthropic and OpenAI).

It provides:
  - PKCE/state helpers and provider-specific authorization URL builders;
  - callback handling for browser-based login flows;
  - token exchange and refresh functions;
  - credential persistence in a per-user config file;
  - HTTP round trippers that inject bearer tokens and refresh automatically;
  - provider-specific account usage helpers such as the OpenAI ChatGPT usage API.

Credential storage contract:
credentials are stored under the user's config directory in a JSON file with
0600 permissions and are keyed by provider.

Transport contract:
OAuth transports refresh expiring tokens before requests and mutate request
headers/body as required by provider backends (for example Anthropic beta
headers and OpenAI backend-specific fields).
*/
package auth
