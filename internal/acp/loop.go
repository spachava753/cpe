package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xacp"
	"github.com/spachava753/cpe/internal/acp/xctx"
	cpeagent "github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

const compactionWarningText = `[COMPACTION WARNING]
The conversation has exceeded the configured compaction threshold. Before continuing much further, call the compact_conversation tool with a compact but complete summary of the conversation state needed to continue. This warning will continue to appear until compaction is performed.`

const maxCompactionRetries = 3

const compactionSuccessText = "Conversation compacted successfully."

type sessionIDCtxKey struct{}

type compactionResetter interface {
	Reset()
}

type sessionUpdater interface {
	SessionUpdate(context.Context, *acp.SessionNotification) error
}

func withSessionID(ctx context.Context, sessionID acp.SessionId) context.Context {
	return context.WithValue(ctx, sessionIDCtxKey{}, sessionID)
}

// loop owns the acp full agentic loop for a prompt turn.
type loop struct {
	G     gai.ToolCallingGenerator
	Store *storage.Sqlite
	Cfg   config.Config

	// internal state
	toolCallbacks      map[string]gai.ToolCallback
	seenToolCallIDs    map[string]struct{}
	compactionRestarts int
	compactionRetries  int
	conn               sessionUpdater
}

// Register registers a tool with the provider model and stores its callback.
func (l *loop) Register(tool gai.Tool, callback gai.ToolCallback) error {
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

func (l *loop) validateToolChoice(opts *gai.GenOpts) error {
	if opts == nil || opts.ToolChoice == "" || opts.ToolChoice == gai.ToolChoiceAuto || opts.ToolChoice == gai.ToolChoiceToolsRequired {
		return nil
	}
	if _, exists := l.toolCallbacks[opts.ToolChoice]; !exists {
		return gai.InvalidToolChoiceErr(fmt.Sprintf("tool '%s' not found", opts.ToolChoice))
	}
	return nil
}

// effectiveGenOpts layers per-turn overrides (such as the ACP session's
// thinking level) over the resolved model profile generation parameters,
// so config fields like maxGenerationTokens apply to every Generate call.
func (l *loop) effectiveGenOpts(override *gai.GenOpts) *gai.GenOpts {
	isResponsesModel := strings.EqualFold(l.Cfg.Model.Type, cpeagent.ModelTypeResponses)
	if l.Cfg.GenerationParams == nil && override == nil && !isResponsesModel {
		return nil
	}
	merged := &gai.GenOpts{}
	config.MergeGenOpts(merged, l.Cfg.GenerationParams)
	config.MergeGenOpts(merged, override)
	if isResponsesModel {
		if merged.ExtraArgs != nil {
			merged.ExtraArgs = maps.Clone(merged.ExtraArgs)
		}
		cpeagent.ApplyResponsesThinkingSummary(merged)
	}
	return merged
}

// Generate runs the dialog until a terminal assistant response, nil-callback
// terminal tool, callback error, or compaction restart limit is reached.
//
// TODO: we need to add support for sending session updates for streaming generators for a more real-time experience
// TODO: acp clients, like editors like zed, might have unsaved changes, so generally speaking, it is preferable to use fs/read_text_file and fs/write_text_file tools where possible
// TODO: support unstable feature https://agentclientprotocol.com/rfds/diff-delete
// TODO: starlark_repl should display file edit diffs when practical; unlike text_edit, arbitrary host-backed code may touch many files
// TODO: expose model capability metadata in session updates so ACP clients can adapt UI affordances
func (l *loop) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
	current := append(gai.Dialog(nil), dialog...)
	l.compactionRetries = 0
	if l.seenToolCallIDs == nil {
		l.seenToolCallIDs = make(map[string]struct{})
		for _, msg := range current {
			for _, block := range msg.Blocks {
				if block.BlockType == gai.ToolCall && block.ID != "" {
					l.seenToolCallIDs[block.ID] = struct{}{}
				}
			}
		}
	}

	opts = l.effectiveGenOpts(opts)
	if err := l.validateToolChoice(opts); err != nil {
		return current, err
	}

	if l.Store == nil {
		panic("Store not set")
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
		newToolCallIDs := make(map[string]struct{})
		for _, block := range resp.Candidates[0].Blocks {
			if block.BlockType != gai.ToolCall {
				continue
			}
			if block.ID == "" {
				return current, errors.New("tool call ID cannot be empty")
			}
			if _, exists := l.seenToolCallIDs[block.ID]; exists {
				return current, fmt.Errorf("duplicate tool call ID %q", block.ID)
			}
			if _, exists := newToolCallIDs[block.ID]; exists {
				return current, fmt.Errorf("duplicate tool call ID %q", block.ID)
			}
			newToolCallIDs[block.ID] = struct{}{}
		}

		// save response
		l.attachAgentMetadata(&resp.Candidates[0], resp.UsageMetadata)
		current = append(current, resp.Candidates[0])
		current, err = l.save(ctx, current)
		if err != nil {
			return current, err
		}
		for id := range newToolCallIDs {
			l.seenToolCallIDs[id] = struct{}{}
		}

		for update := range xacp.MsgToSessionUpdate(resp.Candidates[0]) {
			if err := l.conn.SessionUpdate(ctx, &acp.SessionNotification{
				SessionID: sessionID,
				Update:    update,
			}); err != nil {
				return current, fmt.Errorf("send assistant session update: %w", err)
			}
		}
		update, ok, err := l.usageSessionUpdate(ctx, sessionID, resp.UsageMetadata)
		if err != nil {
			return current, err
		}
		if ok {
			if err := l.conn.SessionUpdate(ctx, &acp.SessionNotification{
				SessionID: sessionID,
				Update:    update,
			}); err != nil {
				return current, fmt.Errorf("send usage session update: %w", err)
			}
		}

		if resp.FinishReason != gai.ToolUse {
			return current, nil
		}

		// Persist compaction results on the old branch before linking a replacement root.
		var compactionResults []gai.Message
		var compactionRoot *gai.Message
		var compactionErr error
		current, compactionResults, compactionRoot, compactionErr = l.compact(current)
		current, err = l.save(ctx, current)
		if err != nil {
			return current, err
		}
		if compactionRoot != nil {
			previousLeafID := storage.GetMessageID(current[len(current)-1])
			if previousLeafID != "" {
				compactionRoot.ExtraFields = map[string]any{storage.MessageCompactionParentIDKey: previousLeafID}
			}
			replacement := gai.Dialog{*compactionRoot}
			replacement, err = l.save(ctx, replacement)
			if err != nil {
				// The successful result is already durable. Returning the old branch
				// would let agent.Prompt commit a false-success session head.
				panic(fmt.Errorf("persist compaction replacement root after successful result: %w", err))
			}
			current = replacement

			// The rebase is durable. Only now consume a restart and clear state that
			// must not cross the compaction boundary.
			l.compactionRestarts++
			l.compactionRetries = 0
			for _, callback := range l.toolCallbacks {
				if resetter, ok := callback.(compactionResetter); ok {
					resetter.Reset()
				}
			}
		}
		for _, result := range compactionResults {
			for update := range xacp.MsgToSessionUpdate(result) {
				if err := l.conn.SessionUpdate(ctx, &acp.SessionNotification{
					SessionID: sessionID,
					Update:    update,
				}); err != nil {
					return current, fmt.Errorf("send compaction tool result session update: %w", err)
				}
			}
		}
		if compactionErr != nil {
			return current, compactionErr
		}
		if compactionRoot != nil {
			for update := range xacp.MsgToSessionUpdate(current[0]) {
				if err := l.conn.SessionUpdate(ctx, &acp.SessionNotification{
					SessionID: sessionID,
					Update:    update,
				}); err != nil {
					return current, fmt.Errorf("send compaction session update: %w", err)
				}
			}
			continue
		}

		lastMsg := current[len(current)-1]
		executionMessageID := storage.GetMessageID(lastMsg)
		if executionMessageID == "" {
			return current, errors.New("persisted assistant message has no storage ID")
		}
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
			callbackCtx := xctx.WithToolCallId(ctx, acp.ToolCallId(block.ID))
			callbackCtx = xctx.WithExecutionMessageID(callbackCtx, executionMessageID)
			result, err := callback.Call(callbackCtx, params)
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

			previous := current
			current = append(current, result)
			if ctxErr := ctx.Err(); ctxErr != nil {
				saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				saved, saveErr := l.save(saveCtx, current)
				cancel()
				if saveErr != nil {
					return previous, fmt.Errorf("persist tool result after cancellation: %w", saveErr)
				}
				return saved, ctxErr
			}

			firstBlock = false
		}
	}
}

