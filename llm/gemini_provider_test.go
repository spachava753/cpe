package llm

import (
	"github.com/google/generative-ai-go/genai"
	"os"
	"testing"
)

func TestGeminiProvider(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Fatal("GEMINI_API_KEY environment variable is not set")
	}

	provider, err := NewGeminiProvider(apiKey)
	if err != nil {
		t.Fatalf("Failed to create GeminiProvider: %v", err)
	}
	defer provider.Close()

	// Set up a test conversation
	conversation := Conversation{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: "What is the capital of France?"},
		},
	}

	// Generate a response
	config := GenConfig{
		Model:       "gemini-1.5-flash",
		MaxTokens:   100,
		Temperature: 0.7,
	}

	genai.NewUserContent()
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
