package opencodego

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name, url string
		wantErr   bool
	}{
		{"chat", apiURL + "/chat/completions", false},
		{"responses", apiURL + "/responses", false},
		{"messages", apiURL + "/messages", false},
		{"other host", "https://example.com/zen/go/v1/messages", true},
		{"other path", "https://opencode.ai/zen/v1/messages", true},
		{"plaintext", "http://opencode.ai/zen/go/v1/messages", true},
		{"query", apiURL + "/messages?key=test", true},
		{"escaped path", apiURL + "/%6dessages", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := saveKey(t.Context(), dir, "fixture-key"); err != nil {
				t.Fatal(err)
			}
			called := 0
			transport := &transport{dir: dir, sessionID: "conversation-1", base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				called++
				if r.Header.Get("X-Opencode-Session") != "conversation-1" || !strings.HasPrefix(r.Header.Get("User-Agent"), "cpe/") {
					t.Error("missing client identity")
				}
				if test.name == "messages" {
					if r.Header.Get("X-Api-Key") != "fixture-key" || r.Header.Get("Authorization") != "" {
						t.Error("wrong messages auth")
					}
				} else if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("X-Api-Key") != "" {
					t.Error("wrong bearer auth")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, test.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "placeholder")
			req.Header.Set("X-Api-Key", "placeholder")
			resp, err := transport.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}
			if (err != nil) != test.wantErr {
				t.Fatalf("request error: %v", err)
			}
			if test.wantErr && called != 0 {
				t.Fatal("disallowed request sent")
			}
			if !test.wantErr && called != 1 {
				t.Fatal("request missing")
			}
			if req.Header.Get("Authorization") != "placeholder" || req.Header.Get("X-Api-Key") != "placeholder" {
				t.Fatal("caller headers mutated")
			}
		})
	}
}

func TestClient(t *testing.T) {
	for _, test := range []struct {
		name, sessionID string
		invalid         bool
	}{
		{"session", "conversation-1", false}, {"missing", "", true}, {"invalid", "two\nheaders", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			client, err := Client(dir, test.sessionID)
			if (err != nil) != test.invalid {
				t.Fatalf("constructor error: %v", err)
			}
			if test.invalid {
				return
			}
			var auth []string
			client.Transport.(*transport).base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				auth = append(auth, r.Header.Get("Authorization"))
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://example.com"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, apiURL+"/responses", nil)
			if err != nil {
				t.Fatal(err)
			}
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
				t.Fatal("missing key allowed")
			}
			for _, key := range []string{"first-key", "second-key"} {
				if err := saveKey(t.Context(), dir, key); err != nil {
					t.Fatal(err)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 302 {
					t.Fatal("redirect followed")
				}
			}
			if len(auth) != 2 || auth[0] != "Bearer first-key" || auth[1] != "Bearer second-key" {
				t.Fatal("key not reloaded or redirect sent")
			}
		})
	}
}
