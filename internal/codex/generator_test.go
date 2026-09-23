package codex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"
)

func TestCodexFailuresAreNotRetried(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{"unauthorized", "fixture-access", "/login in CPE", 401},
		{"truncated", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", "before a completion event", 200},
		{"done-incomplete", "data: {\"type\":\"response.done\",\"response\":{\"status\":\"incomplete\"}}\n\n", "", 200},
		{"done-failed", "data: {\"type\":\"response.done\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"failed\"}}}\n\n", "", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			writeAuthFixture(t, path, time.Now().Add(time.Hour))
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			gen := newGenerator(&credentialStore{path: path}, http.DefaultTransport, server.URL+"/responses")
			_, err := gen.Generate(t.Context(), gai.GenerationRequest{Model: "fixture", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}})
			if err == nil || calls != 1 || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "fixture-access") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestCodexCredentialsCannotFollowRedirectsOrArbitraryEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFixture(t, path, time.Now().Add(time.Hour))
	redirects := 0
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirects++
		w.WriteHeader(500)
	}))
	defer sink.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	s := &credentialStore{path: path}
	gen := newGenerator(s, http.DefaultTransport, server.URL+"/responses")
	_, err := gen.Generate(t.Context(), gai.GenerationRequest{Model: "fixture", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}})
	if err == nil || redirects != 0 {
		t.Fatalf("redirect followed: calls=%d err=%v", redirects, err)
	}
	rt := &transport{store: s, base: http.DefaultTransport, endpoint: server.URL + "/responses"}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, sink.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.RoundTrip(req); err == nil || redirects != 0 {
		t.Fatal("arbitrary endpoint accepted")
	}
}
