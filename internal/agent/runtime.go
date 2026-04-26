package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/render"
	"github.com/spachava753/cpe/internal/storage"
)

const compactionWarningText = `[COMPACTION WARNING]
The conversation has exceeded the configured compaction threshold. Before continuing much further, call the compact_conversation tool with a compact but complete summary of the conversation state needed to continue. This warning will continue to appear until compaction is performed.`

type registeredTool struct {
	tool     gai.Tool
	callback gai.ToolCallback
}

type compactionState struct {
	warningActive bool
	restarts      uint
}

// Runtime owns CPE's full agentic loop around a single-turn tool-capable model.
type Runtime struct {
	model             gai.ToolCapableGenerator
	tools             map[string]registeredTool
	dialogSaver       storage.DialogSaver
	contentRenderer   render.Iface
	thinkingRenderer  render.Iface
	toolCallRenderer  render.Iface
	metadataRenderer  render.Iface
	stdout            io.Writer
	stderr            io.Writer
	cfg               config.Config
	cumulativeCostUSD float64
	compaction        compactionState
}

// RuntimeOption configures Runtime construction.
type RuntimeOption func(*Runtime)

// NewRuntime creates a CPE-owned runtime around a single-turn model generator.
func NewRuntime(model gai.ToolCapableGenerator, cfg config.Config, saver storage.DialogSaver, stdout, stderr io.Writer, disablePrinting bool, opts ...RuntimeOption) *Runtime {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	r := &Runtime{
		model:       model,
		tools:       make(map[string]registeredTool),
		dialogSaver: saver,
		stdout:      stdout,
		stderr:      stderr,
		cfg:         cfg,
	}
	r.configureOutput(disablePrinting)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register registers a tool with the provider model and stores its callback.
func (r *Runtime) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if tool.Name == "" {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool name cannot be empty")}
	}
	if tool.Name == gai.ToolChoiceAuto || tool.Name == gai.ToolChoiceToolsRequired {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool name is reserved")}
	}
	if _, exists := r.tools[tool.Name]; exists {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool already registered")}
	}
	if err := r.model.Register(tool); err != nil {
		return err
	}
	r.tools[tool.Name] = registeredTool{tool: tool, callback: callback}
	return nil
}

// Generate runs the dialog until a terminal assistant response, nil-callback
// terminal tool, callback error, or compaction restart limit is reached.
func (r *Runtime) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	current := append(gai.Dialog(nil), dialog...)
	if optsGen == nil {
		optsGen = func(gai.Dialog) *gai.GenOpts { return nil }
	}

	for {
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		default:
		}

		var err error
		current, err = r.saveAndPrintPending(ctx, current)
		if err != nil {
			return current, err
		}

		opts := optsGen(current)
		if err := r.validateToolChoice(opts); err != nil {
			return current, err
		}

		resp, err := r.model.Generate(ctx, current, opts)
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

		current = append(current, resp.Candidates[0])
		if r.dialogSaver != nil {
			resp, err = saveAssistantResponse(ctx, r.dialogSaver, current[:len(current)-1], resp)
			if err != nil {
				return current, fmt.Errorf("failed to save assistant message: %w", err)
			}
			current[len(current)-1] = resp.Candidates[0]
		}

		r.printResponse(resp)
		r.printTokenUsage(resp.UsageMetadata)
		r.updateCompactionWarning(resp.UsageMetadata)

		if resp.FinishReason != gai.ToolUse {
			return current, nil
		}

		calls, err := r.extractToolCalls(resp.Candidates[0])
		if err != nil {
			return current, err
		}
		if len(calls) == 0 {
			return current, nil
		}

		if compactionCall, ok := r.firstCompactionCall(calls); ok {
			current, err = r.restartCompacted(ctx, current, compactionCall)
			if err != nil {
				return current, err
			}
			continue
		}

		var injectedWarning bool
		for _, call := range calls {
			registered := r.tools[call.Name]
			if registered.callback == nil {
				return current, nil
			}

			result, err := registered.callback.Call(ctx, call.ParametersJSON, call.ID)
			if err != nil {
				return current, err
			}
			if err := validateToolResultMessage(&result, call.ID); err != nil {
				return current, fmt.Errorf("invalid tool result message: %w", err)
			}
			if r.compaction.warningActive && !injectedWarning {
				result = prependCompactionWarning(result, compactionWarningText, call.ID)
				if err := validateToolResultMessage(&result, call.ID); err != nil {
					return current, fmt.Errorf("invalid tool result message after compaction warning injection: %w", err)
				}
				injectedWarning = true
			}
			current = append(current, result)
		}
	}
}

func (r *Runtime) saveAndPrintPending(ctx context.Context, dialog gai.Dialog) (gai.Dialog, error) {
	if r.dialogSaver != nil {
		saved, err := saveDialog(ctx, r.dialogSaver, dialog)
		if err != nil {
			return dialog, fmt.Errorf("failed to save dialog: %w", err)
		}
		dialog = saved
	}
	for _, toolResultMsg := range trailingToolResults(dialog) {
		r.printToolResult(dialog, toolResultMsg)
	}
	return dialog, nil
}

