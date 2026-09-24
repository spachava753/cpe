package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/responses"
)

// Check the actual outbound provider payload, including the Responses adapter
// shared by Codex. A base64 text block alone would not make the image visible.
func TestResultMessage(t *testing.T) {
	const imageData = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII="
	for _, tc := range []struct {
		name, output, failure, wantText string
		images                          []repl.Image
	}{
		{name: "empty", wantText: "(no output)"},
		{name: "text", output: "hello", wantText: "hello"},
		{name: "image", images: []repl.Image{{Data: imageData, MIMEType: "image/png"}}, wantText: "(no output)"},
		{name: "mixed", output: "a pixel", images: []repl.Image{{Data: imageData, MIMEType: "image/png"}}, wantText: "a pixel"},
		{name: "error after image", output: "partial", failure: "failed", images: []repl.Image{{Data: imageData, MIMEType: "image/png"}}, wantText: "partial\nfailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := resultMessage(repl.Result{CallID: "call", Output: tc.output, Error: tc.failure, Images: tc.images})
			if message.Role != gai.ToolResult || message.ToolResultError != (tc.failure != "") || len(message.Blocks) != 1+len(tc.images) || message.Blocks[0].Content.String() != tc.wantText {
				t.Fatalf("message=%+v", message)
			}
			for _, block := range message.Blocks {
				if block.ID != "call" {
					t.Fatalf("missing call correlation: %+v", block)
				}
			}
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
					received := make(chan struct{}, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						defer func() { received <- struct{}{} }()
						var request struct {
							Input []struct {
								Type   string          `json:"type"`
								CallID string          `json:"call_id"`
								Output json.RawMessage `json:"output"`
							} `json:"input"`
							Stream bool `json:"stream"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Error(err)
							http.Error(w, "bad request", 400)
							return
						}
						if request.Stream != stream {
							t.Errorf("stream=%v", request.Stream)
						}
						if len(request.Input) != 2 || request.Input[1].Type != "function_call_output" || request.Input[1].CallID != "call" {
							t.Errorf("input=%+v", request.Input)
							http.Error(w, "bad input", 400)
							return
						}
						if len(tc.images) == 0 {
							var text string
							if err := json.Unmarshal(request.Input[1].Output, &text); err != nil || text != tc.wantText {
								t.Errorf("text=%q err=%v", text, err)
							}
						} else {
							var content []struct {
								Type     string `json:"type"`
								Text     string `json:"text"`
								ImageURL string `json:"image_url"`
							}
							if err := json.Unmarshal(request.Input[1].Output, &content); err != nil {
								t.Error(err)
							}
							if len(content) != 2 || content[0].Type != "input_text" || content[0].Text != tc.wantText || content[1].Type != "input_image" || content[1].ImageURL != "data:image/png;base64,"+imageData {
								t.Errorf("image serialized as non-visual content: %+v", content)
							}
						}
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"seen\",\"output_index\":0,\"content_index\":0}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"seen"}]}]}`)
						}
					}))
					defer server.Close()
					client := openai.NewClient(option.WithAPIKey("fixture"), option.WithBaseURL(server.URL), option.WithMaxRetries(0))
					gen := responses.New(&client.Responses)
					call, err := gai.ToolCallBlock("call", "starlark_repl", map[string]any{"code": "..."})
					if err != nil {
						t.Fatal(err)
					}
					request := gai.GenerationRequest{Model: "fixture", Dialog: gai.Dialog{{Role: gai.Assistant, Blocks: []gai.Block{call}}, message}}
					if stream {
						for chunk := range gen.Stream(t.Context(), request) {
							if chunk.Err != nil {
								t.Fatal(chunk.Err)
							}
						}
					} else {
						if _, err := gen.Generate(t.Context(), request); err != nil {
							t.Fatal(err)
						}
					}
					select {
					case <-received:
					default:
						t.Fatal("provider received no request")
					}
				})
			}
		})
	}
}
