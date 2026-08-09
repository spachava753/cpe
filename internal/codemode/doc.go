/*
Package codemode implements CPE's starlark_repl feature.

Code mode evaluates model-generated Starlark in a session-scoped REPL. A Dyson
Sphere owns the Starlark thread, globals, compatibility library, retained
resources, and CPE's read-only acp.star module from the nested codemode/acp
package. The module exposes persisted ACP session discovery and complete
compaction-aware history as immutable acp.Session, acp.Message, and acp.Block
values. Current-session reads use the persisted assistant message containing
the executing tool call as their cutoff, not the session's older committed
head. Session listing defaults to an exact match on the active working directory
and accepts another exact directory explicitly. CPE also enables filesystem,
process, and HTTP access, and exposes a view_file builtin that returns local
binary artifacts as multimodal tool-result blocks.

REPL globals persist between tool calls while the owning session runtime remains
active. A Sphere is not serialized with persisted conversation history, so an
ACP session reconstructed by load or resume and every fork starts with a fresh
REPL. ACP warns the model on the next user prompt in those cases. A successful
conversation compaction also closes and discards the Sphere so the next
evaluation starts with a fresh thread and state does not grow for the lifetime
of a long conversation.

Execution timeouts and prompt cancellation are propagated to the Sphere, which
cancels its Starlark thread. Syntax, runtime, and timeout failures are returned as
tool results so the model can iterate, while output is bounded by the configured
large-output limit.
*/
package codemode
