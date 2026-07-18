/*
Package codemode implements CPE's starlark_repl feature.

Code mode evaluates model-generated Starlark in a session-scoped REPL. A Dyson
Sphere owns the Starlark thread and globals, provides host-backed standard-library
compatibility modules, and exposes a view_file builtin that returns local binary
artifacts as multimodal tool-result blocks.

REPL globals persist between tool calls. A successful conversation compaction
discards the Sphere so the next evaluation starts with a fresh thread and state
does not grow for the lifetime of a long conversation. Dyson's durable recording
and replay features are intentionally disabled.

Execution timeouts and prompt cancellation are propagated to the Sphere, which
cancels its Starlark thread. Syntax, runtime, and timeout failures are returned as
tool results so the model can iterate, while output is bounded by the configured
large-output limit.
*/
package codemode
