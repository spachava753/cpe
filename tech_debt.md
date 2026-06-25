# Tech Debt

Various tech debt accumlated in the codebase that we must eventually get to

## Context Propogation

- have various lifetimes for different operations controlled by context propogation, need to think about how we want to do this cleanly
- mcp init should be cancelled, but once init, should last lifetime of acp session
- if prompt turn is cancelled, should cancel downstream tasks like tool call execution
- runtime creation for acp session should have context for building (maybe?), but build time context should not be leaked into runtime resources whose lifetime extends to the lifetime of a whole session
- execute_go_code tool should report context cancelled instead of erroring

## Edit tool

- apply patch tool for gpt style models
- support custom tools for gpt

## Config

- config is gradually getting complicated, with system templates and yaml anchors, change to using starlark config, allows for code resuse, more complicated system prompt buiding, compaction building
- hot reload config on change
