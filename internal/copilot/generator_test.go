package copilot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"
)

func TestCopilotRequests(t *testing.T) {
	for _, test := range []struct{ protocol, path, body, stream string }{
		{chatProtocol, "/chat/completions", `{"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`, "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"},
		{responsesProtocol, "/responses", `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}]}`, "data: " + `{"type":"response.output_text.delta","delta":"Hello","output_index":0,"content_index":0}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed"}}` + "\n\n"},
		{messagesProtocol, "/v1/messages", `{"id":"fixture","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
			"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` +
				"\n\nevent: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` +
				"\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` +
				"\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` +
				"\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` +
				"\n\nevent: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			for _, mode := range []string{"generate", "stream"} {
				t.Run(mode, func(t *testing.T) {
					dir := t.TempDir()
					store := credentialStore{dir: dir}
					saved := fixtureCredential()
					if err := store.save(t.Context(), saved); err != nil {
						t.Fatal(err)
					}
					t.Setenv("OPENAI_API_KEY", "wrong-openai")
					t.Setenv("ANTHROPIC_API_KEY", "wrong-anthropic")
					requests, exchanges := 0, 0
					previous := http.DefaultTransport
					t.Cleanup(func() { http.DefaultTransport = previous })
					http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
						if r.URL.String() == tokenURL {
							exchanges++
							if r.Header.Get("Authorization") != "token fixture-github-secret" {
								t.Error("wrong exchange credential")
							}
							return jsonResponse(200, fmt.Sprintf(`{"token":"refreshed-secret","expires_at":%d,"endpoints":{"api":"https://api.business.githubcopilot.com"}}`, time.Now().Add(time.Hour).Unix())), nil
						}
						requests++
						host, key := apiURL, "fixture-copilot-secret"
						if exchanges > 0 {
							host, key = "https://api.business.githubcopilot.com", "refreshed-secret"
						}
						if r.URL.String() != host+test.path || r.Header.Get("Authorization") != "Bearer "+key || r.Header.Get("X-Api-Key") != "" {
							t.Error("inference credential or endpoint mismatch")
						}
						if r.Header.Get("X-Session-Id") != "durable-root" || r.Header.Get("X-Request-Id") == "" || r.Header.Get("X-Initiator") != "user" || r.Header.Get("Copilot-Vision-Request") != "true" || !strings.HasPrefix(r.Header.Get("User-Agent"), "cpe/") {
							t.Error("missing request identity or vision")
						}
						data, err := io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
						if strings.Contains(string(data), "secret") {
							t.Fatal("credential in model body")
						}
						if requests == 1 {
							return jsonResponse(401, `{"error":{"type":"authentication_error","message":"Invalid fixture-secret"}}`), nil
						}
						resp := jsonResponse(200, test.body)
						if mode == "stream" {
							resp.Header.Set("Content-Type", "text/event-stream")
							resp.Body = io.NopCloser(strings.NewReader(test.stream))
						}
						return resp, nil
					})
					gen, err := New(test.protocol, dir, "durable-root", true)
					if err != nil {
						t.Fatal(err)
					}
					invoke := gen
					if mode == "stream" {
						invoke = &gai.StreamingAdapter{S: gen.(gai.StreamingGenerator)}
					}
					req := gai.GenerationRequest{Model: "fixture", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test"), gai.ImageBlock([]byte{1, 2, 3}, "image/png")}}}, Options: gai.NewGenerationOptions(gai.WithMaxGenerationTokens(100))}
					ctx := WithUserInitiation(t.Context())
					_, err = invoke.Generate(ctx, req)
					var apiErr *gai.ApiErr
					if !errors.As(err, &apiErr) || apiErr.Provider != credentialName || apiErr.StatusCode != 401 || !strings.Contains(err.Error(), "/login copilot") || strings.Contains(err.Error(), "fixture-secret") || requests != 1 {
						t.Fatalf("authentication attribution/retry: %v", err)
					}
					// Preflight refresh uses the GitHub credential and the returned subscription host.
					saved.ExpiresAt = 1
					if err := store.save(t.Context(), saved); err != nil {
						t.Fatal(err)
					}
					response, err := invoke.Generate(ctx, req)
					if err != nil || len(response.Candidates) != 1 || len(response.Candidates[0].Blocks) != 1 || response.Candidates[0].Blocks[0].Content.String() != "Hello" || requests != 2 || exchanges != 1 {
						t.Fatalf("renewed request: %+v err=%v requests=%d exchanges=%d", response, err, requests, exchanges)
					}
				})
			}
		})
	}
}

func TestTransportRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name, url, method string
		rejected          bool
	}{
		{"chat", apiURL + "/chat/completions", http.MethodPost, false}, {"responses", apiURL + "/responses", http.MethodPost, false}, {"messages", apiURL + "/v1/messages", http.MethodPost, false}, {"different host", "https://example.com/responses", http.MethodPost, true}, {"plaintext", "http://api.githubcopilot.com/responses", http.MethodPost, true}, {"wrong method", apiURL + "/responses", http.MethodGet, true}, {"wrong path", apiURL + "/models", http.MethodPost, true}, {"query", apiURL + "/responses?secret=1", http.MethodPost, true}, {"escaped path", apiURL + "/%72esponses", http.MethodPost, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store := credentialStore{dir: dir}
			if err := store.save(t.Context(), fixtureCredential()); err != nil {
				t.Fatal(err)
			}
			calls := 0
			tr := transport{store: &store, sessionID: "session", base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("X-Initiator") != "agent" {
					t.Error("tool result marked as user initiation")
				}
				return jsonResponse(200, `{}`), nil
			})}
			ctx := requestContext(t.Context(), gai.GenerationRequest{Dialog: gai.Dialog{gai.ToolResultMessage("call", gai.TextBlock("42"))}})
			req, err := http.NewRequestWithContext(ctx, test.method, test.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "original")
			resp, err := tr.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}
			if (err != nil) != test.rejected {
				t.Fatalf("transport: %v", err)
			}
			if test.rejected && calls != 0 || !test.rejected && calls != 1 || req.Header.Get("Authorization") != "original" || req.URL.String() != test.url {
				t.Fatal("unexpected network call or caller mutation")
			}
		})
	}
}

func TestServiceError(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{"success", nil}, {"cancellation", context.Canceled}, {"permission", &gai.ApiErr{Provider: gai.ProviderOpenAI, Kind: gai.APIErrorKindPermission, StatusCode: 403, Message: "policy disabled"}}, {"quota", &gai.ApiErr{Provider: gai.ProviderResponses, Kind: gai.APIErrorKindRateLimit, StatusCode: 429, Message: "quota"}}} {
		t.Run(test.name, func(t *testing.T) {
			var before *gai.ApiErr
			if !errors.As(test.err, &before) {
				if serviceError(test.err) != test.err {
					t.Fatal("non-API error changed")
				}
				return
			}
			snapshot := *before
			err := serviceError(test.err)
			var got *gai.ApiErr
			if !errors.As(err, &got) || got.Provider != credentialName || got.Kind != snapshot.Kind || got.StatusCode != snapshot.StatusCode || got.Message != snapshot.Message || got.Retryable() != before.Retryable() || !errors.Is(err, test.err) || *before != snapshot {
				t.Fatalf("classification lost: %v", err)
			}
		})
	}
}
