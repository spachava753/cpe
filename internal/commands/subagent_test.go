package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spachava753/gai"
)

func TestExtractFinalAnswerParams(t *testing.T) {
	tests := []struct {
		name       string
		dialog     gai.Dialog
		wantResult string
		wantErr    bool
	}{
		{
			name: "final_answer called with simple params",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Please analyze this"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", FinalAnswerToolName, map[string]any{
							"summary": "This is the analysis result",
							"score":   42,
						}),
					},
				},
			},
			wantResult: `{"score":42,"summary":"This is the analysis result"}`,
			wantErr:    false,
		},
		{
			name: "final_answer with nested object",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Get review"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", FinalAnswerToolName, map[string]any{
							"review": map[string]any{
								"rating":  5,
								"comment": "Great!",
							},
						}),
					},
				},
			},
			wantResult: `{"review":{"comment":"Great!","rating":5}}`,
			wantErr:    false,
		},
		{
			name: "final_answer after other tool calls",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Do something"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", "other_tool", map[string]any{"arg": "value"}),
					},
				},
				{
					Role: gai.ToolResult,
					Blocks: []gai.Block{
						{ID: "tc1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("result")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc2", FinalAnswerToolName, map[string]any{
							"done": true,
						}),
					},
				},
			},
			wantResult: `{"done":true}`,
			wantErr:    false,
		},
		{
			name: "no final_answer called",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Do something"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						gai.TextBlock("Here is my response"),
					},
				},
			},
			wantResult: "",
			wantErr:    true,
		},
		{
			name: "only other tool calls, no final_answer",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Do something"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", "other_tool", map[string]any{"arg": "value"}),
					},
				},
			},
			wantResult: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractFinalAnswerParams(tt.dialog)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractFinalAnswerParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// Parse both as JSON and compare (to ignore key ordering differences)
				var gotMap, wantMap map[string]any
				if err := json.Unmarshal([]byte(got), &gotMap); err != nil {
					t.Fatalf("failed to parse got result as JSON: %v", err)
				}
				if err := json.Unmarshal([]byte(tt.wantResult), &wantMap); err != nil {
					t.Fatalf("failed to parse want result as JSON: %v", err)
				}
				if diff := cmp.Diff(wantMap, gotMap); diff != "" {
					t.Errorf("extractFinalAnswerParams() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestExtractFinalResponse(t *testing.T) {
	tests := []struct {
		name       string
		dialog     gai.Dialog
		wantResult string
	}{
		{
			name: "simple text response",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Hello"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						gai.TextBlock("Hello! How can I help?"),
					},
				},
			},
			wantResult: "Hello! How can I help?",
		},
		{
			name: "multiple text blocks",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Hello"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						gai.TextBlock("First part"),
						gai.TextBlock("Second part"),
					},
				},
			},
			wantResult: "First part\nSecond part",
		},
		{
			name:       "empty dialog",
			dialog:     gai.Dialog{},
			wantResult: "",
		},
		{
			name: "no assistant message",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Hello"),
					},
				},
			},
			wantResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFinalResponse(tt.dialog)
			if got != tt.wantResult {
				t.Errorf("extractFinalResponse() = %q, want %q", got, tt.wantResult)
			}
		})
	}
}

// mustToolCallBlock creates a tool call block for testing, panics on error
func mustToolCallBlock(t *testing.T, id, toolName string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, toolName, params)
	if err != nil {
		t.Fatalf("failed to create tool call block: %v", err)
	}
	return block
}

// --- Storage Persistence Tests ---

// subagentMockStorage is a mock storage for subagent tests that tracks parent IDs and labels
type subagentMockStorage struct {
	savedEntries []subagentSavedEntry
	idCounter    int
}

type subagentSavedEntry struct {
	msg      gai.Message
	parentID string
	label    string
	id       string
}

func (m *subagentMockStorage) GetMostRecentAssistantMessageId(ctx context.Context) (string, error) {
	return "", nil
}

func (m *subagentMockStorage) GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error) {
	return nil, nil, nil
}

func (m *subagentMockStorage) SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
	id := fmt.Sprintf("msg_%d", m.idCounter)
	m.idCounter++
	m.savedEntries = append(m.savedEntries, subagentSavedEntry{
		msg:      message,
		parentID: parentID,
		label:    label,
		id:       id,
	})
	return id, nil
}

