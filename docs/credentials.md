# Credentials

[All guides](README.md)

CPE can use an API key supplied in a model's configuration or credentials saved
through `cpe auth login`. Explicit model keys take precedence. Saved credentials
are attached only to the supported HTTPS provider hosts.

## Signing in

ChatGPT login normally prints a browser link and waits for a callback on the
local machine. Device login is also available for SSH sessions or a browser on
another machine. It displays a code to enter at OpenAI's sign-in page. The login
waits up to fifteen minutes and can be canceled with Ctrl+C.

OpenCode Go login accepts an API key through a masked terminal form. It checks
the key locally. Subscription status and available credits remain a matter for
the service when requests are made.

ACP authentication checks credentials established by the login flow. It can
renew ChatGPT credentials when needed; it does not open an interactive login
window. See the [main README](../README.md#authentication) for login commands and
endpoint configuration.

## Where credentials live

Saved credentials share a plaintext `~/.cpe/auth.json` file, with an entry for
each provider. On Unix, CPE writes the file with private permissions and creates
its configuration directory with access limited to the user. On Windows, protect
the directory with filesystem ACLs. Treat the file as a password. A model
configuration containing API keys deserves the same protection.

Login replaces only the chosen provider's entry. CLI logout removes only that
provider's entry; ACP logout removes both saved subscription entries. Other
provider data is preserved during updates, including concurrent commands.
A failed or canceled update to a provider's entry leaves its previous saved
credentials in place.

Logout removes local access information. Provider-issued keys and remote
sessions remain active until revoked through the provider. Keys written directly
in model configuration also remain there.

Stored ChatGPT credentials are used for a Responses model on the exact HTTPS
host `chatgpt.com`. Stored OpenCode credentials can be used for models on the
exact HTTPS host `opencode.ai`. They are never attached to other hosts or plain
HTTP. The usual ChatGPT subscription endpoint is shown in the README.

## Refreshing ChatGPT access

ChatGPT credentials are checked before every provider request, including later
requests in the same turn. When the access token is within five minutes of
expiry, CPE starts a background refresh.

```text
 Current token
      |
      +-- comfortably valid ---> use current token
      |
      +-- near expiry ---------> start background refresh
      |                          keep using unexpired token
      |
      +-- expired -------------> wait for renewal and saving
                                 then use saved replacement
```

An unexpired token remains usable while refresh is pending or failing. Refresh
attempts have a thirty-second timeout and retry with increasing delays, from
one second up to thirty seconds. Exchange retries stop when the existing token
expires. Credentials already expired when renewal begins get one bounded
exchange attempt.

A successful exchange can rotate the refresh token. CPE keeps the replacement
credentials while retrying a failed save, even past the old access token's
expiry. It retries saving that replacement without exchanging the old refresh
token again. New credentials become available to requests after they are saved.
A concurrent login or logout takes precedence over the pending refresh.

Expired tokens are never sent. A request needing expired credentials waits for
renewal and persistence, subject to its own cancellation or deadline. If usable
credentials cannot be obtained, the request fails. Revoked refresh credentials
require signing in again.

Refresh work belongs to the ACP connection and can continue after the turn that
triggered it ends. ACP logout and connection shutdown stop it. API keys,
including OpenCode keys, have no automatic refresh.

These credential retries are separate from
[model-response retries](failures.md). A credential failure cannot be repaired
by blindly repeating the agent's earlier tools.
