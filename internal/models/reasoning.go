package models

//go:generate go run golang.org/x/tools/cmd/stringer@latest -type=ReasoningField -linecomment

type ReasoningField int

const (
	ReasoningDetails ReasoningField = iota // reasoning_details
	ReasoningContent                       // reasoning_content
	Reasoning                              // reasoning
)
