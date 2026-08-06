# Tech Debt

Various tech debt accumlated in the codebase that we must eventually get to

## Context Propogation

- have various lifetimes for different operations controlled by context propogation, need to think about how we want to do this cleanly
- mcp init should be cancelled, but once init, should last lifetime of acp session
- if prompt turn is cancelled, should cancel downstream tasks like tool call execution
- runtime creation for acp session should have context for building (maybe?), but build time context should not be leaked into runtime resources whose lifetime extends to the lifetime of a whole session
- session config changes close the active runtime while a prompt can still be using it; should either defer runtime close/recreate until the active prompt finishes, reject config changes during active generation, or cancel first
- CloseSession closes the runtime without first cancelling an active prompt, unlike DeleteSession
- runtime Close currently cancels the session runtime context after MCP close returns; if MCP close blocks, the context that can kill stdio MCP servers is not cancelled soon enough
- OpenAI account login reports any callback context cancellation as an authentication timeout instead of distinguishing cancellation from deadline expiry

## Edit tool

- apply patch tool for gpt style models
- support custom tools for gpt

## Config

- config is gradually getting complicated, with system templates and yaml anchors, change to using starlark config, allows for code resuse, more complicated system prompt buiding, compaction building
- hot reload config on change

## Persistence

- compaction persists the successful tool result and replacement root in separate `SaveDialog` transactions because the root links to the persisted result ID
- if replacement-root persistence fails, the successful result remains orphaned; CPE currently panics to prevent `Agent.Prompt` from advancing the session to that false-success branch
- add a storage operation that atomically persists the successful result and linked replacement root, then replace the invariant panic with normal error handling

## Errors

- need to report compete error response
- need infinite retries if not phase problem and not 429
  - on 429, just wait until rate limits reset