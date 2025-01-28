package agent

import (
	"github.com/pkoukk/tiktoken-go"
	"github.com/spachava753/cpe/internal/tiktokenloader"
)

// countTokens returns the number of tokens in the given text using the specified model
func countTokens(text string, model string) (int, error) {
	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		// If model not found, use default loader
		loader := tiktokenloader.NewOfflineLoader()
		tkm, err = tiktoken.NewEncodingWithLoader("o200k_base", loader)
		if err != nil {
			return 0, err
		}
	}
	
	return len(tkm.Encode(text, nil, nil)), nil
}

// truncateResult truncates a tool result to fit within maxTokens while preserving important information
func truncateResult(result string, maxTokens int, model string) (string, error) {
	// First check if truncation is needed
	tokens, err := countTokens(result, model)
	if err != nil {
		return "", err
	}
	
	if tokens <= maxTokens {
		return result, nil
	}
	
	// If result needs truncation, try to preserve important parts:
	// 1. For file content, keep the first part showing file info
	// 2. For error messages, keep the error part
	// 3. For command output, keep the first and last parts
	
	// Start with half the max tokens to leave room for context
	halfMaxTokens := maxTokens / 2
	
	// Try to preserve beginning
	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		loader := tiktokenloader.NewOfflineLoader()
		tkm, err = tiktoken.NewEncodingWithLoader("o200k_base", loader)
		if err != nil {
			return "", err
		}
	}
	
	// Encode full text
	encoded := tkm.Encode(result, nil, nil)
	
	// Take first half of max tokens
	start := tkm.Decode(encoded[:halfMaxTokens])
	
	// Take last portion to fill remaining tokens
	remaining := maxTokens - halfMaxTokens
	end := tkm.Decode(encoded[len(encoded)-remaining:])
	
	return start + "\n...[truncated]...\n" + end, nil
}