package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"google.golang.org/genai"
)

func TestImportedCalls(t *testing.T) {
	for _, test := range []struct {
		name, signature string
		parallel        bool
	}{
		{name: "foreign call"}, {name: "authentic signature", signature: base64.StdEncoding.EncodeToString([]byte("authentic"))},
		{name: "parallel foreign", parallel: true}, {name: "parallel authentic", signature: base64.StdEncoding.EncodeToString([]byte("authentic")), parallel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			call, err := gai.ToolCallBlock("one", "starlark_repl", map[string]any{"code": "print(42)"})
			if err != nil {
				t.Fatal(err)
			}
			if test.signature != "" {
				call.ExtraFields = map[string]any{gai.GeminiExtraFieldThoughtSignature: test.signature}
			}
			blocks := []gai.Block{gai.TextBlock("working"), call}
			if test.parallel {
				second := call
				second.ID = "two"
				second.ExtraFields = nil
				blocks = append(blocks, second)
			}
			req := gai.GenerationRequest{Model: "gemini-3-pro", Dialog: gai.Dialog{{Role: gai.Assistant, Blocks: blocks}}}
			before, _ := json.Marshal(req)
			got := importedCalls(req)
			expected := test.signature
			if expected == "" {
				expected = base64.StdEncoding.EncodeToString([]byte("skip_thought_signature_validator"))
			}
			if got.Dialog[0].Blocks[1].ExtraFields[gai.GeminiExtraFieldThoughtSignature] != expected {
				t.Fatal("wrong signature")
			}
			if test.parallel && got.Dialog[0].Blocks[2].ExtraFields != nil {
				t.Fatal("rewrote unsigned sibling")
			}
			after, _ := json.Marshal(req)
			if string(before) != string(after) {
				t.Fatal("rewrote original request")
			}
		})
	}
}

func TestGeneratorRequests(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "Generate"
		if stream {
			name = "Stream"
		}
		t.Run(name, func(t *testing.T) {
			var captured string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				captured = string(data)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture captured request","status":"INVALID_ARGUMENT"}}`)
			}))
			defer server.Close()
			g, err := New(t.Context(), genai.ClientConfig{APIKey: "fixture", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			call, err := gai.ToolCallBlock("foreign", "starlark_repl", map[string]any{"code": "print(42)"})
			if err != nil {
				t.Fatal(err)
			}
			req := gai.GenerationRequest{Model: "gemini-3-pro", Dialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("compute")}},
				{Role: gai.Assistant, Blocks: []gai.Block{call}},
				gai.ToolResultMessage("foreign", gai.TextBlock("42")),
			}}
			if stream {
				for chunk := range g.(gai.StreamingGenerator).Stream(t.Context(), req) {
					if chunk.Err != nil {
						err = chunk.Err
					}
				}
			} else {
				_, err = g.Generate(t.Context(), req)
			}
			if err == nil || !strings.Contains(captured, base64.StdEncoding.EncodeToString([]byte("skip_thought_signature_validator"))) || !strings.Contains(captured, "starlark_repl") {
				t.Fatalf("request=%s err=%v", captured, err)
			}
			if req.Dialog[1].Blocks[0].ExtraFields != nil {
				t.Fatal("adapter changed source history")
			}
		})
	}
}

