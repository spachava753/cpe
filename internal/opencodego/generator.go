package opencodego

import (
	"context"
	"errors"
	"iter"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
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

// New binds a wire protocol to OpenCode Go's endpoints, saved credentials, and
// error identity. SDK retries are disabled; credentials reload on each request.
func New(protocol, dir, sessionID string) (gai.Generator, error) {
	url, err := baseURL(protocol)
	if err != nil {
		return nil, err
	}
	client, err := newClient(dir, sessionID)
	if err != nil {
		return nil, err
	}
	const key = "cpe-key-loaded-by-transport" // Replaced before reaching the wire.
	var source backend
	switch protocol {
	case chatProtocol:
		source, err = gai.NewOpenAiGenerator(client, url, key)
	case responsesProtocol:
		c := openai.NewClient(oopts.WithAPIKey(key), oopts.WithHTTPClient(client), oopts.WithBaseURL(url), oopts.WithMaxRetries(0))
		source = responses.New(&c.Responses)
	case anthropicProtocol:
		c := anthropic.NewClient(aopts.WithAPIKey(key), aopts.WithHTTPClient(client), aopts.WithBaseURL(url), aopts.WithMaxRetries(0))
		source = gai.NewAnthropicGenerator(&c.Messages)
	}
	if err != nil {
		return nil, err
	}
	return &generator{source}, nil
}

func (g *generator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	response, err := g.backend.Generate(ctx, req)
	return response, serviceError(err)
}

func (g *generator) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		for chunk := range g.backend.Stream(ctx, req) {
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
		result.Message = "OpenCode Go authentication failed; check your API key from https://opencode.ai/auth and use /login opencode-go to replace it"
	}
	return &result
}
