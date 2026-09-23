package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"
)

func TestResponsesUsageIncludesWritesAndFailures(t *testing.T) {
	const incompleteFixture = "incomplete"
	for _, mode := range []string{"generate", "stream", incompleteFixture} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				usage := `"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":40,"cache_write_tokens":30},"output_tokens_details":{"reasoning_tokens":10}}`
				if !request.Stream {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"id":"response-fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"}]}],`+usage+`}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Done\",\"output_index\":0,\"content_index\":0}\n\n")
				kind := "completed"
				if mode == incompleteFixture {
					kind = mode
				}
				fmt.Fprintf(w, "data: {\"type\":\"response.%s\",\"response\":{\"status\":%q,%s}}\n\n", kind, kind, usage)
			}))
			defer server.Close()
			client := openai.NewClient(option.WithAPIKey("fixture-key"), option.WithBaseURL(server.URL), option.WithMaxRetries(0))
			gen := New(&client.Responses)
			req := gai.GenerationRequest{Model: "usage-model", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}}
			var metadata gai.Metadata
			if mode == "generate" {
				response, err := gen.Generate(t.Context(), req)
				if err != nil {
					t.Fatal(err)
				}
				metadata = response.UsageMetadata
			} else {
				failed := false
				for chunk := range gen.Stream(t.Context(), req) {
					if chunk.Err != nil {
						failed = true
						continue
					}
					if chunk.Block.BlockType == gai.MetadataBlockType {
						var raw map[string]int
						if err := json.Unmarshal([]byte(chunk.Block.Content.String()), &raw); err != nil {
							t.Fatal(err)
						}
						metadata = gai.Metadata{}
						for k, v := range raw {
							metadata[k] = v
						}
					}
				}
				if failed != (mode == incompleteFixture) {
					t.Fatal("unexpected stream status")
				}
			}
			if calls != 1 || metadata[gai.UsageMetricInputTokens] != 100 || metadata[gai.UsageMetricGenerationTokens] != 20 || metadata[gai.UsageMetricCacheReadTokens] != 40 || metadata[gai.UsageMetricCacheWriteTokens] != 30 {
				t.Fatalf("calls=%d usage=%+v", calls, metadata)
			}
		})
	}
}
