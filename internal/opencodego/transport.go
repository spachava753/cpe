package opencodego

import (
	"errors"
	"net/http"
	"time"

	"github.com/spachava753/cpe/internal/version"
)

// Client reloads CPE's saved API key for each request. Credentials are sent only
// to the fixed Go inference endpoints, with CPE's user agent and a stable session
// identifier shared by interactive turns and compaction. Redirects are disabled.
func Client(dir, sessionID string) (*http.Client, error) {
	if !validKey(sessionID) {
		return nil, errors.New("OpenCode Go requires a stable conversation ID")
	}
	return &http.Client{
		Timeout:       10 * time.Minute,
		Transport:     &transport{dir: dir, sessionID: sessionID, base: http.DefaultTransport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// BaseURL returns the fixed SDK base for the selected protocol. Anthropic's SDK
// appends v1/messages itself; the other adapters append paths relative to v1.
func BaseURL(provider string) (string, error) {
	switch provider {
	case "openai", "responses":
		return apiURL, nil
	case anthropicProtocol:
		return "https://opencode.ai/zen/go", nil
	default:
		return "", errors.New("OpenCode Go requires an openai, responses, or anthropic profile")
	}
}

type transport struct {
	dir, sessionID string
	base           http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.Host != "opencode.ai" || req.URL.User != nil || req.URL.RawQuery != "" {
		return nil, errors.New("refusing to send OpenCode Go credentials outside its inference endpoints")
	}
	switch req.URL.EscapedPath() {
	case "/zen/go/v1/chat/completions", "/zen/go/v1/responses", "/zen/go/v1/messages":
	default:
		return nil, errors.New("unsupported OpenCode Go inference endpoint")
	}
	key, err := readKey(t.dir)
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Del("Authorization")
	req.Header.Del("X-Api-Key")
	if req.URL.Path == "/zen/go/v1/messages" {
		req.Header.Set("X-Api-Key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("User-Agent", "cpe/"+version.Get())
	req.Header.Set("X-Opencode-Session", t.sessionID)
	return t.base.RoundTrip(req)
}
