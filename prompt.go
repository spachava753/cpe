package main

import _ "embed"

//go:embed simple_prompt.txt
var SimplePrompt string

// AgentlessPrompt is a system prompt for a two-step approach, not yet in use. The first step is creating a high level map of the code_mapsitory for reduced token count and to fit large code bases in the context window.
// Then we will ask the AIto identify which files it thinks is relevant. With the relevant files, we will feed them and any dependencies (callsites? definitions?) back into the LLM to modify.
//
//go:embed agentless_prompt.txt
var AgentlessPrompt string
