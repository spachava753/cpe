package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/config"
	geminiadapter "github.com/spachava753/cpe/internal/gemini"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

// switchSource supplies equivalent completed responses or protocol stream chunks.
func switchSource(stream bool, check func(gai.GenerationRequest) error, blocks []gai.Block, finish gai.FinishReason) gai.Generator {
	usage := gai.Metadata{gai.UsageMetricInputTokens: 10, gai.UsageMetricGenerationTokens: 2}
	if !stream {
		return agenttest.NewScriptedGenerator(agenttest.GenerateStep{Check: check, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: blocks}}, FinishReason: finish, UsageMetadata: usage}})
	}
	var chunks []gai.StreamChunk
	for _, b := range blocks {
		if b.BlockType == gai.ToolCall {
			var call struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			}
			_ = json.Unmarshal([]byte(b.Content.String()), &call)
			header := b
			header.Content = gai.Str(call.Name)
			chunks = append(chunks, gai.StreamChunk{Block: header}, gai.StreamChunk{Block: gai.Block{BlockType: gai.ToolCall, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str(call.Parameters)}})
		} else {
			chunks = append(chunks, gai.StreamChunk{Block: b})
		}
		chunks = append(chunks, gai.StreamChunk{Block: gai.SeparatorBlock()})
	}
	chunks = append(chunks, gai.StreamChunk{Block: gai.MetadataBlock(usage)})
	return agenttest.NewScriptedStreamingGenerator(agenttest.StreamStep{Check: check, Chunks: chunks})
}