func (l *loop) save(ctx context.Context, dialog gai.Dialog) (gai.Dialog, error) {
	if l.Store == nil {
		return dialog, nil
	}
	idx := 0
	for saved, err := range l.Store.SaveDialog(ctx, slices.Values(dialog)) {
		if err != nil {
			return nil, err
		}
		dialog[idx] = saved
		idx++
	}
	return dialog, nil
}

func (l *loop) shouldInjectCompactionWarning(metadata gai.Metadata) bool {
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

func (l *loop) attachAgentMetadata(msg *gai.Message, metadata gai.Metadata) {
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

// compact handles a compact_conversation call in the latest assistant message.
// The returned dialog remains on the existing branch and includes any tool
// results that must be persisted. The message slice contains those same new
// results for ACP publication. On success, the replacement root is returned
// separately so Generate can persist the completed result, link the root to it,
// and only then switch branches and reset compaction-scoped state. Recoverable
// rejections return a nil error so the model can retry; terminal failures return
// both a failed result and an error.
func (l *loop) compact(current gai.Dialog) (gai.Dialog, []gai.Message, *gai.Message, error) {
	if l.Cfg.Compaction == nil {
		return current, nil, nil, nil
	}

	lastMsg := current[len(current)-1]
	toolCalls := make([]gai.Block, 0, len(lastMsg.Blocks))
	for _, block := range lastMsg.Blocks {
		if block.BlockType == gai.ToolCall {
			toolCalls = append(toolCalls, block)
		}
	}

	idx := slices.IndexFunc(toolCalls, func(block gai.Block) bool {
		var input gai.ToolCallInput
		if err := json.Unmarshal([]byte(block.Content.String()), &input); err != nil {
			panic(err)
		}
		return input.Name == l.Cfg.Compaction.Tool.Name
	})
	if idx == -1 {
		return current, nil, nil, nil
	}

	compactionBlock := toolCalls[idx]
	var compactionTool gai.ToolCallInput
	if err := json.Unmarshal([]byte(compactionBlock.Content.String()), &compactionTool); err != nil {
		panic(err)
	}
	toolError := func(block gai.Block, text string) gai.Message {
		result := gai.ToolResultMessage(block.ID, gai.TextBlock(text))
		result.ToolResultError = true
		return result
	}

	// A rejection is recoverable until the retry budget is exhausted. In both
	// cases, append results to the existing branch so they are saved and visible
	// to the model; only the terminal case also stops Generate.
	reject := func(results []gai.Message, reason string) (gai.Dialog, []gai.Message, *gai.Message, error) {
		if l.compactionRetries >= maxCompactionRetries {
			for i := range results {
				if results[i].Blocks[0].ID == compactionBlock.ID {
					results[i] = toolError(compactionBlock, fmt.Sprintf(
						"compact_conversation retry limit of %d exceeded. Last error: %s",
						maxCompactionRetries,
						reason,
					))
					break
				}
			}
			return append(current, results...), results, nil, fmt.Errorf("maximum compaction retries exceeded: %s", reason)
		}
		l.compactionRetries++
		return append(current, results...), results, nil, nil
	}

	// Compaction replaces the active dialog, so sibling calls cannot be executed
	// without losing their results. Reject every call and ask the model to retry
	// compaction by itself.
	if len(toolCalls) > 1 {
		reason := "compact_conversation must be the only tool call in an assistant response"
		results := make([]gai.Message, 0, len(toolCalls))
		for i, block := range toolCalls {
			text := "Tool call rejected because compact_conversation must be called without sibling tool calls."
			if i == idx {
				text = reason + ". Call compact_conversation again without sibling tool calls."
			}
			results = append(results, toolError(block, text))
		}
		return reject(results, reason)
	}

	if uint(l.compactionRestarts) >= l.Cfg.Compaction.MaxCompactions {
		result := toolError(compactionBlock, "compact_conversation cannot run because the maximum compaction restarts have been exceeded.")
		return append(current, result), []gai.Message{result}, nil, fmt.Errorf("maximum compaction restarts exceeded")
	}
	if l.Cfg.Compaction.InputSchema != nil {
		if err := l.Cfg.Compaction.InputSchema.Validate(compactionTool.Parameters); err != nil {
			reason := fmt.Sprintf("arguments do not match the input schema: %v", err)
			result := toolError(compactionBlock, "compact_conversation "+reason+". Call compact_conversation again with corrected arguments.")
			return reject([]gai.Message{result}, reason)
		}
	}

	// Render only after schema validation. This keeps payload shape errors, such
	// as ranging over a scalar, recoverable instead of surfacing as template
	// configuration failures.
	paramJson, err := json.Marshal(compactionTool.Parameters)
	if err != nil {
		panic(err)
	}
	data := config.CompactionTemplateData{
		Dialog:             current,
		ToolArguments:      compactionTool.Parameters,
		ToolArgumentsJSON:  string(paramJson),
		CompactionToolName: l.Cfg.Compaction.Tool.Name,
	}
	var rendered bytes.Buffer
	if err := l.Cfg.Compaction.InitialMessageTemplate.Execute(&rendered, data); err != nil {
		result := toolError(compactionBlock, fmt.Sprintf(
			"compact_conversation could not render the configured initial message: %v. This is a CPE configuration or runtime error; do not retry compaction.",
			err,
		))
		return append(current, result), []gai.Message{result}, nil, fmt.Errorf("rendering compaction initial message: %w", err)
	}

	// Keep the completed result on the old branch and return the rendered root
	// separately. Generate saves the result first and uses its persisted ID as the
	// replacement root's compaction parent, preserving completion during replay.
	root := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock(rendered.String())}}
	result := gai.ToolResultMessage(compactionBlock.ID, gai.TextBlock(compactionSuccessText))
	return append(current, result), []gai.Message{result}, &root, nil
}

