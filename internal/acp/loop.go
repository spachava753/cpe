package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

const compactionWarningText = `[COMPACTION WARNING]
The conversation has exceeded the configured compaction threshold. Before continuing much further, call the compact_conversation tool with a compact but complete summary of the conversation state needed to continue. This warning will continue to appear until compaction is performed.`

type sessionIDCtxKey struct{}

func withSessionID(ctx context.Context, sessionID acp.SessionId) context.Context {
	return context.WithValue(ctx, sessionIDCtxKey{}, sessionID)
}

// Loop owns the acp full agentic loop for a prompt turn.
type Loop struct {
	G           gai.ToolCallingGenerator
	DialogSaver storage.DialogSaver
	Cfg         config.Config

	// internal state
	toolCallbacks      map[string]gai.ToolCallback
	compactionRestarts int
	conn               *acp.AgentSideConnection
}

// Register registers a tool with the provider model and stores its callback.
func (l *Loop) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if l.toolCallbacks == nil {
		l.toolCallbacks = make(map[string]gai.ToolCallback)
	}
	if tool.Name == "" {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool name cannot be empty")}
	}
	if tool.Name == gai.ToolChoiceAuto || tool.Name == gai.ToolChoiceToolsRequired {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool name is reserved")}
	}
	if _, exists := l.toolCallbacks[tool.Name]; exists {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("tool already registered")}
	}
	if err := l.G.Register(tool); err != nil {
		return err
	}
	l.toolCallbacks[tool.Name] = callback
	return nil
}

func (l *Loop) validateToolChoice(opts *gai.GenOpts) error {
	if opts == nil || opts.ToolChoice == "" || opts.ToolChoice == gai.ToolChoiceAuto || opts.ToolChoice == gai.ToolChoiceToolsRequired {
		return nil
	}
	if _, exists := l.toolCallbacks[opts.ToolChoice]; !exists {
		return gai.InvalidToolChoiceErr(fmt.Sprintf("tool '%s' not found", opts.ToolChoice))
	}
	return nil
}

// Generate runs the dialog until a terminal assistant response, nil-callback
// terminal tool, callback error, or compaction restart limit is reached.
//
// TODO: we need to add support for sending session updates for streaming generators for a more real-time experience
// TODO: acp clients, like editors like zed, might have unsaved changes, so generally speaking, it is preferable to use fs/read_text_file and fs/write_text_file tools where possible
// TODO: text_edit tool should display file edit diff, see https://agentclientprotocol.com/protocol/tool-calls#diffs
// TODO: support unstable feature https://agentclientprotocol.com/rfds/diff-delete
// TODO: execute_go_code tool should display file edit diff, see https://agentclientprotocol.com/protocol/tool-calls#diffs, which would mean we would need to capture before and after we run execute go code tool, more complicated than text_edit
// TODO: displaying the live output of the execute go code tool would be valuable, available in https://agentclientprotocol.com/protocol/terminals#embedding-in-tool-calls
// TODO: we should add support for unstable feature https://agentclientprotocol.com/rfds/session-usage
// TODO: we should have support MCP config passed
// TODO: expose model capability metadata in session updates so ACP clients can adapt UI affordances
// TODO: map ACP cancellation requests onto in-flight generator and tool execution contexts
func (l *Loop) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
	current := append(gai.Dialog(nil), dialog...)

	if err := l.validateToolChoice(opts); err != nil {
		return current, err
	}

	if l.DialogSaver == nil {
		panic("DialogSaver not set")
	}

	sessionID, ok := ctx.Value(sessionIDCtxKey{}).(acp.SessionId)
	if !ok || sessionID == "" {
		return current, errors.New("missing ACP session id")
	}
	if l.conn == nil {
		return current, errors.New("missing ACP session connection")
	}

	for {
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		default:
		}

		var err error
		current, err = l.save(ctx, current)
		if err != nil {
			return current, err
		}

		resp, err := l.G.Generate(ctx, current, opts)
		if err != nil {
			return current, err
		}
		if len(resp.Candidates) != 1 {
			return current, fmt.Errorf("expected exactly one candidate in response, got: %d", len(resp.Candidates))
		}
		if resp.Candidates[0].Role != gai.Assistant {
			return current, fmt.Errorf("expected assistant role in response, got: %v", resp.Candidates[0].Role)
		}

		// save response
		l.attachAgentMetadata(&resp.Candidates[0], resp.UsageMetadata)
		current = append(current, resp.Candidates[0])
		current, err = l.save(ctx, current)
		if err != nil {
			return current, err
		}

		for update := range msgToSessionUpdate(resp.Candidates[0]) {
			if err := l.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: sessionID,
				Update:    update,
			}); err != nil {
				return current, fmt.Errorf("send assistant session update: %w", err)
			}
		}

		if resp.FinishReason != gai.ToolUse {
			return current, nil
		}

		// compact conversation
		current, err = l.compact(current)
		if err != nil {
			return current, err
		}
		current, err = l.save(ctx, current)
		if err != nil {
			return current, err
		}

		// if compacted, len of dialog will be 1
		if len(current) == 1 {
			for update := range msgToSessionUpdate(current[0]) {
				if err := l.conn.SessionUpdate(ctx, acp.SessionNotification{
					SessionId: sessionID,
					Update:    update,
				}); err != nil {
					return current, fmt.Errorf("send compaction session update: %w", err)
				}
			}
			continue
		}

		lastMsg := current[len(current)-1]

		firstBlock := true
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
			if _, exists := l.toolCallbacks[tc.Name]; !exists {
				return current, fmt.Errorf("tool '%s' not found", tc.Name)
			}
			params := tc.Parameters
			if params == nil {
				params = make(map[string]any)
			}

			callback := l.toolCallbacks[tc.Name]
			// TODO: what happens when there are a mix of nil tool callback and some non-nil? Should we even allow nil callback?
			if callback == nil {
				return current, nil
			}
			result, err := callback.Call(ctx, params)
			if err != nil {
				return current, err
			}
			if firstBlock && l.shouldInjectCompactionWarning(resp.UsageMetadata) {
				warningBlock := gai.TextBlock(compactionWarningText)
				warningBlock.ID = block.ID
				result.Blocks = append([]gai.Block{warningBlock}, result.Blocks...)
			}

			// ensure that all of the blocks in the tool result have the associated tool call block id
			for i := range result.Blocks {
				result.Blocks[i].ID = block.ID
			}

			current = append(current, result)

			for update := range msgToSessionUpdate(result) {
				if err := l.conn.SessionUpdate(ctx, acp.SessionNotification{
					SessionId: sessionID,
					Update:    update,
				}); err != nil {
					return current, fmt.Errorf("send tool result session update: %w", err)
				}
			}

			firstBlock = false
		}
	}
}

