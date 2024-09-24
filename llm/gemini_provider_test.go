package llm

import (
	"encoding/json"
	"github.com/google/generative-ai-go/genai"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestConvertToGeminiTools(t *testing.T) {
	tests := []struct {
		name     string
		input    []Tool
		expected []*genai.Tool
	}{
		{
			name: "Simple string input",
			input: []Tool{
				{
					Name:        "get_weather",
					Description: "Get the current weather for a location",
					InputSchema: json.RawMessage(`{
                                                "type": "object",
                                                "properties": {
                                                        "location": {
                                                                "type": "string",
                                                                "description": "The city and state, e.g. San Francisco, CA"
                                                        }
                                                },
                                                "required": ["location"]
                                        }`),
				},
			},
			expected: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "get_weather",
							Description: "Get the current weather for a location",
							Parameters: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"location": {
										Type:        genai.TypeString,
										Description: "The city and state, e.g. San Francisco, CA",
									},
								},
								Required: []string{"location"},
							},
						},
					},
				},
			},
		},
		{
			name: "Complex nested object",
			input: []Tool{
				{
					Name:        "create_user",
					Description: "Create a new user in the system",
					InputSchema: json.RawMessage(`{
                                                "type": "object",
                                                "properties": {
                                                        "name": {
                                                                "type": "string",
                                                                "description": "Full name of the user"
                                                        },
                                                        "age": {
                                                                "type": "integer",
                                                                "description": "Age of the user"
                                                        },
                                                        "address": {
                                                                "type": "object",
                                                                "properties": {
                                                                        "street": {"type": "string"},
                                                                        "city": {"type": "string"},
                                                                        "country": {"type": "string"}
                                                                },
                                                                "required": ["street", "city", "country"]
                                                        },
                                                        "tags": {
                                                                "type": "array",
                                                                "items": {"type": "string"},
                                                                "description": "List of tags associated with the user"
                                                        }
                                                },
                                                "required": ["name", "age"]
                                        }`),
				},
			},
			expected: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "create_user",
							Description: "Create a new user in the system",
							Parameters: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"name": {
										Type:        genai.TypeString,
										Description: "Full name of the user",
									},
									"age": {
										Type:        genai.TypeInteger,
										Description: "Age of the user",
									},
									"address": {
										Type: genai.TypeObject,
										Properties: map[string]*genai.Schema{
											"street":  {Type: genai.TypeString},
											"city":    {Type: genai.TypeString},
											"country": {Type: genai.TypeString},
										},
										Required: []string{"street", "city", "country"},
									},
									"tags": {
										Type:        genai.TypeArray,
										Description: "List of tags associated with the user",
										Items:       &genai.Schema{Type: genai.TypeString},
									},
								},
								Required: []string{"name", "age"},
							},
						},
					},
				},
			},
		},
		{
			name: "Multiple tools",
			input: []Tool{
				{
					Name:        "get_time",
					Description: "Get the current time for a timezone",
					InputSchema: json.RawMessage(`{
                                                "type": "object",
                                                "properties": {
                                                        "timezone": {
                                                                "type": "string",
                                                                "description": "The timezone (e.g., 'UTC', 'America/New_York')"
                                                        }
                                                },
                                                "required": ["timezone"]
                                        }`),
				},
				{
					Name:        "calculate",
					Description: "Perform a simple calculation",
					InputSchema: json.RawMessage(`{
                                                "type": "object",
                                                "properties": {
                                                        "operation": {
                                                                "type": "string",
                                                                "enum": ["add", "subtract", "multiply", "divide"],
                                                                "description": "The mathematical operation to perform"
                                                        },
                                                        "x": {
                                                                "type": "number",
                                                                "description": "The first operand"
                                                        },
                                                        "y": {
                                                                "type": "number",
                                                                "description": "The second operand"
                                                        }
                                                },
                                                "required": ["operation", "x", "y"]
                                        }`),
				},
			},
			expected: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "get_time",
							Description: "Get the current time for a timezone",
							Parameters: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"timezone": {
										Type:        genai.TypeString,
										Description: "The timezone (e.g., 'UTC', 'America/New_York')",
									},
								},
								Required: []string{"timezone"},
							},
						},
					},
				},
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "calculate",
							Description: "Perform a simple calculation",
							Parameters: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"operation": {
										Type:        genai.TypeString,
										Description: "The mathematical operation to perform",
										Enum:        []string{"add", "subtract", "multiply", "divide"},
									},
									"x": {
										Type:        genai.TypeNumber,
										Description: "The first operand",
									},
									"y": {
										Type:        genai.TypeNumber,
										Description: "The second operand",
									},
								},
								Required: []string{"operation", "x", "y"},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToGeminiTools(tt.input)
			assert.Equal(t, len(tt.expected), len(result), "The number of converted tools should match the expected output")

			for i, expectedTool := range tt.expected {
				assert.Equal(t, len(expectedTool.FunctionDeclarations), len(result[i].FunctionDeclarations), "The number of function declarations should match")

				for j, expectedFunc := range expectedTool.FunctionDeclarations {
					assert.Equal(t, expectedFunc.Name, result[i].FunctionDeclarations[j].Name, "Function names should match")
					assert.Equal(t, expectedFunc.Description, result[i].FunctionDeclarations[j].Description, "Function descriptions should match")

					// Compare Parameters
					assert.Equal(t, expectedFunc.Parameters.Type, result[i].FunctionDeclarations[j].Parameters.Type, "Parameter types should match")
					assert.Equal(t, expectedFunc.Parameters.Description, result[i].FunctionDeclarations[j].Parameters.Description, "Parameter descriptions should match")
					assert.Equal(t, expectedFunc.Parameters.Required, result[i].FunctionDeclarations[j].Parameters.Required, "Required parameters should match")

					// Compare Properties
					for propName, expectedProp := range expectedFunc.Parameters.Properties {
						resultProp, exists := result[i].FunctionDeclarations[j].Parameters.Properties[propName]
						assert.True(t, exists, "Property %s should exist in the result", propName)
						if exists {
							assert.Equal(t, *expectedProp, *resultProp, "Property %s should match", propName)
						}
					}

					// Compare Items if they exist
					if expectedFunc.Parameters.Items != nil {
						assert.NotNil(t, result[i].FunctionDeclarations[j].Parameters.Items, "Items should not be nil if expected")
						if result[i].FunctionDeclarations[j].Parameters.Items != nil {
							assert.Equal(t, *expectedFunc.Parameters.Items, *result[i].FunctionDeclarations[j].Parameters.Items, "Items should match")
						}
					} else {
						assert.Nil(t, result[i].FunctionDeclarations[j].Parameters.Items, "Items should be nil if not expected")
					}
				}
			}
		})
	}
}

func TestGeminiProvider(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY environment variable is not set")
	}

	provider, err := NewGeminiProvider(apiKey)
	if err != nil {
		t.Fatalf("Failed to create GeminiProvider: %v", err)
	}
	defer provider.Close()

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

		response, err := provider.GenerateResponse(config, conversation)
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

		response, err := provider.GenerateResponse(config, conversation)
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
