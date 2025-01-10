package agent

import (
	"bytes"
	"github.com/google/generative-ai-go/genai"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGeminiExecutor_SaveMessages(t *testing.T) {
	// Create a minimal geminiExecutor with a conversation that includes all possible content block types
	executor := &geminiExecutor{
		session: &genai.ChatSession{
			History: []*genai.Content{
				{
					Role: "user",
					Parts: []genai.Part{
						genai.Text("test message"),
					},
				},
				{
					Role: "model",
					Parts: []genai.Part{
						genai.Text("test response"),
						genai.FunctionCall{
							Name: "test_tool",
							Args: map[string]interface{}{
								"key": "value",
							},
						},
					},
				},
				{
					Role: "function",
					Parts: []genai.Part{
						genai.FunctionResponse{
							Name: "test_tool",
							Response: map[string]interface{}{
								"result": "tool result",
							},
						},
					},
				},
			},
		},
	}

	// Try to save messages
	var buf bytes.Buffer
	err := executor.SaveMessages(&buf)

	// Now it should succeed
	assert.NoError(t, err)

	// Verify we can load the messages back
	var loadedExecutor geminiExecutor
	err = loadedExecutor.LoadMessages(&buf)
	assert.NoError(t, err)

	// Verify the loaded messages match the original
	assert.Equal(t, executor.session.History, loadedExecutor.session.History)
}
