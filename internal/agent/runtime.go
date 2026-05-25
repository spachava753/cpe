package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/render"
	"github.com/spachava753/cpe/internal/storage"
)

const compactionWarningText = `[COMPACTION WARNING]
The conversation has exceeded the configured compaction threshold. Before continuing much further, call the compact_conversation tool with a compact but complete summary of the conversation state needed to continue. This warning will continue to appear until compaction is performed.`

// Runtime owns CPE's full agentic loop around a single-turn tool-capable model.
type Runtime struct {
	G                gai.ToolCallingGenerator
	DialogSaver      storage.DialogSaver
	ContentRenderer  render.Iface
	ThinkingRenderer render.Iface
	ToolCallRenderer render.Iface
	MetadataRenderer render.Iface
	Stdout           io.Writer
	Stderr           io.Writer
	Cfg              config.Config

	// internal state
	toolCallbacks      map[string]gai.ToolCallback
	compactionRestarts int
	cumulativeCostUSD  float64
}

// Register registers a tool with the provider model and stores its callback.
func (r *Runtime) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if r.toolCallbacks == nil {
		r.toolCallbacks = make(map[string]gai.ToolCallback)
	}
	if tool.Name == "" {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool name cannot be empty")}
	}
	if tool.Name == gai.ToolChoiceAuto || tool.Name == gai.ToolChoiceToolsRequired {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool name is reserved")}
	}
	if _, exists := r.toolCallbacks[tool.Name]; exists {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool already registered")}
	}
	if err := r.G.Register(tool); err != nil {
		return err
	}
	r.toolCallbacks[tool.Name] = callback
	return nil
}

// Generate runs the dialog until a terminal assistant response, nil-callback
// terminal tool, callback error, or compaction restart limit is reached.
func (r *Runtime) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
	current := append(gai.Dialog(nil), dialog...)

	if err := r.validateToolChoice(opts); err != nil {
		return current, err
	}

	if r.ThinkingRenderer == nil || r.ContentRenderer == nil || r.ToolCallRenderer == nil {
		r.configureOutput()
	}

	for {
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		default:
		}

		var err error
		current, err = r.save(ctx, current)
		if err != nil {
			return current, err
		}
		for _, toolResultMsg := range trailingToolResults(current) {
			r.printToolResult(current, toolResultMsg)
		}

		resp, err := r.G.Generate(ctx, current, opts)
		if err != nil {
			r.printResponse(resp)
			r.printTokenUsage(resp.UsageMetadata)
			return current, err
		}
		if len(resp.Candidates) != 1 {
			return current, fmt.Errorf("expected exactly one candidate in response, got: %d", len(resp.Candidates))
		}
		if resp.Candidates[0].Role != gai.Assistant {
			return current, fmt.Errorf("expected assistant role in response, got: %v", resp.Candidates[0].Role)
		}

		// save response
		r.attachAgentMetadata(&resp.Candidates[0], resp.UsageMetadata)
		current = append(current, resp.Candidates[0])
		current, err = r.save(ctx, current)
		if err != nil {
			return current, err
		}

		r.printResponse(resp)
		r.printTokenUsage(resp.UsageMetadata)

		if resp.FinishReason != gai.ToolUse {
			return current, nil
		}

		// compact conversation
		current, err = r.compact(current)
		if err != nil {
			return current, err
		}
		current, err = r.save(ctx, current)
		if err != nil {
			return current, err
		}
		// if compacted, last message will be a user type message
		lastMsg := current[len(current)-1]
		if lastMsg.Role == gai.User {
			continue
		}

		warningInjected := false
		for _, block := range lastMsg.Blocks {
			if block.BlockType != gai.ToolCall {
				continue
			}
			if block.Content == nil {
				return current, errors.New("invalid tool call JSON: missing content")
			}
			var tc gai.ToolCallInput
			if err := json.Unmarshal([]byte(block.Content.String()), &tc); err != nil {
				return current, fmt.Errorf("invalid tool call JSON: %w", err)
			}
			if tc.Name == "" {
				return current, fmt.Errorf("missing tool name")
			}
			if _, exists := r.toolCallbacks[tc.Name]; !exists {
				return current, fmt.Errorf("tool '%s' not found", tc.Name)
			}
			params := tc.Parameters
			if params == nil {
				params = make(map[string]any)
			}

			callback := r.toolCallbacks[tc.Name]
			// TODO: what happens when there are a mix of nil tool callback and some non-nil? Should we even allow nil callback?
			if callback == nil {
				return current, nil
			}
			result, err := callback.Call(ctx, params)
			if err != nil {
				return current, err
			}
			if err := validateToolResultMessage(&result, block.ID); err != nil {
				return current, fmt.Errorf("invalid tool result message: %w", err)
			}
			if !warningInjected && r.shouldInjectCompactionWarning(resp.UsageMetadata) {
				warningBlock := gai.TextBlock(compactionWarningText)
				warningBlock.ID = block.ID
				result.Blocks = append([]gai.Block{warningBlock}, result.Blocks...)
				warningInjected = true
			}

			// ensure that all of the blocks in the tool result have the associated tool call block id
			for i := range result.Blocks {
				result.Blocks[i].ID = block.ID
			}

			current = append(current, result)
		}
	}
}

