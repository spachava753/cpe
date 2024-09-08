package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiProvider implements the LLMProvider interface using the Gemini SDK
type GeminiProvider struct {
	apiKey string
	client *genai.Client
	model  *genai.GenerativeModel
}

// NewGeminiProvider creates a new GeminiProvider with the given API key
func NewGeminiProvider(apiKey string) (*GeminiProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("error creating Gemini client: %w", err)
	}

	return &GeminiProvider{
		apiKey: apiKey,
		client: client,
		// model will be set in GenerateResponse
	}, nil
}

// GenerateResponse generates a response using the Gemini API
func (g *GeminiProvider) GenerateResponse(config ModelConfig, conversation Conversation) (string, error) {

	g.model = g.client.GenerativeModel(config.Model)

	g.model.SetTemperature(config.Temperature)
	g.model.SetTopK(int32(config.TopK))
	g.model.SetTopP(config.TopP)
	g.model.SetMaxOutputTokens(int32(config.MaxTokens))
	g.model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(conversation.SystemPrompt)},
	}

	session := g.model.StartChat()

	session.History = convertToGeminiContent(conversation.Messages[:len(conversation.Messages)-1])

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var resp *genai.GenerateContentResponse
	var err error

	resp, err = session.SendMessage(ctx, genai.Text(conversation.Messages[len(conversation.Messages)-1].Content))

	if err != nil {
		return "", fmt.Errorf("error sending message to Gemini: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response generated")
	}

	response := resp.Candidates[0].Content.Parts[0].(genai.Text)

	return string(response), nil
}

// Close closes the Gemini client and cleans up resources
func (g *GeminiProvider) Close() error {

	if g.client != nil {
		return g.client.Close()
	}
	return nil
}

// convertToGeminiContent converts a slice of Messages to a slice of genai.Content
func convertToGeminiContent(messages []Message) []*genai.Content {
	content := make([]*genai.Content, len(messages))
	for i, msg := range messages {
		content[i] = convertMessageToGeminiContent(msg)
	}
	return content
}

// convertMessageToGeminiContent converts a single Message to genai.Content
func convertMessageToGeminiContent(message Message) *genai.Content {
	return &genai.Content{
		Parts: []genai.Part{genai.Text(message.Content)},
		Role:  message.Role,
	}
}
