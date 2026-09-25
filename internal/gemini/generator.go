package gemini

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"iter"
	"maps"
	"net/http"
	"slices"

	"github.com/spachava753/gai"
	"google.golang.org/genai"
)

type provider interface {
	gai.Generator
	gai.StreamingGenerator
	gai.TokenCounter
}
type generator struct{ provider }

// New constructs a Gemini generator that accepts imported tool-call history.
func New(ctx context.Context, config genai.ClientConfig) (gai.Generator, error) {
	client := http.Client{}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = signatureTransport{base: base}
	config.HTTPClient = &client
	sdk, err := genai.NewClient(ctx, &config)
	if err != nil {
		return nil, err
	}
	return &generator{provider: gai.NewGeminiGenerator(sdk)}, nil
}

func (g *generator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	response, err := g.provider.Generate(ctx, importedCalls(req))
	ids := newCallIDs(req.Dialog)
	for i := range response.Candidates {
		for j := range response.Candidates[i].Blocks {
			block := &response.Candidates[i].Blocks[j]
			if block.BlockType == gai.ToolCall {
				block.ID = ids.next()
			}
		}
	}
	return response, err
}
func (g *generator) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		// gai currently drops signatures on streamed function-call parts. Observe
		// them at the SDK boundary and restore them onto the corresponding chunks.
		signatures := &signatureStream{}
		ctx := context.WithValue(ctx, signatureKey{}, signatures)
		ids := newCallIDs(req.Dialog)
		for chunk := range g.provider.Stream(ctx, importedCalls(req)) {
			if chunk.Block.BlockType == gai.ToolCall {
				if signature := signatures.take(); signature != "" {
					chunk.Block.ExtraFields = maps.Clone(chunk.Block.ExtraFields)
					if chunk.Block.ExtraFields == nil {
						chunk.Block.ExtraFields = make(map[string]any)
					}
					chunk.Block.ExtraFields[gai.GeminiExtraFieldThoughtSignature] = signature
				}
				if chunk.Block.ID != "" {
					chunk.Block.ID = ids.next()
				}
			}
			if !yield(chunk) {
				return
			}
		}
	}
}

// Gemini's wire protocol relates results by function name. These IDs belong to
// the local conversation; gai's per-request counters can collide with history.
type callIDs struct {
	used map[string]bool
}

func newCallIDs(dialog gai.Dialog) *callIDs {
	ids := &callIDs{used: make(map[string]bool)}
	for _, message := range dialog {
		for _, block := range message.Blocks {
			if block.ID != "" {
				ids.used[block.ID] = true
			}
		}
	}
	return ids
}

func (ids *callIDs) next() string {
	for {
		// A fresh identity also survives context compaction removing earlier
		// calls from the request while they remain in the run/session tree.
		id := "cpe-gemini-" + rand.Text()
		if !ids.used[id] {
			ids.used[id] = true
			return id
		}
	}
}

func importedCalls(req gai.GenerationRequest) gai.GenerationRequest {
	req.Dialog = slices.Clone(req.Dialog)
	for i, m := range req.Dialog {
		if m.Role != gai.Assistant {
			continue
		}
		for j, b := range m.Blocks {
			if b.BlockType != gai.ToolCall {
				continue
			}
			if signature := b.ExtraFields[gai.GeminiExtraFieldThoughtSignature]; signature == nil || signature == "" {
				m.Blocks = slices.Clone(m.Blocks)
				b.ExtraFields = maps.Clone(b.ExtraFields)
				if b.ExtraFields == nil {
					b.ExtraFields = make(map[string]any)
				}
				b.ExtraFields[gai.GeminiExtraFieldThoughtSignature] = base64.StdEncoding.EncodeToString([]byte("skip_thought_signature_validator"))
				m.Blocks[j] = b
				req.Dialog[i] = m
			}
			break
		}
	}
	return req
}