func TestQueuedModelSwitch(t *testing.T) {
	for _, test := range []struct {
		name                                                                string
		firstStream, nextStream, final, cancel, small, latest, toolBoundary bool
	}{
		{name: "Generate to Generate"}, {name: "Stream to Generate", firstStream: true},
		{name: "Generate to Stream", nextStream: true}, {name: "Stream to Stream", firstStream: true, nextStream: true},
		{name: "final response", final: true}, {name: "canceled request", cancel: true},
		{name: "new context budget", small: true}, {name: "latest selection wins", latest: true},
		{name: "during host call", toolBoundary: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "switch.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			cfg := config.Config{System: "Switch fixture", Agent: config.Agent{ToolTimeout: "5s", OutputLimit: 32000, MaxRounds: 5}, Compaction: config.Compaction{Prompt: "Summarize"}}
			priceA, priceB := 1.0, 2.0
			modelA := config.Model{Provider: "responses", ID: "a", ReasoningEffort: "low", Cost: &config.Pricing{Rates: config.Rates{Input: &priceA, Output: &priceA, CacheRead: &priceA, CacheWrite: &priceA}}}
			modelB := config.Model{Provider: "anthropic", ID: "b", ReasoningEffort: "high", Cost: &config.Pricing{Rates: config.Rates{Input: &priceB, Output: &priceB, CacheRead: &priceB, CacheWrite: &priceB}}}
			if test.small {
				modelB.ContextWindow = 1
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			block := func() error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			hostCalls, nextCalls := 0, 0
			host := repl.Tool{Name: "once", Execute: func(context.Context, map[string]any) (any, error) {
				hostCalls++
				if test.toolBoundary {
					if err := block(); err != nil {
						return nil, err
					}
				}
				return 42, nil
			}}
			call, err := gai.ToolCallBlock("old-call", "starlark_repl", map[string]any{codeParameter: `load("tools.star", "once"); answer = once(); print(answer)`})
			if err != nil {
				t.Fatal(err)
			}
			thinking := gai.Block{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("old private thinking"), ExtraFields: map[string]any{gai.ResponsesExtraFieldEncryptedContent: "old encrypted"}}
			blocks, finish := []gai.Block{thinking, call}, gai.ToolUse
			if test.final {
				blocks, finish = []gai.Block{gai.TextBlock("old final")}, gai.EndTurn
			}
			old := switchSource(test.firstStream, func(req gai.GenerationRequest) error {
				if req.Model != "a" {
					return fmt.Errorf("old request model %s", req.Model)
				}
				if test.toolBoundary {
					return nil
				}
				return block()
			}, blocks, finish)
			next := switchSource(test.nextStream, func(req gai.GenerationRequest) error {
				nextCalls++
				if req.Model != "b" {
					return fmt.Errorf("new request model %s", req.Model)
				}
				raw, _ := json.Marshal(req.Options)
				if !strings.Contains(string(raw), "high") || strings.Contains(string(raw), "low") {
					return fmt.Errorf("stale reasoning %s", raw)
				}
				last := req.Dialog[len(req.Dialog)-1]
				if last.Role != gai.ToolResult || last.Blocks[0].ID != "old-call" || last.Blocks[0].Content.String() != "42\n" {
					return fmt.Errorf("lost tool result: %+v", last)
				}
				data, _ := json.Marshal(req)
				if strings.Contains(string(data), "old private thinking") || strings.Contains(string(data), "old encrypted") {
					return errors.New("foreign thinking forwarded")
				}
				return nil
			}, []gai.Block{gai.TextBlock("new final")}, gai.EndTurn)
			a, err := Open(t.Context(), Options{Config: cfg, Model: modelA, Generator: old, Store: store, CWD: dir, Tools: []repl.Tool{host}})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			done := make(chan error, 1)
			go func() { done <- a.Prompt(ctx, "start", nil) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("no generation")
			}
			if err := a.QueueModel("beta", modelB, next); err != nil {
				t.Fatal(err)
			}
			if test.latest {
				if err := a.QueueModel("unused", modelA, old); err != nil {
					t.Fatal(err)
				}
				if err := a.QueueModel("beta", modelB, next); err != nil {
					t.Fatal(err)
				}
			}
			if test.cancel {
				cancel()
			} else {
				close(release)
			}
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("switch did not finish")
			}
			if (err != nil) != (test.cancel || test.small) {
				t.Fatal(err)
			}
			if test.small && !strings.Contains(err.Error(), "context_window") {
				t.Fatal(err)
			}
			if a.Model() != modelB || a.SelectedModel().Name != "beta" {
				t.Fatal("queued choice lost")
			}
			wantCalls := 1
			if test.final || test.cancel {
				wantCalls = 0
			}
			wantNext := wantCalls
			if test.small {
				wantNext = 0
			}
			if hostCalls != wantCalls || nextCalls != wantNext {
				t.Fatalf("host=%d next=%d", hostCalls, nextCalls)
			}
			if !test.cancel {
				firstPrice := true
				for _, entry := range store.Entries() {
					if entry.Type != EventUsage {
						continue
					}
					var record usageRecord
					if err := json.Unmarshal(entry.Data, &record); err != nil {
						t.Fatal(err)
					}
					want := priceB
					if firstPrice {
						want = priceA
						firstPrice = false
					}
					if record.Pricing == nil || *record.Pricing.Input != want {
						t.Fatalf("wrong request pricing: %+v", record)
					}
				}
			}
			if err := a.repl.Close(); err != nil {
				t.Fatal(err)
			}
			a.repl = nil
			before := hostCalls
			if err := a.restore(t.Context()); err != nil {
				t.Fatal(err)
			}
			if hostCalls != before {
				t.Fatal("restoration repeated host call")
			}
		})
	}
}

