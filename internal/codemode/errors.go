package codemode

import "fmt"

// RecoverableError represents an execution failure that the LLM can potentially
// adapt to. This includes compilation errors, Run() returning an error (exit 1),
// panics (exit 2), and timeout/kill scenarios.
type RecoverableError struct {
	Err error
}

func (e RecoverableError) Error() string {
	return fmt.Sprintf("recoverable execution error: %s", e.Err)
}
