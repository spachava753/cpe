/*
Package subagentlog streams subagent execution events from child CPE processes
to the root process for real-time observability.

Architecture:
  - Client emits JSON events over localhost HTTP.
  - Server receives events and forwards them to a handler.
  - EmittingGenerator middleware derives events from tool calls/results and
    thinking blocks.
  - Renderer formats events in concise or verbose mode for stderr output.

Contract:
event emission failures are treated as execution failures so subagent runs do
not silently lose observability.
*/
package subagentlog
