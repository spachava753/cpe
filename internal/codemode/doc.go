/*
Package codemode implements CPE's starlark_repl feature.

Code mode evaluates model-generated Starlark in a session-scoped REPL. A Dyson
Sphere owns the Starlark thread, globals, compatibility library, and retained
resources. CPE enables filesystem, process, and HTTP access, and exposes a
view_file builtin that returns local binary artifacts as multimodal tool-result
blocks.

REPL globals persist between tool calls. A successful conversation compaction
closes and discards the Sphere so the next evaluation starts with a fresh thread
and state does not grow for the lifetime of a long conversation.

Execution timeouts and prompt cancellation are propagated to the Sphere, which
cancels its Starlark thread. Syntax, runtime, and timeout failures are returned as
tool results so the model can iterate, while output is bounded by the configured
large-output limit.
*/
package codemode
