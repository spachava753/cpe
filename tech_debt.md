# Tech Debt

Various tech debt accumlated in the codebase that we must eventually get to

## Context Propogation

- have various lifetimes for different operations controlled by context propogation, need to think about how we want to do this cleanly
- mcp init should be cancelled, but once init, should last lifetime of acp session
- if prompt turn is cancelled, should cancel downstream tasks like tool call execution
- runtime creation for acp session should have context for building (maybe?), but build time context should not be leaked into runtime resources whose lifetime extends to the lifetime of a whole session
- execute_go_code tool should report context cancelled instead of erroring
- execute_go_code terminal execution can skip KillTerminal when WaitForTerminalExit returns context.Canceled first, and TerminalOutput uses the cancelled prompt context after cleanup
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

## Errors

- need to report compete error response
- need infinite retries if not phase problem and not 429
  - on 429, just wait until rate limits reset