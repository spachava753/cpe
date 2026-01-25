package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
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
		name    string
		storage *mockDialogStorageWithList
		wantErr bool
		errMsg  string
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
		},
		{
			name: "no messages found",
			storage: &mockDialogStorageWithList{
				mockDialogStorage: mockDialogStorage{},
				messages:          []storage.MessageIdNode{},
			},
			wantErr: false,
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

			if !tt.wantErr {
				cupaloy.SnapshotT(t, buf.String())
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
		name       string
		storage    *mockDialogStorageWithList
		messageIDs []string
		cascade    bool
		wantErr    bool
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

			cupaloy.SnapshotT(t, map[string]string{
				"stdout": stdout.String(),
				"stderr": stderr.String(),
			})
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
		name      string
		storage   *mockDialogStorage
		formatter DialogFormatter
		messageID string
		wantErr   bool
		errMsg    string
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

			if tt.wantErr && err != nil {
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ConversationPrint() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			cupaloy.SnapshotT(t, buf.String())
		})
	}
}

// plainRenderer is a simple renderer for testing that returns input as-is
type plainRenderer struct{}

func (p *plainRenderer) Render(in string) (string, error) {
	return in, nil
}

func TestIsCodeModeResult(t *testing.T) {
	tests := []struct {
		name     string
		dialog   gai.Dialog
		index    int
		expected bool
	}{
		{
			name:     "empty dialog",
			dialog:   gai.Dialog{},
			index:    0,
			expected: false,
		},
		{
			name:     "index 0 always returns false",
			dialog:   gai.Dialog{{Role: gai.ToolResult}},
			index:    0,
			expected: false,
		},
		{
			name: "single execute_go_code tool call with matching result",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							ID:           "tool-1",
							BlockType:    gai.ToolCall,
							ModalityType: gai.Text,
							Content:      gai.Str(`{"name":"execute_go_code","parameters":{"code":"package main"}}`),
						},
					},
				},
				{
					Role: gai.ToolResult,
					Blocks: []gai.Block{
						{ID: "tool-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("output")},
					},
				},
			},
			index:    2,
			expected: true,
		},
		{
			name: "non-execute_go_code tool call",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							ID:           "tool-1",
							BlockType:    gai.ToolCall,
							ModalityType: gai.Text,
							Content:      gai.Str(`{"name":"get_weather","parameters":{"city":"NYC"}}`),
						},
					},
				},
				{
					Role: gai.ToolResult,
					Blocks: []gai.Block{
						{ID: "tool-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("sunny")},
					},
				},
			},
			index:    2,
			expected: false,
		},
		{
			name: "multiple tool calls - matching ID is execute_go_code",
			dialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")}}},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{ID: "tool-1", BlockType: gai.ToolCall, ModalityType: gai.Text, Content: gai.Str(`{"name":"get_weather","parameters":{}}`)},
						{ID: "tool-2", BlockType: gai.ToolCall, ModalityType: gai.Text, Content: gai.Str(`{"name":"execute_go_code","parameters":{"code":"pkg"}}`)},
					},
				},
				{
					Role:   gai.ToolResult,
					Blocks: []gai.Block{{ID: "tool-2", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("output")}},
				},
			},
			index:    2,
			expected: true,
		},
		{
			name: "multiple tool calls - matching ID is NOT execute_go_code",
			dialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")}}},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{ID: "tool-1", BlockType: gai.ToolCall, ModalityType: gai.Text, Content: gai.Str(`{"name":"get_weather","parameters":{}}`)},
						{ID: "tool-2", BlockType: gai.ToolCall, ModalityType: gai.Text, Content: gai.Str(`{"name":"execute_go_code","parameters":{"code":"pkg"}}`)},
					},
				},
				{
					Role:   gai.ToolResult,
					Blocks: []gai.Block{{ID: "tool-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("sunny")}},
				},
			},
			index:    2,
			expected: false,
		},
		{
			name: "previous message is not assistant",
			dialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")}}},
				{
					Role:   gai.ToolResult,
					Blocks: []gai.Block{{ID: "tool-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("output")}},
				},
			},
			index:    1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCodeModeResult(tt.dialog, tt.index)
			if got != tt.expected {
				t.Errorf("isCodeModeResult() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFormatCodeModeResultMarkdown(t *testing.T) {
	result := formatCodeModeResultMarkdown("Hello, World!")
	cupaloy.SnapshotT(t, result)
}

func TestMarkdownDialogFormatterWithExecuteGoCode(t *testing.T) {
	renderer := &plainRenderer{}
	formatter := &MarkdownDialogFormatter{Renderer: renderer}

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Run some code")},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					ID:           "tool-1",
					BlockType:    gai.ToolCall,
					ModalityType: gai.Text,
					Content:      gai.Str(`{"name":"execute_go_code","parameters":{"code":"package main\\n\\nfunc Run() {}"}}`),
				},
			},
		},
		{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{ID: "tool-1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Code executed successfully")},
			},
		},
	}

	output, err := formatter.FormatDialog(dialog, []string{"msg-1", "msg-2", "msg-3"})
	if err != nil {
		t.Fatalf("FormatDialog() error = %v", err)
	}

	cupaloy.SnapshotT(t, output)
}
