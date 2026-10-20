package agent

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/toolruntime"
)

type turn struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *session) prompt(ctx context.Context, message gai.Message) (*acp.PromptResponse, error) {
	s.mu.Lock()
	if s.closed || s.active != nil {
		s.mu.Unlock()
		return nil, errors.New("session is closed or already has an active prompt")
	}
	ctx, cancel := context.WithTimeout(
		ctx,
		s.config.TurnTimeout,
	)
	//nolint:contextcheck // A turn is cancelled by both its request and its connection lifetime.
	stopLifetime := context.AfterFunc(
		s.lifetime,
		cancel,
	)
	running := &turn{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.active = running
	options := s.record.Options
	dialog := slices.Clone(s.record.Dialog)
	compaction := s.record.Compaction
	s.mu.Unlock()
	defer func() {
		cancel()
		stopLifetime()
		s.mu.Lock()
		s.active = nil
		close(running.done)
		s.mu.Unlock()
	}()

	model := s.config.Models[options.Model]
	ctx, span := s.deps.Traces.Tracer("cpe/agent").Start(
		ctx,
		"chat "+model.ID,
	)
	defer span.End()
	span.SetAttributes(
		attribute.String(
			"gen_ai.operation.name",
			"chat",
		),
		attribute.String(
			"gen_ai.request.model",
			model.ID,
		),
		attribute.String(
			"gen_ai.conversation.id",
			string(s.record.Info.ID),
		),
	)
	message = s.catalog.expand(message)
	dialog = append(
		dialog,
		message,
	)
	generator, generationOptions, err := s.deps.Generators(
		ctx,
		model,
		options.Reasoning,
	)
	if err != nil {
		return nil, err
	}
	if generator == nil {
		return nil, errors.New("generator factory returned no generator")
	}
	hooks := &turnHooks{
		session:        s,
		compaction:     compaction,
		runtimeReset:   s.runtimeReset,
		client:         s.client,
		model:          model,
		modelID:        options.Model,
		maxGenerations: s.config.MaxGenerations,
		completed:      make(map[string]gai.Message),
		pending:        make(map[string]bool),
		streamed:       make(map[uint]bool),
	}
	ctx = toolruntime.WithCompaction(
		ctx,
		func(summary string) {
			hooks.compactionRequest.summary = new(summary)
		},
	)
	loop, err := gaiagent.New(gaiagent.Config{
		Generator: &responseGenerator{
			source:  generator,
			session: s,
			hooks:   hooks,
		},
		Model:            model.ID,
		Instructions:     gai.SystemMessage(gai.TextBlock(s.config.Instructions + s.projectInstructions + s.catalog.instructions() + s.runtime.Instructions())),
		Tools:            s.runtime.Tools(),
		BeforeGeneration: hooks,
		AfterGeneration:  hooks,
		BeforeTool:       hooks,
		AfterTool:        hooks,
	})
	if err != nil {
		return nil, err
	}
	result, runErr := loop.Run(
		ctx,
		gaiagent.RunRequest{
			Dialog:  dialog,
			Options: generationOptions,
		},
	)
	if len(result.Dialog) != 0 {
		dialog = result.Dialog
	}
	// Recover known completed results before marking any remaining calls uncertain.
	for _, completed := range hooks.completed {
		if missingResult(
			dialog,
			completed.Blocks[0].ID,
		) {
			dialog = append(
				dialog,
				completed,
			)
		}
	}
	dialog = settleDialog(dialog)
	cleanup, cleanupCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		10*time.Second,
	)
	defer cleanupCancel()
	updateErr := hooks.finish(cleanup)
	s.mu.Lock()
	s.record.Dialog = dialog
	s.record.Info.UpdatedAt = time.Now().UTC()
	checkpoint := s.record
	if pending := hooks.compactionRequest.checkpoint(len(dialog)); pending != nil {
		checkpoint.Compaction = pending
	}
	s.mu.Unlock()
	saveErr := s.deps.History.Save(
		cleanup,
		checkpoint,
	)
	if saveErr == nil {
		s.mu.Lock()
		s.record.Compaction = checkpoint.Compaction
		s.mu.Unlock()
	}
	if runErr != nil {
		span.RecordError(runErr)
		span.SetStatus(
			codes.Error,
			"generation failed",
		)
	}
	response := &acp.PromptResponse{
		StopReason: acp.StopReasonEndTurn,
		Usage:      protocolUsage(checkpoint.Usage),
	}
	if ctx.Err() != nil {
		response.StopReason = acp.StopReasonCancelled
		return response, errors.Join(
			saveErr,
			updateErr,
		)
	}
	if err := errors.Join(
		runErr,
		saveErr,
		updateErr,
	); err != nil {
		return nil, err
	}
	switch {
	case result.StopReason == "generation_limit":
		response.StopReason = acp.StopReasonMaxTurnRequests
	case result.ModelFinishReason == gai.MaxGenerationLimit:
		response.StopReason = acp.StopReasonMaxTokens
	case result.ModelFinishReason == gai.ContentPolicyViolation:
		response.StopReason = acp.StopReasonRefusal
	}
	return response, nil
}

func missingResult(dialog gai.Dialog, id string) bool {
	found := false
	for _, message := range dialog {
		for _, block := range message.Blocks {
			if block.ID == id {
				if block.BlockType == gai.ToolCall {
					found = true
				}
				if message.Role == gai.ToolResult {
					return false
				}
			}
		}
	}
	return found
}

type turnHooks struct {
	session           *session
	usage             conversation.TokenUsage
	compaction        *conversation.Compaction
	compactionRequest compactionRequest
	runtimeReset      *runtimeReset
	client            *sessionClient
	model             models.Model
	modelID           models.ID
	maxGenerations    uint
	completed         map[string]gai.Message
	pending           map[string]bool
	streamed          map[uint]bool
	generation        uint
	dialog            gai.Dialog
}

