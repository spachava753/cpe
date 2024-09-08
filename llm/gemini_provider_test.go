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

	err = provider.SetConversation(conversation)
	if err != nil {
		t.Fatalf("Failed to set conversation: %v", err)
	}

	// Generate a response
	config := ModelConfig{
		Model:       "gemini-1.5-flash",
		MaxTokens:   100,
		Temperature: 0.7,
	}

	genai.NewUserContent()
	response, err := provider.GenerateResponse(config)
	if err != nil {
		t.Fatalf("Failed to generate response: %v", err)
	}

	// Check if we got a non-empty response
	if response == "" {
		t.Error("Generated response is empty")
	}

	t.Logf("Generated response: %s", response)

	// Check if the response was added to the conversation
	updatedConversation := provider.GetConversation()
	if len(updatedConversation.Messages) != 2 {
		t.Errorf("Expected 2 messages in conversation, got %d", len(updatedConversation.Messages))
	}

	if updatedConversation.Messages[1].Role != "assistant" {
		t.Errorf("Expected last message role to be 'assistant', got '%s'", updatedConversation.Messages[1].Role)
	}

	if updatedConversation.Messages[1].Content != response {
		t.Errorf("Last message content doesn't match generated response")
	}
}
