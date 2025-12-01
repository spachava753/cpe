package codemode

import "fmt"

// RecoverableError represents an execution failure that the LLM can potentially
// adapt to. This includes compilation errors, Run() returning an error (exit 1),
// panics (exit 2), and timeout/kill scenarios.
type RecoverableError struct {
	Output   string
	ExitCode int
}

func (e RecoverableError) Error() string {
	return fmt.Sprintf("recoverable execution error (exit code %d): %s", e.ExitCode, e.Output)
}

// FatalExecutionError represents exit code 3 from the generated code, indicating
// an unrecoverable error in the generated MCP setup code (e.g., connection failures,
// unexpected content types). CPE should stop agent execution when this occurs.
type FatalExecutionError struct {
	Output string
}

func (e FatalExecutionError) Error() string {
	return fmt.Sprintf("fatal execution error: %s", e.Output)
}
