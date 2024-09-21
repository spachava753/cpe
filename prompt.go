package main

import (
	_ "embed"
	"encoding/json"
)

// InitialPrompt contains the embedded content of the initial_prompt.txt file.
//
//go:embed initial_prompt.txt
var InitialPrompt string

//go:embed decide_codebase_access.json
var InitialPromptToolCallDef json.RawMessage

//go:embed code_map_analysis_prompt.txt
var CodeMapAnalysisPrompt string

//go:embed select_files_for_analysis.json
var SelectFilesForAnalysisToolDef json.RawMessage

//go:embed code_analysis_modification_prompt.txt
var CodeAnalysisModificationPrompt string
