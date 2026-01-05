package commands

import (
	"encoding/json"
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
