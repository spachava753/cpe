package copilot

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/url"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	oopts "github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/responses"
)

type backend interface {
	gai.Generator
	gai.StreamingGenerator
}
type generator struct{ backend }
type requestFlags struct {
	initiator string
	vision    bool
}
type flagsKey struct{}
type userInitiationKey struct{}

// WithUserInitiation marks the primary response to an explicit user prompt.
// Internal requests (including compaction and tool continuations) omit it,
// regardless of synthetic user messages in their dialog.
func WithUserInitiation(ctx context.Context) context.Context {
	return context.WithValue(ctx, userInitiationKey{}, struct{}{})
}

// New creates a lazy Copilot generator. It can be selected before login.
// Streaming is exposed only when supported by the catalog model.
func New(protocol, dir, sessionID string, streaming bool) (gai.Generator, error) {
	if protocol != chatProtocol && protocol != responsesProtocol && protocol != messagesProtocol {
		return nil, errors.New("GitHub Copilot requires openai, responses, or anthropic protocol")
	}
	if !validToken(sessionID) {
		return nil, errors.New("GitHub Copilot requires a stable conversation ID")
	}
	client := newClient()
	client.Timeout = 10 * time.Minute
	client.Transport = &transport{store: &credentialStore{dir: dir, client: newClient()}, sessionID: sessionID, base: http.DefaultTransport}
	const key = "cpe-token-loaded-by-transport"
	var source backend
	var err error
	switch protocol {
	case chatProtocol:
		source, err = gai.NewOpenAiGenerator(client, apiURL, key)
	case responsesProtocol:
		c := openai.NewClient(oopts.WithAPIKey(key), oopts.WithBaseURL(apiURL), oopts.WithHTTPClient(client), oopts.WithMaxRetries(0))
		source = responses.New(&c.Responses)
	case messagesProtocol:
		c := anthropic.NewClient(aopts.WithAPIKey(key), aopts.WithBaseURL(apiURL), aopts.WithHTTPClient(client), aopts.WithMaxRetries(0))
		source = gai.NewAnthropicGenerator(&c.Messages)
	}
	if err != nil {
		return nil, err
	}
	g := &generator{source}
	if !streaming {
		return struct{ gai.Generator }{g}, nil
	}
	return g, nil
}

func requestContext(ctx context.Context, req gai.GenerationRequest) context.Context {
	flags := requestFlags{initiator: "agent"}
	if _, user := ctx.Value(userInitiationKey{}).(struct{}); user {
		flags.initiator = "user"
	}
	for _, message := range req.Dialog {
		for _, block := range message.Blocks {
			if block.ModalityType == gai.Image {
				flags.vision = true
			}
		}
	}
	return context.WithValue(ctx, flagsKey{}, flags)
}

func (g *generator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	response, err := g.backend.Generate(requestContext(ctx, req), req)
	return response, serviceError(err)
}

func (g *generator) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		for chunk := range g.backend.Stream(requestContext(ctx, req), req) {
			chunk.Err = serviceError(chunk.Err)
			if !yield(chunk) {
				return
			}
		}
	}
}

func serviceError(err error) error {
	var upstream *gai.ApiErr
	if !errors.As(err, &upstream) {
		return err
	}
	result := *upstream
	result.Provider = credentialName
	result.Cause = err
	if result.Kind == gai.APIErrorKindAuthentication {
		result.Message = "GitHub Copilot authentication failed; use /login copilot to sign in again"
	}
	return &result
}

type transport struct {
	store     *credentialStore
	sessionID string
	base      http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.URL.Scheme != "https" || req.URL.Host != "api.githubcopilot.com" || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.ForceQuery || req.URL.Fragment != "" {
		return nil, errors.New("refusing to send Copilot credentials outside inference endpoints")
	}
	switch req.URL.EscapedPath() {
	case "/chat/completions", "/responses", "/v1/messages":
	default:
		return nil, errors.New("unsupported Copilot inference endpoint")
	}
	saved, err := t.store.current(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	endpoint, _ := url.Parse(saved.APIURL)
	req.URL.Host = endpoint.Host
	req.Host = ""
	req.Header.Del("Authorization")
	req.Header.Del("X-Api-Key")
	req.Header.Set("Authorization", "Bearer "+saved.Token)
	clientHeaders(req)
	req.Header.Set("X-Request-Id", uuid.NewString())
	req.Header.Set("X-Session-Id", t.sessionID)
	req.Header.Set("Openai-Intent", "conversation-edits")
	flags, _ := req.Context().Value(flagsKey{}).(requestFlags)
	req.Header.Set("X-Initiator", flags.initiator)
	req.Header.Del("Copilot-Vision-Request")
	if flags.vision {
		req.Header.Set("Copilot-Vision-Request", "true")
	}
	return t.base.RoundTrip(req)
}
