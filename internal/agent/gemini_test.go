package agent

import (
	"bytes"
	"context"
	"github.com/google/generative-ai-go/genai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"testing"
)

func TestGeminiExecutor_SaveMessages(t *testing.T) {
	// Create a minimal geminiExecutor with a conversation that includes all possible content block types
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey("test-key"))
	require.NoError(t, err)
	model := client.GenerativeModel("test-model")

	executor := &geminiExecutor{
		model: model,
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
	err = executor.SaveMessages(&buf)

	// Now it should succeed
	assert.NoError(t, err)

	// Create a new executor with model initialized
	loadedExecutor := &geminiExecutor{
		model: model,
	}
	err = loadedExecutor.LoadMessages(&buf)
	assert.NoError(t, err)

	// Verify the loaded messages match the original
	assert.Equal(t, executor.session.History, loadedExecutor.session.History)
}

func TestGeminiExecutor_LoadMessages_NilSession(t *testing.T) {
	// Create a minimal geminiExecutor with a conversation
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey("test-key"))
	require.NoError(t, err)
	model := client.GenerativeModel("test-model")

	executor := &geminiExecutor{
		model: model,
		session: &genai.ChatSession{
			History: []*genai.Content{
				{
					Role: "user",
					Parts: []genai.Part{
						genai.Text("test message"),
					},
				},
			},
		},
	}

	// Save messages
	var buf bytes.Buffer
	err = executor.SaveMessages(&buf)
	assert.NoError(t, err)

	// Try to load messages into a new executor without initializing session
	loadedExecutor := &geminiExecutor{
		model: model,
	}
	err = loadedExecutor.LoadMessages(&buf)
	assert.NoError(t, err)

	// Verify session was properly initialized
	assert.NotNil(t, loadedExecutor.session)
	assert.Equal(t, executor.session.History, loadedExecutor.session.History)
}
