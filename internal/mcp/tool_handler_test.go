package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

// TestToolInput is a test struct used for testing the createToolHandler function
type TestToolInput struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func (t TestToolInput) Validate() error {
	return nil
}

// TestCreateToolHandler tests the generic createToolHandler function
func TestCreateToolHandler(t *testing.T) {
	// Create test function that will be wrapped by createToolHandler
	testFunction := func(ctx context.Context, input TestToolInput) (string, error) {
		return input.Name + "-" + string(rune(input.Value)), nil
	}

	// Create the handler function
	handler := createToolHandler(testFunction)

	// Prepare a test request
	request := mcp.CallToolRequest{
		Params: struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments,omitempty"`
			Meta      *struct {
				ProgressToken mcp.ProgressToken `json:"progressToken,omitempty"`
			} `json:"_meta,omitempty"`
		}{
			Arguments: map[string]interface{}{
				"name":  "test",
				"value": 65, // ASCII 'A'
			},
		},
	}

	// Call the handler
	result, err := handler(context.Background(), request)

	// Assertions
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)

	// Extract text content from the Content interface
	contentBytes, err := json.Marshal(result.Content[0])
	assert.NoError(t, err)

	var textContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	err = json.Unmarshal(contentBytes, &textContent)
	assert.NoError(t, err)
	assert.Equal(t, "text", textContent.Type)
	assert.Equal(t, "test-A", textContent.Text)
}

// TestCreateToolHandlerValidation tests validation in the generic handler
type TestValidatingInput struct {
	Name string `json:"name"`
}

func (t TestValidatingInput) Validate() error {
	if t.Name == "invalid" {
		return assert.AnError
	}
	return nil
}

func TestCreateToolHandlerValidation(t *testing.T) {
	// Create test function
	testFunction := func(ctx context.Context, input TestValidatingInput) (string, error) {
		return input.Name, nil
	}

	// Create the handler function
	handler := createToolHandler(testFunction)

	// Test with valid input
	validRequest := mcp.CallToolRequest{
		Params: struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments,omitempty"`
			Meta      *struct {
				ProgressToken mcp.ProgressToken `json:"progressToken,omitempty"`
			} `json:"_meta,omitempty"`
		}{
			Arguments: map[string]interface{}{
				"name": "valid",
			},
		},
	}
	validResult, err := handler(context.Background(), validRequest)
	assert.NoError(t, err)
	assert.NotNil(t, validResult)
	assert.False(t, validResult.IsError)

	// Extract text content from the Content interface
	contentBytes, err := json.Marshal(validResult.Content[0])
	assert.NoError(t, err)

	var validTextContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	err = json.Unmarshal(contentBytes, &validTextContent)
	assert.NoError(t, err)
	assert.Equal(t, "text", validTextContent.Type)
	assert.Equal(t, "valid", validTextContent.Text)

	// Test with invalid input that fails validation
	invalidRequest := mcp.CallToolRequest{
		Params: struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments,omitempty"`
			Meta      *struct {
				ProgressToken mcp.ProgressToken `json:"progressToken,omitempty"`
			} `json:"_meta,omitempty"`
		}{
			Arguments: map[string]interface{}{
				"name": "invalid",
			},
		},
	}
	invalidResult, err := handler(context.Background(), invalidRequest)
	assert.NoError(t, err) // Handler itself doesn't return an error
	assert.NotNil(t, invalidResult)
	assert.True(t, invalidResult.IsError) // But the result has error flag set

	// Extract text content from the Content interface for error message
	errorContentBytes, err := json.Marshal(invalidResult.Content[0])
	assert.NoError(t, err)

	var errorTextContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	err = json.Unmarshal(errorContentBytes, &errorTextContent)
	assert.NoError(t, err)
	assert.Equal(t, "text", errorTextContent.Type)
	assert.Contains(t, errorTextContent.Text, assert.AnError.Error())
}
