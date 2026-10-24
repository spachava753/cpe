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
			len(partial.Blocks) != 0,
		)
		cleanup, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			10*time.Second,
		)
		saveErr := g.session.recordAborted(
			cleanup,
			g.hooks.dialog,
			conversation.AbortedResponse{
				Model:         g.hooks.modelID,
				At:            time.Now().UTC(),
				AfterMessages: len(g.hooks.dialog),
				Reason:        err.Error(),
				Partial:       partial,
			},
		)
		if saveErr != nil || deliveryErr != nil {
			cancel()
			return gai.Response{}, errors.Join(
				err,
				saveErr,
				deliveryErr,
			)
		}
		description := "The incomplete response was recorded as aborted and will not be replayed."
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
		noticeErr := g.session.client.notice(
			cleanup,
			"Model response aborted",
			description,
		)
		cancel()
		if noticeErr != nil || !retry {
			return gai.Response{}, errors.Join(
				err,
				noticeErr,
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

func (g *responseGenerator) generate(ctx context.Context, request gai.GenerationRequest) (gai.Response, gai.Message, gai.Metadata, error, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	partial := gai.Message{
		Role: gai.Assistant,
	}
	if source, ok := g.source.(gai.StreamingGenerator); ok {
		observed := &responseStream{
			source:   source,
			hooks:    g.hooks,
			partial:  &partial,
			metadata: make(gai.Metadata),
		}
		adapter := gai.StreamingAdapter{
			S: observed,
		}
		response, err := adapter.Generate(
			ctx,
			request,
		)
		return response, partial, observed.metadata, observed.deliveryErr, err
	}
	response, err := g.source.Generate(
		ctx,
		request,
	)
	if err != nil && len(response.Candidates) != 0 {
		partial = response.Candidates[0]
	}
	return response, partial, response.UsageMetadata, nil, err
}

type responseStream struct {
	source      gai.StreamingGenerator
	hooks       *turnHooks
	partial     *gai.Message
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
				s.partial.Blocks = append(
					s.partial.Blocks,
					chunk.Block,
				)
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

func (s *session) recordAborted(ctx context.Context, dialog gai.Dialog, aborted conversation.AbortedResponse) error {
	s.mu.Lock()
	s.record.Dialog = dialog
	s.record.AbortedResponses = append(
		s.record.AbortedResponses,
		aborted,
	)
	s.record.Info.UpdatedAt = aborted.At
	checkpoint := s.record
	s.mu.Unlock()
	return s.deps.History.Save(
		ctx,
		checkpoint,
	)
}
