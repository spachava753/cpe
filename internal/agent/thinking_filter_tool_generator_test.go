package agent

import (
	"context"
	"testing"

	"github.com/spachava753/gai"
	"github.com/stretchr/testify/assert"
)

// MockToolGenerator is a mock implementation of gai.ToolGenerator for testing
type MockToolGenerator struct {
	GenerateFn     func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
	RegisterFn     func(tool gai.Tool, callback gai.ToolCallback) error
	ReceivedDialog gai.Dialog
}

func (m *MockToolGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	m.ReceivedDialog = dialog
	return m.GenerateFn(ctx, dialog, optsGen)
}

func (m *MockToolGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	return m.RegisterFn(tool, callback)
}

func TestThinkingFilterToolGenerator(t *testing.T) {
	// Create a mock ToolGenerator
	mockToolGen := &MockToolGenerator{
		GenerateFn: func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
			// Return the dialog with an additional message
			return append(dialog, gai.Message{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("Test response"),
					},
				},
			}), nil
		},
		RegisterFn: func(tool gai.Tool, callback gai.ToolCallback) error {
			return nil
		},
	}

	// Create sample dialog with thinking blocks
	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("User message"),
				},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Thinking,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Thinking process..."),
				},
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Assistant response"),
				},
			},
		},
	}

	// Create expected filtered dialog
	expectedFilteredDialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("User message"),
				},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Assistant response"),
				},
			},
		},
	}

	// Create the filter generator with an interface wrapper for the mock
	filterToolGen := &ThinkingFilterToolGenerator{
		wrapped: mockToolGen,
	}

	// Call Generate
	ctx := context.Background()
	_, err := filterToolGen.Generate(ctx, dialog, nil)
	assert.NoError(t, err)

	// Verify the filtered dialog was passed to the mock
	assert.Equal(t, len(expectedFilteredDialog), len(mockToolGen.ReceivedDialog))

	// Check if the thinking blocks were removed
	for i, msg := range mockToolGen.ReceivedDialog {
		expectedMsg := expectedFilteredDialog[i]
		assert.Equal(t, expectedMsg.Role, msg.Role)
		assert.Equal(t, len(expectedMsg.Blocks), len(msg.Blocks))

		for j, block := range msg.Blocks {
			assert.NotEqual(t, gai.Thinking, block.BlockType)
			expectedBlock := expectedMsg.Blocks[j]
			assert.Equal(t, expectedBlock.BlockType, block.BlockType)
			assert.Equal(t, expectedBlock.ModalityType, block.ModalityType)
			assert.Equal(t, expectedBlock.Content.String(), block.Content.String())
		}
	}
}
