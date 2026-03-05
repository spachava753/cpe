/*
Package codemode implements CPE's execute_go_code feature.

Code mode exposes selected MCP tools as strongly typed Go functions, asks the
model to generate a complete Go program, and executes that program in a
temporary sandbox module.

Execution pipeline:
 1. Partition MCP tools into code-mode and normal tools.
 2. Generate tool type definitions/signatures and the execute_go_code prompt.
 3. Generate main.go wiring for MCP sessions and tool function adapters.
 4. Compile and run model-generated run.go with timeout enforcement.
 5. Return combined output plus optional multimedia content from Run().

Safety and reliability guarantees:
  - tool-name collision checks prevent ambiguous Go identifiers;
  - execution timeouts are enforced (SIGINT then SIGKILL grace path);
  - recoverable failures (compile/runtime/panic/timeout) are returned as tool
    results so the model can iterate;
  - fatal harness failures are surfaced as hard errors to stop execution.
*/
package codemode