// usageSessionUpdate persists the cost of a single generation into the ACP
// session and builds the usage session update reporting context size and the
// session's cumulative cost. Cost is persisted whenever the model pricing
// allows calculating it, even if no usage update can be built (for example,
// when the model has no configured context window).
func (l *loop) usageSessionUpdate(ctx context.Context, sessionID acp.SessionId, metadata gai.Metadata) (acp.SessionUpdate, bool, error) {
	var cost *acp.Cost
	if generationCost, ok := calculateUsageCostUSD(metadata, l.Cfg.Model); ok {
		total, err := l.Store.AddACPSessionCost(ctx, sessionID, generationCost)
		if err != nil {
			return acp.SessionUpdate{}, false, fmt.Errorf("persist session cost: %w", err)
		}
		cost = &acp.Cost{
			Amount:   total,
			Currency: "USD",
		}
	}

	if l.Cfg.Model.ContextWindow == 0 {
		return acp.SessionUpdate{}, false, nil
	}

	used, ok := contextUsedTokens(metadata)
	if !ok {
		return acp.SessionUpdate{}, false, nil
	}

	update := acp.UsageUpdateSessionUpdate(uint64(used), uint64(l.Cfg.Model.ContextWindow))
	update.Cost = cost
	return update, true, nil
}