func validateToolResultMessage(message *gai.Message, toolCallID string) error {
	if message.Role != gai.ToolResult {
		return fmt.Errorf("message must have ToolResult role, got: %v", message.Role)
	}
	if len(message.Blocks) == 0 {
		return fmt.Errorf("message must have at least one block")
	}
	for i, block := range message.Blocks {
		if block.ID != "" && block.ID != toolCallID {
			return fmt.Errorf("block %d has incorrect ID: expected %q or empty, got %q", i, toolCallID, block.ID)
		}
		if block.Content == nil {
			return fmt.Errorf("block %d has nil content", i)
		}
		if block.BlockType == "" {
			return fmt.Errorf("block %d is missing block type", i)
		}
		if block.MimeType == "" {
			return fmt.Errorf("block %d is missing MIME type", i)
		}
		switch block.ModalityType {
		case gai.Text:
			if !strings.HasPrefix(block.MimeType, "text/") {
				return fmt.Errorf("block %d has text modality but non-text MIME type: %q", i, block.MimeType)
			}
		case gai.Image:
			if !strings.HasPrefix(block.MimeType, "image/") && block.MimeType != "application/pdf" {
				return fmt.Errorf("block %d has image modality but non-image MIME type: %q", i, block.MimeType)
			}
		case gai.Audio:
			if !strings.HasPrefix(block.MimeType, "audio/") {
				return fmt.Errorf("block %d has audio modality but non-audio MIME type: %q", i, block.MimeType)
			}
		case gai.Video:
			if !strings.HasPrefix(block.MimeType, "video/") {
				return fmt.Errorf("block %d has video modality but non-video MIME type: %q", i, block.MimeType)
			}
		default:
			return fmt.Errorf("block %d has invalid modality type: %v", i, block.ModalityType)
		}
	}
	return nil
}

func (r *Runtime) shouldInjectCompactionWarning(metadata gai.Metadata) bool {
	if r.Cfg.Compaction == nil || r.Cfg.Compaction.TokenThreshold == 0 {
		return false
	}

	inputTokens, hasInputTokens := gai.InputTokens(metadata)
	outputTokens, hasOutputTokens := gai.OutputTokens(metadata)
	if !hasInputTokens && !hasOutputTokens {
		return false
	}
	return uint(inputTokens+outputTokens) >= r.Cfg.Compaction.TokenThreshold
}

func (r *Runtime) validateToolChoice(opts *gai.GenOpts) error {
	if opts == nil || opts.ToolChoice == "" || opts.ToolChoice == gai.ToolChoiceAuto || opts.ToolChoice == gai.ToolChoiceToolsRequired {
		return nil
	}
	if _, exists := r.toolCallbacks[opts.ToolChoice]; !exists {
		return gai.InvalidToolChoiceErr(fmt.Sprintf("tool '%s' not found", opts.ToolChoice))
	}
	return nil
}

