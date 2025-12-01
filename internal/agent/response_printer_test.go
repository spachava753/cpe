package agent

import (
	"testing"
)

// mockRenderer implements Renderer for testing
type mockRenderer struct {
	renderFunc func(string) (string, error)
}

func (m *mockRenderer) Render(in string) (string, error) {
	return m.renderFunc(in)
}

func TestRenderToolCall(t *testing.T) {
	// Mock renderers that pass through input with a prefix to identify which renderer was used
	contentRenderer := &mockRenderer{
		renderFunc: func(in string) (string, error) {
			return in, nil
		},
	}
	toolCallRenderer := &mockRenderer{
		renderFunc: func(in string) (string, error) {
			return in, nil
		},
	}

	g := &ResponsePrinterGenerator{
		contentRenderer:  contentRenderer,
		toolCallRenderer: toolCallRenderer,
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "execute_go_code renders as Go block",
			content: `{"name":"execute_go_code","parameters":{"code":"package main\n\nfunc Run() error { return nil }","executionTimeout":30}}`,
			want:    "#### [tool call]\n```go\npackage main\n\nfunc Run() error { return nil }\n```",
		},
		{
			name:    "regular tool renders as JSON block",
			content: `{"name":"get_weather","parameters":{"city":"New York"}}`,
			want: `#### [tool call]
` + "```json\n" + `{
  "name": "get_weather",
  "parameters": {
    "city": "New York"
  }
}
` + "```",
		},
		{
			name:    "execute_go_code with empty code falls back to JSON",
			content: `{"name":"execute_go_code","parameters":{"code":"","executionTimeout":30}}`,
			want: `#### [tool call]
` + "```json\n" + `{
  "name": "execute_go_code",
  "parameters": {
    "code": "",
    "executionTimeout": 30
  }
}
` + "```",
		},
		{
			name:    "execute_go_code with missing code falls back to JSON",
			content: `{"name":"execute_go_code","parameters":{"executionTimeout":30}}`,
			want: `#### [tool call]
` + "```json\n" + `{
  "name": "execute_go_code",
  "parameters": {
    "executionTimeout": 30
  }
}
` + "```",
		},
		{
			name:    "execute_go_code with non-string code falls back to JSON",
			content: `{"name":"execute_go_code","parameters":{"code":123,"executionTimeout":30}}`,
			want: `#### [tool call]
` + "```json\n" + `{
  "name": "execute_go_code",
  "parameters": {
    "code": 123,
    "executionTimeout": 30
  }
}
` + "```",
		},
		{
			name:    "malformed JSON falls back to plain text",
			content: `{invalid json`,
			want:    `{invalid json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.renderToolCall(tt.content)
			if got != tt.want {
				t.Errorf("renderToolCall() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestRenderToolCallUsesCorrectRenderer(t *testing.T) {
	tests := []struct {
		name               string
		content            string
		wantContentRenderer bool
	}{
		{
			name:               "execute_go_code uses contentRenderer",
			content:            `{"name":"execute_go_code","parameters":{"code":"package main","executionTimeout":30}}`,
			wantContentRenderer: true,
		},
		{
			name:               "regular tool uses toolCallRenderer",
			content:            `{"name":"get_weather","parameters":{"city":"NYC"}}`,
			wantContentRenderer: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var contentRendererCalled, toolCallRendererCalled bool

			g := &ResponsePrinterGenerator{
				contentRenderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						contentRendererCalled = true
						return in, nil
					},
				},
				toolCallRenderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						toolCallRendererCalled = true
						return in, nil
					},
				},
			}

			g.renderToolCall(tt.content)

			if tt.wantContentRenderer {
				if !contentRendererCalled {
					t.Error("expected contentRenderer to be called, but it was not")
				}
				if toolCallRendererCalled {
					t.Error("expected toolCallRenderer to not be called, but it was")
				}
			} else {
				if contentRendererCalled {
					t.Error("expected contentRenderer to not be called, but it was")
				}
				if !toolCallRendererCalled {
					t.Error("expected toolCallRenderer to be called, but it was not")
				}
			}
		})
	}
}
