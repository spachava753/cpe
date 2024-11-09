package llm

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGeminiProvider(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY environment variable is not set")
	}

	provider, _, err := GetProvider("gemini-1.5-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("Failed to create GeminiProvider: %v", err)
	}

	t.Run("Basic conversation", func(t *testing.T) {
		// Set up a test conversation
		conversation := Conversation{
			SystemPrompt: "You are a helpful assistant.",
			Messages: []Message{
				{Role: "user", Content: []ContentBlock{{Type: "text", Text: "What is the capital of France?"}}},
			},
		}

		// Generate a response
		config := GenConfig{
			Model:       "gemini-1.5-flash",
			MaxTokens:   100,
			Temperature: 0.7,
		}

		response, _, err := provider.GenerateResponse(config, conversation)
		if err != nil {
			t.Fatalf("Failed to generate response: %v", err)
		}

		// Check if we got a non-empty response
		if len(response.Content) == 0 {
			t.Error("Generated response is empty")
		}

		t.Logf("Generated response: %v", response)
	})

	t.Run("Tool calling", func(t *testing.T) {
		// Set up a test conversation with a tool
		conversation := Conversation{
			SystemPrompt: "You are a helpful assistant with access to tools.",
			Messages: []Message{
				{Role: "user", Content: []ContentBlock{{Type: "text", Text: "What's the weather like in Paris?"}}},
			},
			Tools: []Tool{
				{
					Name:        "get_weather",
					Description: "Get the current weather in a given location",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string","description":"The city and country"}},"required":["location"]}`),
				},
			},
		}

		// Generate a response
		config := GenConfig{
			Model:       "gemini-1.5-flash",
			MaxTokens:   100,
			Temperature: 0.7,
			ToolChoice:  "auto",
		}

		response, _, err := provider.GenerateResponse(config, conversation)
		if err != nil {
			t.Fatalf("Failed to generate response: %v", err)
		}

		// Check if we got a non-empty response
		if len(response.Content) == 0 {
			t.Error("Generated response is empty")
		}

		// Check if the response includes a tool call
		hasToolCall := false
		for _, block := range response.Content {
			if block.Type == "tool_use" {
				hasToolCall = true
				break
			}
		}

		if !hasToolCall {
			t.Error("Expected a tool call in the response, but found none")
		}

		t.Logf("Generated response: %v", response)
	})
}
