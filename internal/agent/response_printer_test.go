package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/gai"
)

// mockGenerator implements gai.ToolCapableGenerator
type mockGenerator struct {
	response gai.Response
	err      error
}

func (m *mockGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	return m.response, m.err
}

func (m *mockGenerator) Register(tool gai.Tool) error {
	return nil
}

// mockRenderer implements Renderer
type mockRenderer struct{}

func (m *mockRenderer) Render(in string) (string, error) {
	if in != "" {
		return fmt.Sprintln(in), nil
	}
	return in, nil
}

func TestResponsePrinterGenerator_Generate(t *testing.T) {
	tests := []struct {
		name           string
		codeMode       bool
		response       gai.Response
		expectedStdout string
		expectedStderr string
	}{
		{
			name: "Assistant Content",
			response: gai.Response{
				Candidates: []gai.Message{
					{
						Blocks: []gai.Block{
							{
								BlockType:    gai.Content,
								ModalityType: gai.Text,
								Content:      gai.Str("Hello, world!"),
							},
						},
					},
				},
			},
			expectedStdout: "Hello, world!\n",
			expectedStderr: "",
		},
		{
			name: "Thinking Content",
			response: gai.Response{
				Candidates: []gai.Message{
					{
						Blocks: []gai.Block{
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								Content:      gai.Str("Thinking about it..."),
							},
						},
					},
				},
			},
			expectedStdout: "",
			expectedStderr: "Thinking about it...\n",
		},
		{
			name: "Thinking and Content",
			response: gai.Response{
				Candidates: []gai.Message{
					{
						Blocks: []gai.Block{
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								Content:      gai.Str("Thinking..."),
							},
							{
								BlockType:    gai.Content,
								ModalityType: gai.Text,
								Content:      gai.Str("Answer."),
							},
						},
					},
				},
			},
			expectedStdout: "Answer.\n",
			expectedStderr: "Thinking...\n",
		},
		{
			name: "Tool Call",
			response: gai.Response{
				Candidates: []gai.Message{
					{
						Blocks: []gai.Block{
							{
								BlockType:    gai.ToolCall,
								ModalityType: gai.Text,
								Content: gai.Str(func() string {
									s, _ := json.Marshal(gai.ToolCallInput{
										Name: "some_tool",
										Parameters: map[string]any{
											"param1": "value",
										},
									})
									return string(s)
								}()),
							},
						},
					},
				},
			},
			expectedStdout: "",
			expectedStderr: fmt.Sprintf(`#### [tool call: some_tool]
%sjson
{
  "param1": "value"
}
%s
`, "```", "```"),
		},
		{
			name: "Thinking and Tool Call",
			response: gai.Response{
				Candidates: []gai.Message{
					{
						Blocks: []gai.Block{
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								Content:      gai.Str("Reasoning..."),
							},
							{
								BlockType:    gai.ToolCall,
								ModalityType: gai.Text,
								Content: gai.Str(func() string {
									s, _ := json.Marshal(gai.ToolCallInput{
										Name: "some_tool",
										Parameters: map[string]any{
											"param1": "value",
										},
									})
									return string(s)
								}()),
							},
						},
					},
				},
			},
			expectedStdout: "",
			expectedStderr: fmt.Sprintf(`Reasoning...
#### [tool call: some_tool]
%sjson
{
  "param1": "value"
}
%s
`, "```", "```"),
		},
		{
			name:     "Code Mode Tool Call",
			codeMode: true,
			response: gai.Response{
				Candidates: []gai.Message{
					{
						Blocks: []gai.Block{
							{
								BlockType:    gai.ToolCall,
								ModalityType: gai.Text,
								Content: gai.Str(func() string {
									s, _ := json.Marshal(gai.ToolCallInput{
										Name: executeTypescriptToolName,
										Parameters: map[string]any{
											"code": `console.log("1 + 1");`,
										},
									})
									return string(s)
								}()),
							},
						},
					},
				},
			},
			expectedStdout: "",
			expectedStderr: fmt.Sprintf(`#### [tool call: execute_typescript]
%stypescript
console.log("1 + 1");
%s
`, "```", "```"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGen := &mockGenerator{response: tt.response}
			mockRen := &mockRenderer{}

			var stdout, stderr bytes.Buffer

			printer := &ResponsePrinterGenerator{
				wrapped:          mockGen,
				contentRenderer:  mockRen,
				thinkingRenderer: mockRen,
				toolCallRenderer: mockRen,
				codeMode:         tt.codeMode,
				stdout:           &stdout,
				stderr:           &stderr,
			}

			_, err := printer.Generate(context.Background(), nil, nil)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if gotStdout := stdout.String(); gotStdout != tt.expectedStdout {
				t.Errorf("Expected stdout:\n%q\nGot:\n%q", tt.expectedStdout, gotStdout)
			}
			if gotStderr := stderr.String(); gotStderr != tt.expectedStderr {
				t.Errorf("Expected stderr:\n%q\nGot:\n%q", tt.expectedStderr, gotStderr)
			}
		})
	}
}