func contextUsedTokens(metadata gai.Metadata) (int, bool) {
	inputTokens, hasInputTokens := gai.InputTokens(metadata)
	outputTokens, hasOutputTokens := gai.OutputTokens(metadata)
	if !hasInputTokens && !hasOutputTokens {
		return 0, false
	}

	return inputTokens + outputTokens, true
}

func calculateUsageCostUSD(metadata gai.Metadata, model config.Model) (float64, bool) {
	total := 0.0
	hasAnyCost := false

	if inputTokens, ok := gai.InputTokens(metadata); ok {
		billableInputTokens := inputTokens
		if cacheRead, ok := gai.CacheReadTokens(metadata); ok {
			billableInputTokens -= cacheRead
		}
		if cacheWrite, ok := gai.CacheWriteTokens(metadata); ok && model.CacheWriteCostPerMillion != nil {
			billableInputTokens -= cacheWrite
		}
		if billableInputTokens < 0 {
			billableInputTokens = 0
		}
		if cost, ok := calculateComponentCost(billableInputTokens, model.InputCostPerMillion); ok {
			total += cost
			hasAnyCost = true
		}
	}
	if outputTokens, ok := gai.OutputTokens(metadata); ok {
		if cost, ok := calculateComponentCost(outputTokens, model.OutputCostPerMillion); ok {
			total += cost
			hasAnyCost = true
		}
	}
	if cacheRead, ok := gai.CacheReadTokens(metadata); ok {
		if cost, ok := calculateComponentCost(cacheRead, model.CacheReadCostPerMillion); ok {
			total += cost
			hasAnyCost = true
		}
	}
	if cacheWrite, ok := gai.CacheWriteTokens(metadata); ok {
		if cost, ok := calculateComponentCost(cacheWrite, model.CacheWriteCostPerMillion); ok {
			total += cost
			hasAnyCost = true
		}
	}

	return total, hasAnyCost
}

func calculateComponentCost(tokens int, costPerMillion *float64) (float64, bool) {
	if costPerMillion == nil {
		return 0, false
	}
	return (float64(tokens) * *costPerMillion) / 1_000_000, true
}
