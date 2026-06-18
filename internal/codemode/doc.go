/*
Package codemode implements CPE's execute_go_code feature.

Code mode asks the model to generate a complete Go source file and executes it
in a temporary sandbox module. It does not create MCP server connections or
expose MCP tools as generated Go function bindings; MCP tools remain normal
conversational tools registered by the ACP session runtime.

Execution pipeline:
 1. Generate the execute_go_code prompt and sandbox main.go harness.
 2. Compile model-generated run.go into a temporary binary.
 3. Run the binary through ACP terminal methods when the client supports
    terminals, or directly in the CPE process when it does not.
 4. Return combined output plus optional multimedia content from Run().

Safety and reliability guarantees:
  - execution timeouts are enforced (SIGINT then SIGKILL grace path,
    targeting the local process group on Unix when CPE executes directly);
  - recoverable failures (compile/runtime/panic/timeout) are returned as tool
    results so the model can iterate;
  - fatal harness failures are surfaced as hard errors to stop execution.
*/
package codemode