// Prepare each provider request without altering canonical saved history.
func (h *turnHooks) BeforeGeneration(ctx context.Context, event gaiagent.BeforeGenerationRequest) (gaiagent.BeforeGenerationDecision, error) {
	h.generation = event.Generation
	h.dialog = event.Request.Dialog
	if h.compactionRequest.summary != nil {
		if err := h.checkpointCompaction(ctx); err != nil {
			return gaiagent.BeforeGenerationDecision{}, err
		}
	}
	if event.Generation >= h.maxGenerations {
		return gaiagent.BeforeGenerationDecision{
			StopReason: "generation_limit",
		}, nil
	}
	request := event.Request
	request.Dialog = nil
	for _, message := range h.runtimeReset.workingDialog(
		event.Request.Dialog,
		h.compaction,
	) {
		foreign := message.Role == gai.Assistant && message.ExtraFields["cpe.model"] != string(h.modelID)
		blocks := message.Blocks
		message.Blocks = nil
		for _, block := range blocks {
			if foreign && block.BlockType == gai.Thinking {
				continue
			}
			if foreign {
				block.ExtraFields = nil
			}
			if block.ModalityType != gai.Text && !slices.Contains(
				h.model.InputModalities,
				block.ModalityType,
			) {
				id := block.ID
				block = gai.TextBlock("[Media retained in history but unsupported by the selected model.]")
				block.ID = id
			}
			message.Blocks = append(
				message.Blocks,
				block,
			)
		}
		if foreign {
			message.ExtraFields = nil
		}
		if len(message.Blocks) != 0 {
			request.Dialog = append(
				request.Dialog,
				message,
			)
		}
	}
	return gaiagent.BeforeGenerationDecision{
		Request: &request,
	}, nil
}

func (h *turnHooks) GenerationChunk(ctx context.Context, event gaiagent.GenerationChunkEvent) error {
	block := event.Chunk.Block
	if event.Chunk.Err != nil {
		return event.Chunk.Err
	}
	if block.Content == nil || block.ModalityType != gai.Text {
		return nil
	}
	if block.BlockType != gai.Content && block.BlockType != gai.Thinking {
		return nil
	}
	h.streamed[event.Generation] = true
	content := acp.TextContentBlock(block.Content.String())
	update := acp.AgentMessageChunkSessionUpdate(content)
	if block.BlockType == gai.Thinking {
		update = acp.AgentThoughtChunkSessionUpdate(content)
	}
	return h.client.update(
		ctx,
		update,
	)
}

func (h *turnHooks) AfterGeneration(ctx context.Context, event gaiagent.AfterGenerationRequest) (*gai.Response, error) {
	response := event.Response
	response.Candidates = slices.Clone(response.Candidates)
	for i, message := range response.Candidates {
		message.ExtraFields = maps.Clone(message.ExtraFields)
		if message.ExtraFields == nil {
			message.ExtraFields = make(map[string]any)
		}
		message.ExtraFields["cpe.model"] = string(h.modelID)
		response.Candidates[i] = message
		for _, block := range message.Blocks {
			if block.BlockType != gai.Content && block.BlockType != gai.Thinking {
				continue
			}
			if h.streamed[event.Generation] && block.ModalityType == gai.Text {
				continue
			}
			content, err := acpContent(block)
			if err != nil {
				return nil, err
			}
			update := acp.AgentMessageChunkSessionUpdate(content)
			if block.BlockType == gai.Thinking {
				update = acp.AgentThoughtChunkSessionUpdate(content)
			}
			if err := h.client.update(
				ctx,
				update,
			); err != nil {
				return nil, err
			}
		}
	}
	if h.model.Limit != 0 && h.usage.InputTokens != nil && h.usage.OutputTokens != nil {
		// This is the latest measured request plus its output, not accumulated usage.
		if err := h.client.update(
			ctx,
			acp.UsageUpdateSessionUpdate(
				*h.usage.InputTokens+*h.usage.OutputTokens,
				uint64(h.model.Limit),
			),
		); err != nil {
			return nil, err
		}
	}
	return &response, nil
}

func (h *turnHooks) BeforeTool(ctx context.Context, event gaiagent.BeforeToolRequest) (gaiagent.BeforeToolDecision, error) {
	h.pending[event.Block.ID] = true
	update := acp.ToolCallSessionUpdate(
		acp.ToolCallId(event.Block.ID),
		event.Call.Name,
	)
	update.RawInput = event.Call.Parameters
	update.Status = new(acp.ToolCallStatusInProgress)
	return gaiagent.BeforeToolDecision{}, h.client.update(
		ctx,
		update,
	)
}

func (h *turnHooks) AfterTool(ctx context.Context, event gaiagent.AfterToolRequest) (gaiagent.AfterToolDecision, error) {
	result := gai.ToolResultMessage(
		event.Block.ID,
		event.Result.Blocks...,
	)
	result.ToolResultError = event.Result.ToolResultError
	h.completed[event.Block.ID] = result
	delete(
		h.pending,
		event.Block.ID,
	)
	return gaiagent.AfterToolDecision{}, h.client.toolResult(
		ctx,
		event.Block.ID,
		result,
	)
}

func (h *turnHooks) finish(ctx context.Context) error {
	var errs []error
	for id := range h.pending {
		message := gai.ToolResultMessage(
			id,
			gai.TextBlock("Execution interrupted; inspect state before retrying."),
		)
		message.ToolResultError = true
		errs = append(
			errs,
			h.client.toolResult(
				ctx,
				id,
				message,
			),
		)
	}
	return errors.Join(errs...)
}
