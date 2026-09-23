package responses

import (
	"context"
	"encoding/json"
	"iter"
	"maps"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	api "github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
)

// New returns a generator preserving Responses cache-write usage. The service
// controls authentication, endpoint, and any provider-specific event decoding.
func New(service gai.ResponsesService) *Generator { return &Generator{service: service} }

// Generator exposes the same generation interfaces as gai.ResponsesGenerator.
type Generator struct{ service gai.ResponsesService }

// Generate preserves reported usage even when the provider returns an error.
func (g *Generator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	s := &usageService{ResponsesService: g.service}
	response, err := gai.NewResponsesGenerator(s).Generate(ctx, req)
	if s.usage != nil {
		if response.UsageMetadata == nil {
			response.UsageMetadata = gai.Metadata{}
		}
		maps.Copy(response.UsageMetadata, s.usage)
	}
	return response, err
}

// Stream forwards content and adds complete SDK usage to terminal metadata.
func (g *Generator) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		s := &usageService{ResponsesService: g.service}
		for chunk := range gai.NewResponsesGenerator(s).Stream(ctx, req) {
			if s.usage != nil {
				if chunk.Err != nil {
					if !yield(gai.StreamChunk{Block: gai.MetadataBlock(s.usage)}) {
						return
					}
				} else if chunk.Block.BlockType == gai.MetadataBlockType {
					metadata := gai.Metadata{}
					if chunk.Block.Content != nil {
						_ = json.Unmarshal([]byte(chunk.Block.Content.String()), &metadata)
					}
					maps.Copy(metadata, s.usage)
					chunk.Block = gai.MetadataBlock(metadata)
				}
			}
			if !yield(chunk) {
				return
			}
		}
	}
}

type usageService struct {
	gai.ResponsesService
	usage gai.Metadata
}

func (s *usageService) capture(response api.Response) {
	if !response.JSON.Usage.Valid() {
		return
	}
	u := response.Usage
	s.usage = gai.Metadata{
		gai.UsageMetricInputTokens:      int(u.InputTokens),
		gai.UsageMetricGenerationTokens: int(u.OutputTokens),
		gai.UsageMetricCacheReadTokens:  int(u.InputTokensDetails.CachedTokens),
		gai.UsageMetricCacheWriteTokens: int(u.InputTokensDetails.CacheWriteTokens),
	}
}

func (s *usageService) New(ctx context.Context, body api.ResponseNewParams, opts ...option.RequestOption) (*api.Response, error) {
	response, err := s.ResponsesService.New(ctx, body, opts...)
	if response != nil {
		s.capture(*response)
	}
	return response, err
}

func (s *usageService) NewStreaming(ctx context.Context, body api.ResponseNewParams, opts ...option.RequestOption) *ssestream.Stream[api.ResponseStreamEventUnion] {
	source := s.ResponsesService.NewStreaming(ctx, body, opts...)
	return ssestream.NewStream[api.ResponseStreamEventUnion](&usageDecoder{source: source, service: s}, nil)
}

type usageDecoder struct {
	source  *ssestream.Stream[api.ResponseStreamEventUnion]
	service *usageService
	event   ssestream.Event
}

func (d *usageDecoder) Next() bool {
	if !d.source.Next() {
		return false
	}
	event := d.source.Current()
	switch event.Type {
	case "response.completed", "response.incomplete", "response.failed":
		d.service.capture(event.Response)
	}
	d.event = ssestream.Event{Type: event.Type, Data: []byte(event.RawJSON())}
	return true
}
func (d *usageDecoder) Event() ssestream.Event { return d.event }
func (d *usageDecoder) Err() error             { return d.source.Err() }
func (d *usageDecoder) Close() error           { return d.source.Close() }
