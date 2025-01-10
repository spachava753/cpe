package agent

import (
	"bytes"
	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestOpenAIExecutor_SaveMessages(t *testing.T) {
	// Create a minimal openaiExecutor with a conversation that includes all possible content block types
	executor := &openaiExecutor{
		params: &openai.ChatCompletionNewParams{
			Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("test message"),
				openai.ChatCompletionAssistantMessageParam{
					Role: openai.F(openai.ChatCompletionAssistantMessageParamRoleAssistant),
					Content: openai.F([]openai.ChatCompletionAssistantMessageParamContentUnion{
						openai.TextPart("assistant message"),
					}),
					ToolCalls: openai.F([]openai.ChatCompletionMessageToolCallParam{
						{
							ID:   openai.F("test_id"),
							Type: openai.F(openai.ChatCompletionMessageToolCallTypeFunction),
							Function: openai.F(openai.ChatCompletionMessageToolCallFunctionParam{
								Name:      openai.F("test_tool"),
								Arguments: openai.F(`{"key": "value"}`),
							}),
						},
					}),
				},
				openai.ToolMessage("test_id", "tool result"),
			}),
		},
	}

	// Try to save messages
	var buf bytes.Buffer
	err := executor.SaveMessages(&buf)

	// Now it should succeed
	assert.NoError(t, err)

	// Verify we can load the messages back
	var loadedExecutor openaiExecutor
	loadedExecutor.params = &openai.ChatCompletionNewParams{} // Initialize params
	err = loadedExecutor.LoadMessages(&buf)
	assert.NoError(t, err)

	// Verify the loaded messages match the original
	assert.Equal(t, executor.params.Messages.Value, loadedExecutor.params.Messages.Value)
}