func (r *Runtime) validateToolChoice(opts *gai.GenOpts) error {
	if opts == nil || opts.ToolChoice == "" || opts.ToolChoice == gai.ToolChoiceAuto || opts.ToolChoice == gai.ToolChoiceToolsRequired {
		return nil
	}
	if _, exists := r.tools[opts.ToolChoice]; !exists {
		return gai.InvalidToolChoiceErr(fmt.Sprintf("tool '%s' not found", opts.ToolChoice))
	}
	return nil
}

type toolCall struct {
	ID             string
	Name           string
	ParametersJSON json.RawMessage
	Block          gai.Block
}

func (r *Runtime) extractToolCalls(msg gai.Message) ([]toolCall, error) {
	calls := []toolCall{}
	for _, block := range msg.Blocks {
		if block.BlockType != gai.ToolCall {
			continue
		}
		var data struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
		}
		if block.Content == nil {
			return nil, fmt.Errorf("invalid tool call JSON: missing content")
		}
		if err := json.Unmarshal([]byte(block.Content.String()), &data); err != nil {
			return nil, fmt.Errorf("invalid tool call JSON: %w", err)
		}
		if data.Name == "" {
			return nil, fmt.Errorf("missing tool name")
		}
		if _, exists := r.tools[data.Name]; !exists {
			return nil, fmt.Errorf("tool '%s' not found", data.Name)
		}
		params := data.Parameters
		if len(params) == 0 || string(params) == "null" {
			params = json.RawMessage("{}")
		}
		calls = append(calls, toolCall{ID: block.ID, Name: data.Name, ParametersJSON: params, Block: block})
	}
	return calls, nil
}

func validateToolResultMessage(message *gai.Message, toolCallID string) error {
	if message.Role != gai.ToolResult {
		return fmt.Errorf("message must have ToolResult role, got: %v", message.Role)
	}
	if len(message.Blocks) == 0 {
		return fmt.Errorf("message must have at least one block")
	}
	for i, block := range message.Blocks {
		if block.ID != toolCallID {
			return fmt.Errorf("block %d has incorrect ID: expected %q, got %q", i, toolCallID, block.ID)
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

func (r *Runtime) updateCompactionWarning(metadata gai.Metadata) {
	if r.cfg.Compaction == nil || r.cfg.Compaction.TokenThreshold == 0 {
		return
	}
	metrics := extractTokenUsageMetrics(metadata)
	if !metrics.HasInputTokens && !metrics.HasOutputTokens {
		return
	}
	used := metrics.InputTokens + metrics.OutputTokens
	if uint(used) >= r.cfg.Compaction.TokenThreshold {
		r.compaction.warningActive = true
	}
}

func prependCompactionWarning(msg gai.Message, warning, toolCallID string) gai.Message {
	warningBlock := gai.TextBlock(warning)
	warningBlock.ID = toolCallID
	msg.Blocks = append([]gai.Block{warningBlock}, msg.Blocks...)
	return msg
}

func (r *Runtime) isCompactionTool(name string) bool {
	return r.cfg.Compaction != nil && r.cfg.Compaction.Tool.Name == name
}

func (r *Runtime) firstCompactionCall(calls []toolCall) (toolCall, bool) {
	for _, call := range calls {
		if r.isCompactionTool(call.Name) {
			return call, true
		}
	}
	return toolCall{}, false
}

func (r *Runtime) restartCompacted(ctx context.Context, current gai.Dialog, call toolCall) (gai.Dialog, error) {
	if r.cfg.Compaction == nil {
		return current, fmt.Errorf("compaction tool called but compaction is not configured")
	}
	if r.compaction.restarts >= r.cfg.Compaction.MaxCompactions {
		return current, fmt.Errorf("maximum compaction restarts exceeded")
	}

	previousLeafID := lastMessageID(current)
	var args map[string]any
	if err := json.Unmarshal(call.ParametersJSON, &args); err != nil {
		return current, fmt.Errorf("invalid compaction tool arguments: %w", err)
	}
	data := config.CompactionTemplateData{
		PreviousLeafID:     previousLeafID,
		Dialog:             current,
		ToolArguments:      args,
		ToolArgumentsJSON:  string(call.ParametersJSON),
		CompactionToolName: r.cfg.Compaction.Tool.Name,
	}
	var rendered bytes.Buffer
	if err := r.cfg.Compaction.InitialMessageTemplate.Execute(&rendered, data); err != nil {
		return current, fmt.Errorf("rendering compaction initial message: %w", err)
	}

	root := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(rendered.String())}}
	if previousLeafID != "" {
		root.ExtraFields = map[string]any{storage.MessageCompactionParentIDKey: previousLeafID}
	}
	r.compaction.restarts++
	r.compaction.warningActive = false
	_ = ctx
	return gai.Dialog{root}, nil
}

func lastMessageID(dialog gai.Dialog) string {
	for i := len(dialog) - 1; i >= 0; i-- {
		if id := GetMessageID(dialog[i]); id != "" {
			return id
		}
	}
	return ""
}
