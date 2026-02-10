package commands

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

// mockDialogLoader is a test implementation of DialogLoader
type mockDialogLoader struct {
	mostRecentID  string
	mostRecentErr error
	dialog        gai.Dialog
	msgIDList     []string
	getDialogErr  error
}

func (m *mockDialogLoader) GetMostRecentAssistantMessageId(ctx context.Context) (string, error) {
	if m.mostRecentErr != nil {
		return "", m.mostRecentErr
	}
	return m.mostRecentID, nil
}

func (m *mockDialogLoader) GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error) {
	if m.getDialogErr != nil {
		return nil, nil, m.getDialogErr
	}
	return m.dialog, m.msgIDList, nil
}

// mockDialogStorage is a test implementation used by conversation_test.go
// It implements both DialogLoader methods and additional methods for conversation commands
type mockDialogStorage struct {
	mostRecentID  string
	mostRecentErr error
	dialog        gai.Dialog
	msgIDList     []string
	getDialogErr  error
}

func (m *mockDialogStorage) GetMostRecentAssistantMessageId(ctx context.Context) (string, error) {
	if m.mostRecentErr != nil {
		return "", m.mostRecentErr
	}
	return m.mostRecentID, nil
}

func (m *mockDialogStorage) GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error) {
	if m.getDialogErr != nil {
		return nil, nil, m.getDialogErr
	}
	return m.dialog, m.msgIDList, nil
}

// mockToolCapableGenerator is a test implementation of ToolCapableGenerator
type mockToolCapableGenerator struct {
	result gai.Dialog
	err    error
}

func (m *mockToolCapableGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	if m.err != nil {
		return m.result, m.err
	}
	// Return dialog with an assistant message appended
	result := append(dialog, gai.Message{
		Role: gai.Assistant,
		Blocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("Generated response"),
			},
		},
	})
	return result, nil
}

// generateTestResult captures the outputs from Generate for snapshot testing
type generateTestResult struct {
	Error  string
	Stderr string
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name    string
		opts    GenerateOptions
		wantErr bool
	}{
		{
			name: "successful generation with new conversation",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("test prompt"),
					},
				},
				NewConversation: true,
				DialogLoader:    &mockDialogLoader{},
				Generator:       &mockToolCapableGenerator{},
				Stderr:          &bytes.Buffer{},
			},
			wantErr: false,
		},
		{
			name: "empty input",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{},
				Stderr:     &bytes.Buffer{},
			},
			wantErr: true,
		},
		{
			name: "no model specified",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("test"),
					},
				},
				Stderr: &bytes.Buffer{},
			},
			wantErr: true,
		},
		{
			name: "model not found",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("test"),
					},
				},
				NewConversation: true,
				Generator: &mockToolCapableGenerator{
					err: errors.New("model xyz not found in configuration"),
				},
				Stderr: &bytes.Buffer{},
			},
			wantErr: false,
		},
		{
			name: "generation without storage - succeeds",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("test"),
					},
				},
				NewConversation: true,
				Generator:       &mockToolCapableGenerator{},
				Stderr:          &bytes.Buffer{},
			},
			wantErr: false,
		},
		{
			name: "generation error - non-interrupted",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("test"),
					},
				},
				NewConversation: true,
				Generator: &mockToolCapableGenerator{
					err: errors.New("generation failed"),
				},
				Stderr: &bytes.Buffer{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Generate(context.Background(), tt.opts)

			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Capture results for snapshot
			result := generateTestResult{}
			if err != nil {
				result.Error = err.Error()
			}
			if stderr, ok := tt.opts.Stderr.(*bytes.Buffer); ok {
				result.Stderr = stderr.String()
			}

			cupaloy.SnapshotT(t, result)
		})
	}
}
