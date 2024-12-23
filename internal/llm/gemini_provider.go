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
func NewGeminiProvider(apiKey string, baseURL string) (*GeminiProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := []option.ClientOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithEndpoint(baseURL))
	}

	client, err := genai.NewClient(ctx, opts...)
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
func (g *GeminiProvider) GenerateResponse(config GenConfig, conversation Conversation) (Message, TokenUsage, error) {
	g.model = g.client.GenerativeModel(config.Model)

	g.model.SetTemperature(config.Temperature)
	g.model.SetMaxOutputTokens(int32(config.MaxTokens))

	if config.TopK != nil {
		g.model.SetTopK(int32(*config.TopK))
	}
	if config.TopP != nil {
		g.model.SetTopP(*config.TopP)
	}
	g.model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(conversation.SystemPrompt)},
	}

	// Set up tools for function calling
	tools := convertToGeminiTools(conversation.Tools)
	g.model.Tools = tools

	session := g.model.StartChat()

	session.History = convertToGeminiContent(conversation.Messages[:len(conversation.Messages)-1])

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var resp *genai.GenerateContentResponse
	var err error

	// Handle forced tool use
	if config.ToolChoice == "tool" && config.ForcedTool != "" {
		g.model.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode:                 genai.FunctionCallingAny,
				AllowedFunctionNames: []string{config.ForcedTool},
			},
		}
	}

	msg := convertMessageToGeminiContent(conversation.Messages[len(conversation.Messages)-1])
	if len(msg.Parts) != 1 {
		return Message{}, TokenUsage{}, fmt.Errorf("last message in conversation must have exactly one part")
	}
	resp, err = session.SendMessage(ctx, msg.Parts[0])
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error sending message to Gemini: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return Message{}, TokenUsage{}, fmt.Errorf("no response generated")
	}

	tokenUsage := TokenUsage{
		InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
		OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
	}

	var contentBlocks []ContentBlock

	for _, part := range resp.Candidates[0].Content.Parts {
		switch v := part.(type) {
		case genai.Text:
			if len(v) == 0 {
				continue
			}
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: string(v)})
		case genai.FunctionCall:
			toolUse := &ToolUse{
				ID:   v.Name, // Using the function name as the ID
				Name: v.Name,
			}
			// Convert the args map to JSON
			inputJSON, err := json.Marshal(v.Args)
			if err != nil {
				return Message{}, TokenUsage{}, fmt.Errorf("error marshaling function args: %w", err)
			}
			toolUse.Input = inputJSON
			contentBlocks = append(contentBlocks, ContentBlock{Type: "tool_use", ToolUse: toolUse})
		}
	}

	return Message{
		Role:    "assistant",
		Content: contentBlocks,
	}, tokenUsage, nil
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
			functionCall := genai.FunctionCall{
				Name: content.ToolUse.Name,
				Args: make(map[string]any),
			}
			// Unmarshal the JSON input into the Args map
			if err := json.Unmarshal(content.ToolUse.Input, &functionCall.Args); err != nil {
				// Handle error (log it or panic, depending on your error handling strategy)
				fmt.Printf("Error unmarshaling tool use input: %v\n", err)
			}
			parts = append(parts, functionCall)
		case "tool_result":
			var response map[string]any
			switch v := content.ToolResult.Content.(type) {
			case string:
				response = map[string]any{"result": v}
			case map[string]interface{}:
				response = v
			default:
				// Handle unexpected types
				panic("unexpected type")
			}
			if content.ToolResult.IsError {
				response["error"] = true
			}
			parts = append(parts, genai.FunctionResponse{
				Name:     content.ToolResult.ToolUseID,
				Response: response,
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
		declaration := &genai.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
		}

		// Only include Parameters if the schema has properties
		if properties, ok := tool.InputSchema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
			declaration.Parameters = parseSchemaObject(tool.InputSchema)
		}

		geminiTools[i] = &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{declaration},
		}
	}
	return geminiTools
}

// parseSchemaObject converts a tool schema to Gemini's Schema
func parseSchemaObject(schema map[string]interface{}) *genai.Schema {
	geminiSchema := &genai.Schema{
		Type: convertToGeminiType(schema["type"].(string)),
	}

	if description, ok := schema["description"].(string); ok {
		geminiSchema.Description = description
	}

	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for propName, propSchema := range properties {
			if geminiSchema.Properties == nil {
				geminiSchema.Properties = make(map[string]*genai.Schema)
			}
			geminiSchema.Properties[propName] = parseSchemaObject(propSchema.(map[string]interface{}))
		}
	}

	if items, ok := schema["items"].(map[string]interface{}); ok {
		geminiSchema.Items = parseSchemaObject(items)
	}

	if required, ok := schema["required"].([]interface{}); ok {
		for _, req := range required {
			geminiSchema.Required = append(geminiSchema.Required, req.(string))
		}
	}

	// Handle enum
	if enum, ok := schema["enum"].([]interface{}); ok {
		geminiSchema.Enum = make([]string, len(enum))
		for i, v := range enum {
			geminiSchema.Enum[i] = fmt.Sprintf("%v", v)
		}
	}

	return geminiSchema
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
		panic(fmt.Sprintf("Warning: Unknown type %s, defaulting to TypeString\n", typeStr))
	}
}
