package models

//go:generate go run golang.org/x/tools/cmd/stringer -type=API

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