func TestGeminiHandoff(t *testing.T) {
	for _, test := range []struct {
		name            string
		stream, compact bool
	}{
		{name: "Generate"}, {name: "Stream", stream: true},
		{name: "Generate across compaction", compact: true}, {name: "Stream across compaction", stream: true, compact: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			requests, hostCalls, summaries := 0, 0, 0
			signatures := []string{base64.StdEncoding.EncodeToString([]byte("round one")), base64.StdEncoding.EncodeToString([]byte("round two"))}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				first := 0
				if summaries > 0 {
					first = 1 // Compaction removes the first Gemini call from context.
				}
				for _, signature := range signatures[first:min(requests, 2)] {
					if !strings.Contains(string(data), signature) {
						t.Errorf("request %d lost signature %s: %s", requests+1, signature, data)
					}
				}
				part := map[string]any{"text": "done"}
				if strings.Contains(string(data), "summary fixture") {
					summaries++
					part = map[string]any{"text": "short summary"}
				} else {
					requests++
					if requests <= 2 {
						code := "answer += once(); print(answer)"
						if test.compact && requests == 1 {
							code += `; print("x" * 3000)`
						}
						part = map[string]any{"functionCall": map[string]any{"name": "starlark_repl", "args": map[string]any{"code": code}}, "thoughtSignature": signatures[requests-1]}
					}
				}
				data, err = json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{part}}, "finishReason": "STOP"}}})
				if err != nil {
					t.Error(err)
				}
				if test.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(data)
				}
			}))
			defer server.Close()
			next, err := geminiadapter.New(t.Context(), genai.ClientConfig{APIKey: "fixture", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			if !test.stream {
				next = struct{ gai.Generator }{next}
			}
			path := filepath.Join(dir, "history.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			host := repl.Tool{Name: "once", Execute: func(context.Context, map[string]any) (any, error) { hostCalls++; return 14, nil }}
			call, err := gai.ToolCallBlock("cpe-gemini-2", "starlark_repl", map[string]any{"code": `load("tools.star", "once"); answer=once(); print(answer)`})
			if err != nil {
				t.Fatal(err)
			}
			var a *Agent
			model := config.Model{Provider: "gemini", ID: "gemini-3-pro"}
			old := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Check: func(gai.GenerationRequest) error { return a.QueueModel("gemini", model, next) }, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}})
			opts := Options{Config: config.Config{System: "fixture", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 10000, MaxRounds: 5}}, Model: config.Model{Provider: "responses", ID: "old"}, Generator: old, Store: store, CWD: dir, Tools: []repl.Tool{host}}
			wantSummaries := 0
			if test.compact {
				opts.Config.Compaction = config.Compaction{Prompt: "summary fixture", MaxCharacters: 2000}
				wantSummaries = 1
			}
			a, err = Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err = a.Prompt(t.Context(), "start", nil); err != nil {
				t.Fatalf("handoff after %d Gemini requests: %v", requests, err)
			}
			if requests != 3 || hostCalls != 3 || summaries != wantSummaries {
				t.Fatalf("requests=%d effects=%d summaries=%d", requests, hostCalls, summaries)
			}
			ids := map[string]bool{}
			var recordedSignatures []string
			for _, entry := range store.Entries() {
				if entry.Type != "message" {
					continue
				}
				message, _, err := decodeMessage(entry.Data)
				if err != nil {
					t.Fatal(err)
				}
				for _, block := range message.Blocks {
					if block.BlockType != gai.ToolCall {
						continue
					}
					if block.ID == "" || ids[block.ID] {
						t.Fatalf("duplicate/empty call ID %q", block.ID)
					}
					ids[block.ID] = true
					if signature, ok := block.ExtraFields[gai.GeminiExtraFieldThoughtSignature].(string); ok {
						recordedSignatures = append(recordedSignatures, signature)
					}
				}
			}
			if len(ids) != 3 || !reflect.DeepEqual(recordedSignatures, signatures) {
				t.Fatalf("ids=%v signatures=%v", ids, recordedSignatures)
			}
			before, _ := json.Marshal(a.Messages())
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			opts.Store, opts.Model, opts.Generator = reopened, model, next
			restored, err := Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			after, _ := json.Marshal(restored.Messages())
			if string(before) != string(after) || hostCalls != 3 || requests != 3 {
				t.Fatal("reopen changed history or repeated effects")
			}
			result, err := restored.repl.Eval(t.Context(), "restored-check", "print(answer)")
			if err != nil || result.Output != "42\n" {
				t.Fatalf("restored state=%+v err=%v", result, err)
			}
		})
	}
}