func (l *Loop) save(ctx context.Context, dialog gai.Dialog) (gai.Dialog, error) {
	if l.DialogSaver == nil {
		return dialog, nil
	}
	idx := 0
	for saved, err := range l.DialogSaver.SaveDialog(ctx, slices.Values(dialog)) {
		if err != nil {
			return nil, err
		}
		dialog[idx] = saved
		idx++
	}
	return dialog, nil
}

func (l *Loop) shouldInjectCompactionWarning(metadata gai.Metadata) bool {
	if l.Cfg.Compaction == nil || l.Cfg.Compaction.TokenThreshold == 0 {
		return false
	}

	inputTokens, hasInputTokens := gai.InputTokens(metadata)
	outputTokens, hasOutputTokens := gai.OutputTokens(metadata)
	if !hasInputTokens && !hasOutputTokens {
		return false
	}
	return uint(inputTokens+outputTokens) >= l.Cfg.Compaction.TokenThreshold
}

func (l *Loop) attachAgentMetadata(msg *gai.Message, metadata gai.Metadata) {
	if msg.ExtraFields == nil {
		msg.ExtraFields = make(map[string]any)
	}

	if l.Cfg.Model.Ref != "" {
		msg.ExtraFields[storage.AgentMetadataModelRefKey] = l.Cfg.Model.Ref
	}
	if l.Cfg.Model.ID != "" {
		msg.ExtraFields[storage.AgentMetadataModelIDKey] = l.Cfg.Model.ID
	}
	if l.Cfg.Model.Type != "" {
		msg.ExtraFields[storage.AgentMetadataModelTypeKey] = l.Cfg.Model.Type
	}
	if l.Cfg.Model.DisplayName != "" {
		msg.ExtraFields[storage.AgentMetadataModelDisplayNameKey] = l.Cfg.Model.DisplayName
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

func (l *Loop) compact(current gai.Dialog) (gai.Dialog, error) {
	if l.Cfg.Compaction == nil {
		return current, nil
	}

	lastMsg := current[len(current)-1]

	idx := slices.IndexFunc(lastMsg.Blocks, func(b gai.Block) bool {
		if l.Cfg.Compaction == nil {
			return false
		}

		if b.BlockType != gai.ToolCall {
			return false
		}

		var tci gai.ToolCallInput
		if err := json.Unmarshal([]byte(b.Content.String()), &tci); err != nil {
			panic(err)
		}

		return tci.Name == l.Cfg.Compaction.Tool.Name
	})
	if idx == -1 {
		return current, nil
	}

	var compactionTool gai.ToolCallInput
	if err := json.Unmarshal([]byte(lastMsg.Blocks[idx].Content.String()), &compactionTool); err != nil {
		panic(err)
	}

	if uint(l.compactionRestarts) >= l.Cfg.Compaction.MaxCompactions {
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
		CompactionToolName: l.Cfg.Compaction.Tool.Name,
	}
	var rendered bytes.Buffer
	if err := l.Cfg.Compaction.InitialMessageTemplate.Execute(&rendered, data); err != nil {
		return current, fmt.Errorf("rendering compaction initial message: %w", err)
	}

	root := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(rendered.String())}}
	if previousLeafID != "" {
		root.ExtraFields = map[string]any{storage.MessageCompactionParentIDKey: previousLeafID}
	}
	l.compactionRestarts++
	return gai.Dialog{root}, nil
}
