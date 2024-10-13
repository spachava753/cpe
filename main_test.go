package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetermineCodebaseAccess(t *testing.T) {
	ParseFlags()

	if flags.Model != "" && flags.Model != defaultModel {
		_, ok := modelConfigs[flags.Model]
		if !ok && flags.CustomURL == "" {
			t.Fatalf("Error: Unknown model '%s' requires -custom-url flag\n", flags.Model)
		}
	}

	provider, genConfig, err := GetProvider(flags.Model, Flags{
		Model:             flags.Model,
		CustomURL:         flags.CustomURL,
		MaxTokens:         flags.MaxTokens,
		Temperature:       flags.Temperature,
		TopP:              flags.TopP,
		TopK:              flags.TopK,
		FrequencyPenalty:  flags.FrequencyPenalty,
		PresencePenalty:   flags.PresencePenalty,
		NumberOfResponses: flags.NumberOfResponses,
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
