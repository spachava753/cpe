package models

//go:generate go run golang.org/x/tools/cmd/stringer@latest -type=API

type API int

const (
	Responses API = iota
	Anthropic
	Gemini
	Openai
	Zai
	Moonshot
	Deepseek
	Cerebras
	OpenRouter
)