func (m *subagentMockStorage) Close() error {
	return nil
}

// subagentMockGenerator is a mock generator that returns configured responses
type subagentMockGenerator struct {
	responseBlocks []gai.Block
}

func (m *subagentMockGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	response := gai.Message{
		Role:   gai.Assistant,
		Blocks: m.responseBlocks,
	}
	return append(dialog, response), nil
}

func TestExecuteSubagent_PersistsToStorage(t *testing.T) {
	storage := &subagentMockStorage{}
	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("Hello from the subagent!"),
			},
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:    userBlocks,
		Generator:     generator,
		Storage:       storage,
		SubagentLabel: "subagent:test_agent:abc123",
	})

	if err != nil {
		t.Fatalf("ExecuteSubagent failed: %v", err)
	}

	if result != "Hello from the subagent!" {
		t.Errorf("unexpected result: %q", result)
	}

	// Verify messages were saved
	if len(storage.savedEntries) != 2 {
		t.Fatalf("expected 2 saved messages, got %d", len(storage.savedEntries))
	}

	// Verify user message
	userEntry := storage.savedEntries[0]
	if userEntry.msg.Role != gai.User {
		t.Errorf("expected user role, got %v", userEntry.msg.Role)
	}
	if userEntry.parentID != "" {
		t.Errorf("expected empty parent ID for user message, got %q", userEntry.parentID)
	}
	if userEntry.label != "subagent:test_agent:abc123" {
		t.Errorf("unexpected label: %q", userEntry.label)
	}

	// Verify assistant message
	assistantEntry := storage.savedEntries[1]
	if assistantEntry.msg.Role != gai.Assistant {
		t.Errorf("expected assistant role, got %v", assistantEntry.msg.Role)
	}
	if assistantEntry.parentID != "msg_0" {
		t.Errorf("expected parent ID 'msg_0', got %q", assistantEntry.parentID)
	}
	if assistantEntry.label != "subagent:test_agent:abc123" {
		t.Errorf("unexpected label: %q", assistantEntry.label)
	}
}

func TestExecuteSubagent_NoStorageNoError(t *testing.T) {
	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("Response without storage"),
			},
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	// Execute without storage - should still work
	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks: userBlocks,
		Generator:  generator,
		// No storage set
	})

	if err != nil {
		t.Fatalf("ExecuteSubagent failed: %v", err)
	}

	if result != "Response without storage" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestExecuteSubagent_MultipleAssistantMessages(t *testing.T) {
	storage := &subagentMockStorage{}

	// Simulate a generator that returns multiple assistant messages (e.g., tool call + response)
	generator := &subagentMultiMsgGenerator{
		responses: []gai.Message{
			{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.ToolCall,
						ModalityType: gai.Text,
						Content:      gai.Str(`{"name":"test_tool","parameters":{}}`),
					},
				},
			},
			{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("tool result"),
					},
				},
			},
			{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("Final response"),
					},
				},
			},
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:    userBlocks,
		Generator:     generator,
		Storage:       storage,
		SubagentLabel: "subagent:multi:xyz789",
	})

	if err != nil {
		t.Fatalf("ExecuteSubagent failed: %v", err)
	}

	if result != "Final response" {
		t.Errorf("unexpected result: %q", result)
	}

	// Verify all messages were saved (1 user + 3 assistant/tool)
	if len(storage.savedEntries) != 4 {
		t.Fatalf("expected 4 saved messages, got %d", len(storage.savedEntries))
	}

	// Verify chain of parent IDs
	for i, entry := range storage.savedEntries {
		if i == 0 {
			if entry.parentID != "" {
				t.Errorf("first message should have no parent, got %q", entry.parentID)
			}
		} else {
			expectedParent := fmt.Sprintf("msg_%d", i-1)
			if entry.parentID != expectedParent {
				t.Errorf("message %d: expected parent %q, got %q", i, expectedParent, entry.parentID)
			}
		}
		// All messages should have the same label
		if entry.label != "subagent:multi:xyz789" {
			t.Errorf("message %d: unexpected label %q", i, entry.label)
		}
	}
}

// subagentMultiMsgGenerator returns multiple messages in the dialog
type subagentMultiMsgGenerator struct {
	responses []gai.Message
}

func (m *subagentMultiMsgGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	return append(dialog, m.responses...), nil
}
