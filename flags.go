package main

import (
	"flag"
	"github.com/spachava753/cpe/llm"
)

type Flags struct {
	Model             string
	OpenAIURL         string
	MaxTokens         int
	Temperature       float64
	TopP              float64
	TopK              int
	FrequencyPenalty  float64
	PresencePenalty   float64
	NumberOfResponses int
	Debug             bool
}

func ParseFlags() Flags {
	f := Flags{}

	flag.StringVar(&f.Model, "model", "", "Specify the model to use. Supported models: claude-3-opus, claude-3-5-sonnet, claude-3-5-haiku, gemini-1.5-flash, gemini-1.5-pro, gpt-4o, gpt-4o-mini")
	flag.StringVar(&f.OpenAIURL, "openai-url", "", "Specify a custom base URL for the OpenAI API")
	flag.IntVar(&f.MaxTokens, "max-tokens", 0, "Maximum number of tokens to generate")
	flag.Float64Var(&f.Temperature, "temperature", 0, "Sampling temperature (0.0 - 1.0)")
	flag.Float64Var(&f.TopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	flag.IntVar(&f.TopK, "top-k", 0, "Top-k sampling parameter")
	flag.Float64Var(&f.FrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	flag.Float64Var(&f.PresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	flag.IntVar(&f.NumberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	flag.BoolVar(&f.Debug, "debug", false, "Print the generated system prompt")

	flag.Parse()

	return f
}

func (f Flags) ApplyToGenConfig(config llm.GenConfig) llm.GenConfig {
	if f.MaxTokens != 0 {
		config.MaxTokens = f.MaxTokens
	}
	if f.Temperature != 0 {
		config.Temperature = float32(f.Temperature)
	}
	if f.TopP != 0 {
		config.TopP = float32(f.TopP)
	}
	if f.TopK != 0 {
		config.TopK = f.TopK
	}
	if f.FrequencyPenalty != 0 {
		config.FrequencyPenalty = float32(f.FrequencyPenalty)
	}
	if f.PresencePenalty != 0 {
		config.PresencePenalty = float32(f.PresencePenalty)
	}
	if f.NumberOfResponses != 0 {
		config.NumberOfResponses = f.NumberOfResponses
	}
	return config
}