func TestGeneratorResponses(t *testing.T) {
	t.Run("concurrent stream signatures", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			signature := base64.StdEncoding.EncodeToString([]byte(r.URL.Path))
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"tool","args":{}},"thoughtSignature":%q}]},"finishReason":"STOP"}]}`+"\n\n", signature)
		}))
		t.Cleanup(server.Close)
		g, err := New(t.Context(), genai.ClientConfig{APIKey: "fixture", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: server.URL}})
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range []string{"one", "two"} {
			t.Run(model, func(t *testing.T) {
				t.Parallel()
				req := gai.GenerationRequest{Model: model, Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("start")}}}}
				for range 2 {
					resp, err := (&gai.StreamingAdapter{S: g.(gai.StreamingGenerator)}).Generate(t.Context(), req)
					if err != nil {
						t.Fatal(err)
					}
					want := base64.StdEncoding.EncodeToString([]byte("/v1beta/models/" + model + ":streamGenerateContent"))
					if len(resp.Candidates) != 1 || len(resp.Candidates[0].Blocks) != 1 || resp.Candidates[0].Blocks[0].ExtraFields[gai.GeminiExtraFieldThoughtSignature] != want {
						t.Fatalf("response=%+v", resp)
					}
				}
			})
		}
	})

	for _, mode := range []string{"Generate", "Stream", "Stream separate events"} {
		t.Run(mode, func(t *testing.T) {
			signature := base64.StdEncoding.EncodeToString([]byte("authentic function signature"))
			parts := []map[string]any{
				{"text": "working"},
				{"functionCall": map[string]any{"name": "first", "args": map[string]any{"x": 1}}, "thoughtSignature": signature},
				{"functionCall": map[string]any{"name": "second", "args": map[string]any{}}},
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				groups := [][]map[string]any{parts}
				if mode == "Stream separate events" {
					groups = nil
					for _, p := range parts {
						groups = append(groups, []map[string]any{p})
					}
				}
				for _, group := range groups {
					data, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": group}, "finishReason": "STOP"}}})
					if mode == "Generate" {
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(data)
					} else {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: %s\r\n\r\n", data)
					}
				}
			}))
			defer server.Close()
			g, err := New(t.Context(), genai.ClientConfig{APIKey: "fixture", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			var dialog gai.Dialog
			for _, id := range []string{"toolcall-4", "cpe-gemini-1", "cpe-gemini-3"} {
				call, err := gai.ToolCallBlock(id, "prior", map[string]any{})
				if err != nil {
					t.Fatal(err)
				}
				call.ExtraFields = map[string]any{gai.GeminiExtraFieldThoughtSignature: signature}
				dialog = append(dialog, gai.Message{Role: gai.Assistant, Blocks: []gai.Block{call}}, gai.ToolResultMessage(id, gai.TextBlock("prior result")))
			}
			req := gai.GenerationRequest{Model: "gemini-fixture", Dialog: dialog}
			before, _ := json.Marshal(req)
			if mode != "Generate" {
				g = &gai.StreamingAdapter{S: g.(gai.StreamingGenerator)}
			}
			resp, err := g.Generate(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Candidates) != 1 || len(resp.Candidates[0].Blocks) != 3 {
				t.Fatalf("response=%+v", resp)
			}
			first, second := resp.Candidates[0].Blocks[1], resp.Candidates[0].Blocks[2]
			used := map[string]bool{"toolcall-4": true, "cpe-gemini-1": true, "cpe-gemini-3": true, "": true}
			if used[first.ID] || used[second.ID] || first.ID == second.ID {
				t.Fatalf("colliding IDs: %s %s", first.ID, second.ID)
			}
			used[first.ID], used[second.ID] = true, true
			if first.ExtraFields[gai.GeminiExtraFieldThoughtSignature] != signature || second.ExtraFields[gai.GeminiExtraFieldThoughtSignature] != nil {
				t.Fatalf("lost/misplaced signatures: %v %v", first.ExtraFields, second.ExtraFields)
			}
			for i, block := range []gai.Block{first, second} {
				var call struct {
					Name       string
					Parameters map[string]any
				}
				if err := json.Unmarshal([]byte(block.Content.String()), &call); err != nil {
					t.Fatal(err)
				}
				name, params := "first", map[string]any{"x": float64(1)}
				if i == 1 {
					name, params = "second", map[string]any{}
				}
				if call.Name != name || !reflect.DeepEqual(call.Parameters, params) {
					t.Fatalf("call=%+v", call)
				}
			}
			after, _ := json.Marshal(req)
			if string(before) != string(after) {
				t.Fatal("history IDs/signatures mutated")
			}
			// Compaction may hide all prior calls without ending the current run.
			req.Dialog = gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("summary")}}}
			repeated, err := g.Generate(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			for _, block := range repeated.Candidates[0].Blocks {
				if block.BlockType == gai.ToolCall {
					if used[block.ID] {
						t.Fatalf("reused ID after compaction: %s", block.ID)
					}
					used[block.ID] = true
				}
			}
		})
	}
}
