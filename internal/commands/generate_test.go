package commands

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spachava753/gai"
)

// mockDialogStorage is a test implementation of DialogStorage
type mockDialogStorage struct {
	mostRecentID   string
	mostRecentErr  error
	dialog         gai.Dialog
	msgIDList      []string
	getDialogErr   error
	savedMessages  []gai.Message
	saveMessageErr error
	saveMessageID  string
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

func (m *mockDialogStorage) SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
	if m.saveMessageErr != nil {
		return "", m.saveMessageErr
	}
	m.savedMessages = append(m.savedMessages, message)
	if m.saveMessageID == "" {
		return "msg-" + string(rune(len(m.savedMessages))), nil
	}
	return m.saveMessageID, nil
}

func (m *mockDialogStorage) Close() error {
	return nil
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

func TestGenerate(t *testing.T) {
	tests := []struct {
		name               string
		opts               GenerateOptions
		wantErr            bool
		errMsg             string
		wantStderrContains []string
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
				Storage: &mockDialogStorage{
					saveMessageID: "msg-123",
				},
				Generator: &mockToolCapableGenerator{},
				Stderr:    &bytes.Buffer{},
			},
			wantErr: false,
			wantStderrContains: []string{
				"last_message_id is msg-123",
			},
		},
		{
			name: "empty input",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{},
				Stderr:     &bytes.Buffer{},
			},
			wantErr: true,
			errMsg:  "empty input",
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
			errMsg:  "no model specified",
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
				Stderr: &bytes.Buffer{},
			},
			wantErr: true,
			errMsg:  "not found in configuration",
		},
		{
			name: "incognito mode - no saving",
			opts: GenerateOptions{
				UserBlocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("test"),
					},
				},
				NewConversation: true,
				IncognitoMode:   true,
				Storage: &mockDialogStorage{
					saveMessageID: "should-not-save",
				},
				Generator: &mockToolCapableGenerator{},
				Stderr:    &bytes.Buffer{},
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
				IncognitoMode:   true,
				Generator: &mockToolCapableGenerator{
					err: errors.New("generation failed"),
				},
				Stderr: &bytes.Buffer{},
			},
			wantErr: false,
			wantStderrContains: []string{
				"Error generating response",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Generate(context.Background(), tt.opts)

			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Generate() error = %v, want error containing %q", err, tt.errMsg)
			}

			if !tt.wantErr {
				if stderr, ok := tt.opts.Stderr.(*bytes.Buffer); ok {
					stderrOutput := stderr.String()
					for _, want := range tt.wantStderrContains {
						if !strings.Contains(stderrOutput, want) {
							t.Errorf("Generate() stderr does not contain %q\nStderr: %s", want, stderrOutput)
						}
					}
				}
			}
		})
	}
}
