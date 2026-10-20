package models

// ReasoningLevel names a supported effort; empty means provider default.
type ReasoningLevel string

// ID names a configured model, independently of its provider's identifier.
type ID string

// Selection selects a configured model and its reasoning effort.
type Selection struct {
	Model     ID
	Reasoning ReasoningLevel
}
