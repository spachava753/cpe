package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
)

// mockTreePrinter is a test implementation of TreePrinter
type mockTreePrinter struct {
	printed bool
}

func (m *mockTreePrinter) PrintMessageForest(w io.Writer, roots []storage.MessageIdNode) {
	m.printed = true
	for _, root := range roots {
		io.WriteString(w, root.ID+"\n")
	}
}

func TestConversationList(t *testing.T) {
	tests := []struct {
		name               string
		storage            *mockDialogStorageWithList
		wantErr            bool
		errMsg             string
		wantOutputContains []string
	}{
		{
			name: "list messages successfully",
			storage: &mockDialogStorageWithList{
				mockDialogStorage: mockDialogStorage{},
				messages: []storage.MessageIdNode{
					{ID: "msg-1"},
					{ID: "msg-2"},
				},
			},
			wantErr: false,
			wantOutputContains: []string{
				"msg-1",
				"msg-2",
			},
		},
		{
			name: "no messages found",
			storage: &mockDialogStorageWithList{
				mockDialogStorage: mockDialogStorage{},
				messages:          []storage.MessageIdNode{},
			},
			wantErr: false,
			wantOutputContains: []string{
				"No messages found",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ConversationListOptions{
				Storage:     tt.storage,
				Writer:      &buf,
				TreePrinter: &mockTreePrinter{},
			}

			err := ConversationList(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConversationList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ConversationList() error = %v, want error containing %q", err, tt.errMsg)
			}

			if !tt.wantErr {
				output := buf.String()
				for _, want := range tt.wantOutputContains {
					if !strings.Contains(output, want) {
						t.Errorf("ConversationList() output does not contain %q\nOutput: %s", want, output)
					}
				}
			}
		})
	}
}

// mockDialogStorageWithList extends mockDialogStorage with list support
type mockDialogStorageWithList struct {
	mockDialogStorage
	messages       []storage.MessageIdNode
	listErr        error
	hasChildren    bool
	hasChildrenErr error
	deleteErr      error
}

func (m *mockDialogStorageWithList) ListMessages(ctx context.Context) ([]storage.MessageIdNode, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.messages, nil
}

func (m *mockDialogStorageWithList) HasChildrenByID(ctx context.Context, messageID string) (bool, error) {
	if m.hasChildrenErr != nil {
		return false, m.hasChildrenErr
	}
	return m.hasChildren, nil
}

func (m *mockDialogStorageWithList) DeleteMessage(ctx context.Context, messageID string) error {
	return m.deleteErr
}

func (m *mockDialogStorageWithList) DeleteMessageRecursive(ctx context.Context, messageID string) error {
	return m.deleteErr
}

func TestConversationDelete(t *testing.T) {
	tests := []struct {
		name               string
		storage            *mockDialogStorageWithList
		messageIDs         []string
		cascade            bool
		wantErr            bool
		wantStdoutContains []string
		wantStderrContains []string
	}{
		{
			name: "delete single message without children",
			storage: &mockDialogStorageWithList{
				mockDialogStorage: mockDialogStorage{},
				hasChildren:       false,
			},
			messageIDs: []string{"msg-1"},
			cascade:    false,
			wantErr:    false,
			wantStdoutContains: []string{
				"Successfully deleted message msg-1",
			},
		},
		{
			name: "delete message with children requires cascade",
			storage: &mockDialogStorageWithList{
				mockDialogStorage: mockDialogStorage{},
				hasChildren:       true,
			},
			messageIDs: []string{"msg-1"},
			cascade:    false,
			wantErr:    false,
			wantStderrContains: []string{
				"message msg-1 has children",
			},
		},
		{
			name: "delete message with cascade",
			storage: &mockDialogStorageWithList{
				mockDialogStorage: mockDialogStorage{},
				hasChildren:       true,
			},
			messageIDs: []string{"msg-1"},
			cascade:    true,
			wantErr:    false,
			wantStdoutContains: []string{
				"Successfully deleted message msg-1 and all its descendants",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts := ConversationDeleteOptions{
				Storage:    tt.storage,
				MessageIDs: tt.messageIDs,
				Cascade:    tt.cascade,
				Stdout:     &stdout,
				Stderr:     &stderr,
			}

			err := ConversationDelete(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConversationDelete() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			stdoutStr := stdout.String()
			for _, want := range tt.wantStdoutContains {
				if !strings.Contains(stdoutStr, want) {
					t.Errorf("ConversationDelete() stdout does not contain %q\nStdout: %s", want, stdoutStr)
				}
			}

			stderrStr := stderr.String()
			for _, want := range tt.wantStderrContains {
				if !strings.Contains(stderrStr, want) {
					t.Errorf("ConversationDelete() stderr does not contain %q\nStderr: %s", want, stderrStr)
				}
			}
		})
	}
}

// mockDialogFormatter is a test implementation of DialogFormatter
type mockDialogFormatter struct {
	formatted string
	err       error
}

func (m *mockDialogFormatter) FormatDialog(dialog gai.Dialog, msgIds []string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.formatted, nil
}

func TestConversationPrint(t *testing.T) {
	tests := []struct {
		name               string
		storage            *mockDialogStorage
		formatter          DialogFormatter
		messageID          string
		wantErr            bool
		errMsg             string
		wantOutputContains []string
	}{
		{
			name: "print conversation successfully",
			storage: &mockDialogStorage{
				dialog: gai.Dialog{
					{
						Role: gai.User,
						Blocks: []gai.Block{
							{
								BlockType:    gai.Content,
								ModalityType: gai.Text,
								Content:      gai.Str("Hello"),
							},
						},
					},
				},
				msgIDList: []string{"msg-1"},
			},
			formatter: &mockDialogFormatter{
				formatted: "Formatted conversation",
			},
			messageID: "msg-1",
			wantErr:   false,
			wantOutputContains: []string{
				"Formatted conversation",
			},
		},
		{
			name: "formatter error",
			storage: &mockDialogStorage{
				dialog: gai.Dialog{},
			},
			formatter: &mockDialogFormatter{
				err: errors.New("format error"),
			},
			messageID: "msg-1",
			wantErr:   true,
			errMsg:    "failed to format dialog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ConversationPrintOptions{
				Storage:         tt.storage,
				MessageID:       tt.messageID,
				Writer:          &buf,
				DialogFormatter: tt.formatter,
			}

			err := ConversationPrint(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConversationPrint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ConversationPrint() error = %v, want error containing %q", err, tt.errMsg)
			}

			if !tt.wantErr {
				output := buf.String()
				for _, want := range tt.wantOutputContains {
					if !strings.Contains(output, want) {
						t.Errorf("ConversationPrint() output does not contain %q\nOutput: %s", want, output)
					}
				}
			}
		})
	}
}