func (r *Runtime) save(ctx context.Context, dialog gai.Dialog) (gai.Dialog, error) {
	if r.DialogSaver == nil {
		return dialog, nil
	}
	idx := 0
	for saved, err := range r.DialogSaver.SaveDialog(ctx, slices.Values(dialog)) {
		if err != nil {
			return nil, err
		}
		dialog[idx] = saved
		idx++
	}
	return dialog, nil
}

func (r *Runtime) attachAgentMetadata(msg *gai.Message, metadata gai.Metadata) {
	if msg.ExtraFields == nil {
		msg.ExtraFields = make(map[string]any)
	}

	if r.Cfg.Model.Ref != "" {
		msg.ExtraFields[storage.AgentMetadataModelRefKey] = r.Cfg.Model.Ref
	}
	if r.Cfg.Model.ID != "" {
		msg.ExtraFields[storage.AgentMetadataModelIDKey] = r.Cfg.Model.ID
	}
	if r.Cfg.Model.Type != "" {
		msg.ExtraFields[storage.AgentMetadataModelTypeKey] = r.Cfg.Model.Type
	}
	if r.Cfg.Model.DisplayName != "" {
		msg.ExtraFields[storage.AgentMetadataModelDisplayNameKey] = r.Cfg.Model.DisplayName
	}

	if inputTokens, ok := gai.InputTokens(metadata); ok {
		msg.ExtraFields[storage.AgentMetadataInputTokensKey] = int64(inputTokens)
	}
	if outputTokens, ok := gai.OutputTokens(metadata); ok {
		msg.ExtraFields[storage.AgentMetadataOutputTokensKey] = int64(outputTokens)
	}
	if cacheRead, ok := gai.CacheReadTokens(metadata); ok {
		msg.ExtraFields[storage.AgentMetadataCacheReadTokensKey] = int64(cacheRead)
	}
	if cacheWrite, ok := gai.CacheWriteTokens(metadata); ok {
		msg.ExtraFields[storage.AgentMetadataCacheWriteTokensKey] = int64(cacheWrite)
	}
}

func (r *Runtime) compact(current gai.Dialog) (gai.Dialog, error) {
	if r.Cfg.Compaction == nil {
		return current, nil
	}

	lastMsg := current[len(current)-1]

	idx := slices.IndexFunc(lastMsg.Blocks, func(b gai.Block) bool {
		if r.Cfg.Compaction == nil {
			return false
		}

		if b.BlockType != gai.ToolCall {
			return false
		}

		var tci gai.ToolCallInput
		if err := json.Unmarshal([]byte(b.Content.String()), &tci); err != nil {
			panic(err)
		}

		return tci.Name == r.Cfg.Compaction.Tool.Name
	})
	if idx == -1 {
		return current, nil
	}

	var compactionTool gai.ToolCallInput
	if err := json.Unmarshal([]byte(lastMsg.Blocks[idx].Content.String()), &compactionTool); err != nil {
		panic(err)
	}

	if uint(r.compactionRestarts) >= r.Cfg.Compaction.MaxCompactions {
		return current, fmt.Errorf("maximum compaction restarts exceeded")
	}

	previousLeafID := storage.GetMessageID(lastMsg)
	paramJson, err := json.Marshal(compactionTool.Parameters)
	if err != nil {
		panic(err)
	}
	data := config.CompactionTemplateData{
		PreviousLeafID:     previousLeafID,
		Dialog:             current,
		ToolArguments:      compactionTool.Parameters,
		ToolArgumentsJSON:  string(paramJson),
		CompactionToolName: r.Cfg.Compaction.Tool.Name,
	}
	var rendered bytes.Buffer
	if err := r.Cfg.Compaction.InitialMessageTemplate.Execute(&rendered, data); err != nil {
		return current, fmt.Errorf("rendering compaction initial message: %w", err)
	}

	root := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(rendered.String())}}
	if previousLeafID != "" {
		root.ExtraFields = map[string]any{storage.MessageCompactionParentIDKey: previousLeafID}
	}
	r.compactionRestarts++
	return gai.Dialog{root}, nil
}
