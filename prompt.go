package main

import (
	_ "embed"
)

// InitialPrompt contains the embedded content of the initial_prompt.txt file.
//
//go:embed initial_prompt.txt
var InitialPrompt string

//go:embed code_analysis_modification_prompt.txt
var CodeAnalysisModificationPrompt string

//go:embed general_assistant.txt
var GeneralAssistantPrompt string
