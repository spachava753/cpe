package main

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
)

var model = flag.String("model", "", "Specify the model to use. Supported models: claude-3-opus, claude-3-5-sonnet, claude-3-5-haiku, gemini-1.5-flash, gemini-1.5-pro, gpt-4o, gpt-4o-mini")
var customUrl = flag.String("custom-url", "", "Specify a custom base URL for the model provider API")
var maxTokens = flag.Int("max-tokens", 0, "Maximum number of tokens to generate")
var temp = flag.Float64("temperature", 0, "Sampling temperature (0.0 - 1.0)")
var topP = flag.Float64("top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
var topK = flag.Int("top-k", 0, "Top-k sampling parameter")
var freqPen = flag.Float64("frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
var presencePen = flag.Float64("presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
var n = flag.Int("number-of-responses", 0, "Number of responses to generate")

func TestDetermineCodebaseAccess(t *testing.T) {
	if *model != "" && *model != defaultModel {
		_, ok := modelConfigs[*model]
		if !ok && *customUrl == "" {
			t.Fatalf("Error: Unknown model '%s' requires -custom-url flag\n", *model)
		}
	}

	provider, genConfig, err := GetProvider(*model, Flags{
		Model:             *model,
		CustomURL:         *customUrl,
		MaxTokens:         *maxTokens,
		Temperature:       *temp,
		TopP:              *topP,
		TopK:              *topK,
		FrequencyPenalty:  *freqPen,
		PresencePenalty:   *presencePen,
		NumberOfResponses: *n,
	})
	if err != nil {
		t.Fatalf("Error initializing provider: %v\n", err)
	}

	if closer, ok := provider.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	tests := []struct {
		name             string
		userInput        string
		expectedDecision bool
	}{
		{
			name:             "Simple code explanation",
			userInput:        "Explain how Go's defer statement works.",
			expectedDecision: false,
		},
		{
			name:             "Code modification request",
			userInput:        "Refactor the main function in `main.go` to use dependency injection.",
			expectedDecision: true,
		},
		{
			name:             "Tricky general question",
			userInput:        "How can I implement a function that reverses a string in Go? Implement that in utils.go",
			expectedDecision: true,
		},
		{
			name:             "Tricky project-specific question",
			userInput:        "What's the best way to optimize the performance of our string reversal function?",
			expectedDecision: true,
		},
		{
			name:             "Ambiguous request",
			userInput:        "How can we improve error handling in our application?",
			expectedDecision: true,
		},
		{
			name:             "Code review request",
			userInput:        "Can you review the implementation of the `ParseFlags` function?",
			expectedDecision: true,
		},
		{
			name:             "General Go best practices",
			userInput:        "What are some best practices for writing concurrent Go code?",
			expectedDecision: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			result, genErr := determineCodebaseAccess(provider, genConfig, tt.userInput)

			assert.NoError(t, genErr)
			assert.Equal(t, tt.expectedDecision, result)
		})
	}
}
