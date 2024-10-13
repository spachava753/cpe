package main

import (
	"flag"
	"github.com/spachava753/cpe/llm"
)

type Flags struct {
	Model             string
	CustomURL         string
	MaxTokens         int
	Temperature       float64
	TopP              float64
	TopK              int
	FrequencyPenalty  float64
	PresencePenalty   float64
	NumberOfResponses int
	Debug             bool
	Input             string
	Version           bool
	IncludeFiles      string
}

var flags Flags

func init() {
	flag.BoolVar(&flags.Version, "version", false, "Print the version number and exit")
	flag.StringVar(&flags.Model, "model", "", "Specify the model to use. Supported models: claude-3-opus, claude-3-5-sonnet, claude-3-5-haiku, gemini-1.5-flash, gemini-1.5-pro, gpt-4o, gpt-4o-mini")
	flag.StringVar(&flags.CustomURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	flag.IntVar(&flags.MaxTokens, "max-tokens", 0, "Maximum number of tokens to generate")
	flag.Float64Var(&flags.Temperature, "temperature", 0, "Sampling temperature (0.0 - 1.0)")
	flag.Float64Var(&flags.TopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	flag.IntVar(&flags.TopK, "top-k", 0, "Top-k sampling parameter")
	flag.Float64Var(&flags.FrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	flag.Float64Var(&flags.PresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	flag.IntVar(&flags.NumberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	flag.BoolVar(&flags.Debug, "debug", false, "Print the generated system prompt")
	flag.StringVar(&flags.Input, "input", "-", "Specify the input file path. Use '-' for stdin")
	flag.StringVar(&flags.IncludeFiles, "include-files", "", "Comma-separated list of file paths to include in the system message")
}

func ParseFlags() {
	flag.Parse()
}

func (f Flags) ApplyToGenConfig(config llm.GenConfig) llm.GenConfig {
	if f.MaxTokens != 0 {
		config.MaxTokens = f.MaxTokens
	}
	if f.Temperature != 0 {
		config.Temperature = float32(f.Temperature)
	}
	if f.TopP != 0 {
		topP := float32(f.TopP)
		config.TopP = &topP
	}
	if f.TopK != 0 {
		topK := f.TopK
		config.TopK = &topK
	}
	if f.FrequencyPenalty != 0 {
		freqPenalty := float32(f.FrequencyPenalty)
		config.FrequencyPenalty = &freqPenalty
	}
	if f.PresencePenalty != 0 {
		presPenalty := float32(f.PresencePenalty)
		config.PresencePenalty = &presPenalty
	}
	if f.NumberOfResponses != 0 {
		numResponses := f.NumberOfResponses
		config.NumberOfResponses = &numResponses
	}
	return config
}
