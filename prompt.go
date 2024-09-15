package main

import (
	_ "embed"
	"encoding/json"
)

// SimplePrompt embeds the contents of the file "simple_prompt.txt" as a string for use in the application.
//
//go:embed simple_prompt.txt
var SimplePrompt string

// AgentlessPrompt is a system prompt for a two-step approach, not yet in use. The first step is creating a high level map of the code_mapsitory for reduced token count and to fit large code bases in the context window.
// Then we will ask the AIto identify which files it thinks is relevant. With the relevant files, we will feed them and any dependencies (callsites? definitions?) back into the LLM to modify.
//
//go:embed agentless_prompt.txt
var AgentlessPrompt string

// InitialPrompt contains the embedded content of the initial_prompt.txt file.
//
//go:embed initial_prompt.txt
var InitialPrompt string

//go:embed decide_codebase_access.json
var InitialPromptToolCallDef json.RawMessage
