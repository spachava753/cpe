package llm

import (
	"context"
	"encoding/json"
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
func (g *GeminiProvider) GenerateResponse(config GenConfig, conversation Conversation) (Message, error) {
	if conversation.Messages[len(conversation.Messages)-1].Content[0].Type != "text" {
		return Message{}, fmt.Errorf("last message in conversation must be text")
	}

	g.model = g.client.GenerativeModel(config.Model)

	g.model.SetTemperature(config.Temperature)
	g.model.SetTopK(int32(config.TopK))
	g.model.SetTopP(config.TopP)
	g.model.SetMaxOutputTokens(int32(config.MaxTokens))
	g.model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(conversation.SystemPrompt)},
	}

	// Set up tools for function calling
	tools := convertToGeminiTools(conversation.Tools)
	g.model.Tools = tools

	session := g.model.StartChat()

	session.History = convertToGeminiContent(conversation.Messages[:len(conversation.Messages)-1])

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var resp *genai.GenerateContentResponse
	var err error

	resp, err = session.SendMessage(ctx, genai.Text(conversation.Messages[len(conversation.Messages)-1].Content[0].Text))

	if err != nil {
		return Message{}, fmt.Errorf("error sending message to Gemini: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return Message{}, fmt.Errorf("no response generated")
	}

	var contentBlocks []ContentBlock

	for _, part := range resp.Candidates[0].Content.Parts {
		switch v := part.(type) {
		case genai.Text:
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: string(v)})
		case genai.FunctionCall:
			toolUse := &ToolUse{
				ID:   v.Name, // Using the function name as the ID
				Name: v.Name,
			}
			// Convert the args map to JSON
			inputJSON, err := json.Marshal(v.Args)
			if err != nil {
				return Message{}, fmt.Errorf("error marshaling function args: %w", err)
			}
			toolUse.Input = inputJSON
			contentBlocks = append(contentBlocks, ContentBlock{Type: "tool_use", ToolUse: toolUse})
		}
	}

	return Message{
		Role:    "assistant",
		Content: contentBlocks,
	}, nil
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
	var parts []genai.Part

	for _, content := range message.Content {
		switch content.Type {
		case "text":
			parts = append(parts, genai.Text(content.Text))
		case "tool_use":
			// For tool use, we don't add anything to parts as it's handled separately
		case "tool_result":
			parts = append(parts, genai.FunctionResponse{
				Name:     content.ToolResult.ToolUseID,
				Response: content.ToolResult.Content.(map[string]interface{}),
			})
		}
	}

	return &genai.Content{
		Parts: parts,
		Role:  message.Role,
	}
}

// convertToGeminiTools converts internal Tool type to Gemini's Tool
func convertToGeminiTools(tools []Tool) []*genai.Tool {
	geminiTools := make([]*genai.Tool, len(tools))
	for i, tool := range tools {
		geminiTools[i] = &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters: &genai.Schema{
						Type:       genai.TypeObject,
						Properties: make(map[string]*genai.Schema),
					},
				},
			},
		}

		// Parse the input schema JSON
		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			fmt.Printf("Error parsing input schema for tool %s: %v\n", tool.Name, err)
			continue
		}

		// Convert the schema to Gemini's format
		if properties, ok := schema["properties"].(map[string]interface{}); ok {
			for propName, propSchema := range properties {
				if propDetails, ok := propSchema.(map[string]interface{}); ok {
					geminiTools[i].FunctionDeclarations[0].Parameters.Properties[propName] = &genai.Schema{
						Type:        convertToGeminiType(propDetails["type"].(string)),
						Description: propDetails["description"].(string),
					}
				}
			}
		}

		// Add required properties
		if required, ok := schema["required"].([]interface{}); ok {
			for _, req := range required {
				geminiTools[i].FunctionDeclarations[0].Parameters.Required = append(
					geminiTools[i].FunctionDeclarations[0].Parameters.Required,
					req.(string),
				)
			}
		}
	}
	return geminiTools
}

// convertToGeminiType converts a string type to the corresponding genai.Type
func convertToGeminiType(typeStr string) genai.Type {
	switch typeStr {
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	case "object":
		return genai.TypeObject
	default:
		fmt.Printf("Warning: Unknown type %s, defaulting to TypeString\n", typeStr)
		return genai.TypeString
	}
}
