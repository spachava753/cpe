package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"net"
	"strings"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/providers"
)

// responseGenerator retries one provider call, never an agent loop or a tool call.
// It owns streaming so failed attempts cannot enter GAI's response assembler together.
type responseGenerator struct {
	source  gai.Generator
	session *session
	hooks   *turnHooks
}

func (g *responseGenerator) Generate(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
	request = g.hooks.providerRequest(request)
	for attempt := uint(0); ; attempt++ {
		if err := ctx.Err(); err != nil {
			return gai.Response{}, err
		}
		g.hooks.streamed[g.hooks.generation] = false
		response, partial, metadata, deliveryErr, err := g.generate(
			ctx,
			request,
		)
		usage, usageErr := conversation.ParseTokenUsage(metadata)
		if usageErr == nil {
			usageErr = g.session.recordUsage(usage)
		}
		err = errors.Join(
			err,
			usageErr,
		)
		if err == nil {
			g.hooks.usage = usage
			return response, nil
		}
		retry := usageErr == nil && ctx.Err() == nil && deliveryErr == nil && attempt < g.session.config.MaxResponseRetries && retryResponse(
			err,
			partial,
		)
		if deliveryErr != nil {
			return gai.Response{}, errors.Join(
				err,
				deliveryErr,
			)
		}
		cleanup, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			10*time.Second,
		)
		description := "The model call was aborted."
		if retry {
			description += fmt.Sprintf(
				" Retrying the model response (%d of %d).",
				attempt+1,
				g.session.config.MaxResponseRetries,
			)
		} else if ctx.Err() != nil {
			description += " The turn was cancelled; no retry will be made."
		} else {
			description += " No further retry will be made: " + err.Error()
		}
		updateErr := g.session.client.update(
			cleanup,
			acp.AgentMessageChunkSessionUpdate(acp.TextContentBlock("\n["+description+"]\n")),
		)
		if updateErr == nil && g.session.client.notices {
			updateErr = g.session.client.notice(
				cleanup,
				"Model response aborted",
				description,
			)
		}
		cancel()
		if updateErr != nil || !retry {
			return gai.Response{}, errors.Join(
				err,
				updateErr,
			)
		}
		delay := 250 * time.Millisecond << min(
			attempt,
			5,
		)
		if api, ok := errors.AsType[*gai.ApiErr](err); ok {
			if requested, present := api.RetryAfter(); present && requested > delay {
				delay = requested
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return gai.Response{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *responseGenerator) generate(ctx context.Context, request gai.GenerationRequest) (gai.Response, bool, gai.Metadata, error, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if source, ok := g.source.(gai.StreamingGenerator); ok {
		observed := &responseStream{
			source:   source,
			hooks:    g.hooks,
			metadata: make(gai.Metadata),
		}
		adapter := gai.StreamingAdapter{
			S: observed,
		}
		response, err := adapter.Generate(
			ctx,
			request,
		)
		return response, observed.partial, observed.metadata, observed.deliveryErr, err
	}
	response, err := g.source.Generate(
		ctx,
		request,
	)
	partial := len(response.Candidates) != 0 && len(response.Candidates[0].Blocks) != 0
	return response, partial, response.UsageMetadata, nil, err
}

type responseStream struct {
	source      gai.StreamingGenerator
	hooks       *turnHooks
	partial     bool
	deliveryErr error
	metadata    gai.Metadata
}

func (s *responseStream) Stream(ctx context.Context, request gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		for chunk := range s.source.Stream(
			ctx,
			request,
		) {
			if chunk.Block.BlockType == gai.MetadataBlockType && chunk.Block.Content != nil {
				// Keep final per-attempt snapshots even if assembly later fails or is cancelled.
				decoder := json.NewDecoder(strings.NewReader(chunk.Block.Content.String()))
				decoder.UseNumber()
				var metadata gai.Metadata
				if err := decoder.Decode(&metadata); err != nil {
					yield(gai.StreamChunk{
						Err: fmt.Errorf(
							"decode provider usage: %w",
							err,
						),
					})
					return
				}
				maps.Copy(
					s.metadata,
					metadata,
				)
			}
			if err := ctx.Err(); err != nil {
				yield(gai.StreamChunk{
					Err: err,
				})
				return
			}
			if chunk.Err != nil {
				yield(chunk)
				return
			}
			if chunk.Block.Content != nil && chunk.Block.BlockType != gai.MetadataBlockType {
				s.partial = true
			}
			if err := s.hooks.GenerationChunk(
				ctx,
				gaiagent.GenerationChunkEvent{
					Generation: s.hooks.generation,
					Chunk:      chunk,
				},
			); err != nil {
				s.deliveryErr = err
				yield(gai.StreamChunk{
					Err: err,
				})
				return
			}
			if !yield(chunk) {
				return
			}
		}
	}
}

func retryResponse(err error, partial bool) bool {
	if providers.IsCredentialError(err) {
		return false
	}
	if api, ok := errors.AsType[*gai.ApiErr](err); ok {
		if api.Retryable() {
			return true
		}
		if api.StatusCode >= 400 || api.Kind != "" && api.Kind != gai.APIErrorKindUnknown {
			return false
		}
		// Unknown stream errors can wrap transport failures or follow partial output.
	}
	if errors.Is(
		err,
		context.Canceled,
	) || errors.Is(
		err,
		context.DeadlineExceeded,
	) || errors.Is(
		err,
		io.EOF,
	) || errors.Is(
		err,
		io.ErrUnexpectedEOF,
	) {
		return true
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	// An unknown failure after output began is an interrupted response, not a
	// configuration failure. Errors before any output are not blindly retried.
	return partial
}
