package llm

import (
	"github.com/sashabaranov/go-openai"
	"os"
	"testing"
)

func TestOpenAIProvider(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY environment variable is not set")
	}

	provider := NewOpenAIProvider(apiKey)

	// Set up a test conversation
	conversation := Conversation{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: "What is the capital of France?"},
		},
	}

	// Generate a response
	config := GenConfig{
		Model:       openai.GPT4oMini20240718,
		MaxTokens:   100,
		Temperature: 0.7,
	}

	response, err := provider.GenerateResponse(config, conversation)
	if err != nil {
		t.Fatalf("Failed to generate response: %v", err)
	}

	// Check if we got a non-empty response
	if response == "" {
		t.Error("Generated response is empty")
	}

	t.Logf("Generated response: %s", response)
}
