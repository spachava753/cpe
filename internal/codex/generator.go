package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"

	usageResponses "github.com/spachava753/cpe/internal/responses"
)

const responsesURL = "https://chatgpt.com/backend-api/codex/responses"
const tokenURL = "https://auth.openai.com/oauth/token"

// New creates a Codex generator backed by CPE's credential file. Credentials
// are loaded only when a request is made, allowing the TUI to start before login.
// The caller supplies ~/.cpe/auth.json; tests may supply an isolated file.
func New(authFile string) gai.Generator {
	s := &credentialStore{path: authFile, tokenURL: tokenURL, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect}}
	return newGenerator(s, http.DefaultTransport, responsesURL)
}

func noRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

type generator struct{ *usageResponses.Generator }

func (g *generator) Generate(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
	return (&gai.StreamingAdapter{S: g}).Generate(ctx, request)
}

func newGenerator(s *credentialStore, base http.RoundTripper, endpoint string) *generator {
	c := openai.NewClient(
		option.WithAPIKey("oauth"), // Replaced by the transport; never sent.
		option.WithOrganization(""), option.WithProject(""),
		option.WithBaseURL(strings.TrimSuffix(endpoint, "/responses")),
		option.WithMaxRetries(0),
		option.WithJSONDel("max_output_tokens"),
		option.WithJSONDel("temperature"),
		option.WithJSONDel("top_p"),
		option.WithJSONSet("parallel_tool_calls", false),
		option.WithHTTPClient(&http.Client{
			Timeout: 10 * time.Minute, CheckRedirect: noRedirect,
			Transport: &transport{store: s, base: base, endpoint: endpoint},
		}),
	)
	return &generator{usageResponses.New(&responseService{&c.Responses})}
}

type responseService struct{ *responses.ResponseService }

func (s *responseService) NewStreaming(ctx context.Context, body responses.ResponseNewParams, opts ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion] {
	source := s.ResponseService.NewStreaming(ctx, body, opts...)
	return ssestream.NewStream[responses.ResponseStreamEventUnion](&eventDecoder{source: source}, nil)
}

// Normalize Codex's response.done alias and reject streams cut off before a
// terminal event. Otherwise a disconnected stream could commit partial output.
type eventDecoder struct {
	source   *ssestream.Stream[responses.ResponseStreamEventUnion]
	event    ssestream.Event
	err      error
	terminal bool
}

func (d *eventDecoder) Next() bool {
	if d.terminal || d.err != nil {
		return false
	}
	if !d.source.Next() {
		d.err = d.source.Err()
		if d.err == nil {
			d.err = errors.New("codex stream ended before a completion event; the request was not retried")
		}
		return false
	}
	event := d.source.Current()
	data := []byte(event.RawJSON())
	kind := event.Type
	if kind == "response.done" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			d.err = errors.New("invalid Codex completion event")
			return false
		}
		var response struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw["response"], &response); err != nil {
			d.err = errors.New("invalid Codex completion response")
			return false
		}
		switch response.Status {
		case "", "completed":
			kind = "response.completed"
		case "failed":
			kind = "response.failed"
		case "incomplete":
			kind = "response.incomplete"
		default:
			d.err = errors.New("unexpected Codex completion status")
			return false
		}
		raw["type"], _ = json.Marshal(kind)
		data, d.err = json.Marshal(raw)
		if d.err != nil {
			return false
		}
	}
	switch kind {
	case "response.completed", "response.failed", "response.incomplete", "error":
		d.terminal = true
	}
	d.event = ssestream.Event{Type: kind, Data: data}
	return true
}
func (d *eventDecoder) Event() ssestream.Event { return d.event }
func (d *eventDecoder) Err() error             { return d.err }
func (d *eventDecoder) Close() error           { return d.source.Close() }

type transport struct {
	store    *credentialStore
	base     http.RoundTripper
	endpoint string
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.URL.String() != t.endpoint {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, errors.New("refusing to send Codex credentials to an unexpected endpoint")
	}
	cred, err := t.store.credential(req.Context(), true)
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+cred.access)
	clone.Header.Set("ChatGPT-Account-ID", cred.accountID)
	clone.Header.Set("Originator", "cpe")
	clone.Header.Set("OpenAI-Beta", "responses=experimental")
	clone.Header.Set("Accept", "text/event-stream")
	clone.Header.Del("X-Api-Key")
	clone.Header.Del("OpenAI-Organization")
	clone.Header.Del("OpenAI-Project")
	resp, err := t.base.RoundTrip(clone)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return nil, errors.New("codex rejected the login (HTTP 401); sign in again with /login in CPE; the request was not retried")
	}
	return resp, err
}
