package agent

import (
	"bytes"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAnthropicExecutor_SaveMessages(t *testing.T) {
	// Create a minimal anthropicExecutor with a conversation that includes all possible content block types
	executor := &anthropicExecutor{
		params: &anthropic.BetaMessageNewParams{
			Messages: anthropic.F([]anthropic.BetaMessageParam{
				{
					Role: anthropic.F(anthropic.BetaMessageParamRoleUser),
					Content: anthropic.F([]anthropic.BetaContentBlockParamUnion{
						&anthropic.BetaTextBlockParam{
							Text: anthropic.F("test message"),
							Type: anthropic.F(anthropic.BetaTextBlockParamTypeText),
						},
					}),
				},
				{
					Role: anthropic.F(anthropic.BetaMessageParamRoleAssistant),
					Content: anthropic.F([]anthropic.BetaContentBlockParamUnion{
						&anthropic.BetaTextBlockParam{
							Text: anthropic.F("test response"),
							Type: anthropic.F(anthropic.BetaTextBlockParamTypeText),
						},
						&anthropic.BetaToolUseBlockParam{
							Name:  anthropic.F("test_tool"),
							Input: anthropic.F[any](map[string]interface{}{"key": "value"}),
							Type:  anthropic.F(anthropic.BetaToolUseBlockParamTypeToolUse),
						},
					}),
				},
				{
					Role: anthropic.F(anthropic.BetaMessageParamRoleUser),
					Content: anthropic.F([]anthropic.BetaContentBlockParamUnion{
						&anthropic.BetaToolResultBlockParam{
							Content: anthropic.F([]anthropic.BetaToolResultBlockParamContentUnion{
								anthropic.BetaToolResultBlockParamContent{
									Type: anthropic.F(anthropic.BetaToolResultBlockParamContentTypeText),
									Text: anthropic.F("tool result"),
								},
							}),
							Type:    anthropic.F(anthropic.BetaToolResultBlockParamTypeToolResult),
							IsError: anthropic.F(false),
						},
					}),
				},
			}),
		},
	}

	// Try to save messages
	var buf bytes.Buffer
	err := executor.SaveMessages(&buf)

	// Now it should succeed
	assert.NoError(t, err)

	// Verify we can load the messages back
	var loadedExecutor anthropicExecutor
	loadedExecutor.params = &anthropic.BetaMessageNewParams{} // Initialize params
	err = loadedExecutor.LoadMessages(&buf)
	assert.NoError(t, err)

	// Verify the loaded messages match the original
	assert.Equal(t, executor.params.Messages.Value, loadedExecutor.params.Messages.Value)
}
