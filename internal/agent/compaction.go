package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/ports"
	"github.com/spachava753/cpe/internal/storage"
)

const (
	compactConversationToolName = "compact_conversation"
	compactionWarningHeader     = "[COMPACTION WARNING]"
)

// compactionRunState tracks per-run context utilization for one logical
// generation attempt. It is created fresh for each top-level run or restart and
// is used both to determine whether the compaction threshold was crossed and to
// render any warning text injected into tool results.
type compactionRunState struct {
	contextWindow     uint32
	threshold         float64
	latestUsedTokens  int
	latestUtilization float64
	thresholdExceeded bool
}

// compactionRunStateContextKey is the private context key used to thread the
// active compaction run state through nested generator and tool-callback calls.
type compactionRunStateContextKey struct{}

// toolCallbackFunc adapts a function to gai.ToolCallback so helpers in this file
// can build small callback wrappers without introducing extra named types.
type toolCallbackFunc func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error)

// Call implements gai.ToolCallback.
func (f toolCallbackFunc) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	return f(ctx, parametersJSON, toolCallID)
}

// compactionUsageTrackingGenerator is the low-level generator wrapper that
// records usage metadata into the active compaction run state after each model
// call. It does not decide when to compact; it only observes usage.
type compactionUsageTrackingGenerator struct {
	gai.GeneratorWrapper
}

// compactionAwareGenerator is the outer dialog-level generator that detects a
// compact_conversation tool call in the returned dialog and restarts generation
// into a fresh compacted branch when appropriate.
type compactionAwareGenerator struct {
	base          ports.Generator
	registrar     ports.ToolRegistrar
	cfg           *config.ResolvedCompactionConfig
	contextWindow uint32
}

// compactionRuntime centralizes the compaction-specific assembly steps that are
// applied from generator.go when compaction is enabled.
type compactionRuntime struct {
	cfg           *config.ResolvedCompactionConfig
	contextWindow uint32
}

// newCompactionRuntime returns the compaction assembly helper for the resolved
// config, or nil when compaction is disabled.
func newCompactionRuntime(cfg *config.Config) *compactionRuntime {
	if cfg == nil || cfg.Compaction == nil || !cfg.Compaction.Enabled {
		return nil
	}
	return &compactionRuntime{
		cfg:           cfg.Compaction,
		contextWindow: cfg.Model.ContextWindow,
	}
}

// wrapToolCapableGenerator adds usage observation to the low-level generator so
// compaction thresholds can be evaluated after each model call.
func (r *compactionRuntime) wrapToolCapableGenerator(gen gai.ToolCapableGenerator) gai.ToolCapableGenerator {
	return newCompactionUsageTrackingGenerator(gen)
}

// wrapToolCallbackWrapper composes any existing callback wrapper with the
// compaction warning injector so tool results can carry threshold warnings.
func (r *compactionRuntime) wrapToolCallbackWrapper(base ToolCallbackWrapper) ToolCallbackWrapper {
	return func(toolName string, callback gai.ToolCallback) gai.ToolCallback {
		wrapped := callback
		if base != nil {
			wrapped = base(toolName, wrapped)
		}
		return wrapToolCallbackWithCompactionWarning(wrapped)
	}
}

// registerTool exposes the compact_conversation tool with a nil callback so a
// tool call terminates the current run and can be interpreted by the dialog-
// level compaction wrapper.
func (r *compactionRuntime) registerTool(registrar ports.ToolRegistrar) error {
	if registrar == nil {
		return fmt.Errorf("generator does not support tool registration")
	}
	return registrar.Register(compactionTool(r.cfg), nil)
}

// wrapGenerator adds the dialog-level restart behavior that turns a
// compact_conversation tool call into a fresh compacted branch.
func (r *compactionRuntime) wrapGenerator(base ports.Generator, registrar ports.ToolRegistrar) ports.Generator {
	return newCompactionAwareGenerator(base, registrar, r.cfg, r.contextWindow)
}

// withCompactionRunState attaches the active run state to the context so inner
// generator wrappers and tool callbacks can observe the same compaction state.
func withCompactionRunState(ctx context.Context, state *compactionRunState) context.Context {
	return context.WithValue(ctx, compactionRunStateContextKey{}, state)
}

// compactionRunStateFromContext retrieves the active compaction run state from
// the context when one has been attached.
func compactionRunStateFromContext(ctx context.Context) (*compactionRunState, bool) {
	state, ok := ctx.Value(compactionRunStateContextKey{}).(*compactionRunState)
	return state, ok && state != nil
}

// observeUsage records the highest observed context utilization for the run and
// marks the run as threshold-exceeded once the configured threshold is crossed.
func (s *compactionRunState) observeUsage(metadata gai.Metadata) {
	usedTokens, utilization, ok := calculateCompactionUtilization(extractTokenUsageMetrics(metadata), s.contextWindow)
	if !ok {
		return
	}
	if utilization > s.latestUtilization {
		s.latestUtilization = utilization
		s.latestUsedTokens = usedTokens
	}
	if utilization >= s.threshold {
		s.thresholdExceeded = true
	}
}

// warningText returns the user-visible warning that is prepended to tool
// results after the compaction threshold has been exceeded.
func (s *compactionRunState) warningText() string {
	if s == nil || !s.thresholdExceeded || s.contextWindow == 0 {
		return ""
	}
	return fmt.Sprintf(
		"%s\nObserved context utilization: %.2f%% (%d / %d tokens). Configured threshold: %.2f%%. Finish the current subtask, then call %s.",
		compactionWarningHeader,
		s.latestUtilization*100,
		s.latestUsedTokens,
		s.contextWindow,
		s.threshold*100,
		compactConversationToolName,
	)
}

// calculateCompactionUtilization converts token usage metrics into absolute and
// fractional context usage for compaction threshold checks.
func calculateCompactionUtilization(metrics tokenUsageMetrics, contextWindow uint32) (int, float64, bool) {
	if contextWindow == 0 {
		return 0, 0, false
	}
	used := 0
	if metrics.HasInputTokens {
		used += metrics.InputTokens
	}
	if metrics.HasOutputTokens {
		used += metrics.OutputTokens
	}
	if used == 0 {
		return 0, 0, false
	}
	return used, float64(used) / float64(contextWindow), true
}

// newCompactionUsageTrackingGenerator wraps a tool-capable generator so each
// successful model response updates the active compaction run state.
func newCompactionUsageTrackingGenerator(g gai.ToolCapableGenerator) *compactionUsageTrackingGenerator {
	return &compactionUsageTrackingGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
	}
}

// Generate delegates to the wrapped generator and records usage metadata on the
// active compaction run state when a response is returned successfully.
func (g *compactionUsageTrackingGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	resp, err := g.GeneratorWrapper.Generate(ctx, dialog, opts)
	if err != nil {
		return resp, err
	}
	if state, ok := compactionRunStateFromContext(ctx); ok {
		state.observeUsage(resp.UsageMetadata)
	}
	return resp, nil
}

// newCompactionAwareGenerator constructs the dialog-level wrapper that restarts
// generation into compacted branches when the compaction tool is called.
func newCompactionAwareGenerator(base ports.Generator, registrar ports.ToolRegistrar, cfg *config.ResolvedCompactionConfig, contextWindow uint32) *compactionAwareGenerator {
	return &compactionAwareGenerator{
		base:          base,
		registrar:     registrar,
		cfg:           cfg,
		contextWindow: contextWindow,
	}
}

// Generate runs the wrapped dialog generator, detects any new
// compact_conversation tool call, and if found restarts generation from a fresh
// compacted root message until the run completes or the restart cap is reached.
func (g *compactionAwareGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	currentDialog := dialog
	for compactions := 0; ; compactions++ {
		runState := &compactionRunState{
			contextWindow: g.contextWindow,
			threshold:     g.cfg.AutoTriggerThreshold,
		}

		resultDialog, err := g.base.Generate(withCompactionRunState(ctx, runState), currentDialog, optsGen)
		if err != nil {
			return resultDialog, err
		}

		toolInput, ok, err := extractCompactionToolInput(resultDialog, len(currentDialog))
		if err != nil {
			return resultDialog, err
		}
		if !ok {
			return resultDialog, nil
		}
		if compactions >= g.cfg.MaxAutoCompactionRestarts {
			return resultDialog, fmt.Errorf("compaction restart limit exceeded after %d compactions", g.cfg.MaxAutoCompactionRestarts)
		}

		compactedDialog, err := g.buildCompactedDialog(resultDialog, toolInput)
		if err != nil {
			return resultDialog, err
		}
		currentDialog = compactedDialog
	}
}

// Register forwards tool registration to the underlying registrar so the
// compaction-aware wrapper still satisfies ports.ToolRegistrar.
func (g *compactionAwareGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if g.registrar == nil {
		return fmt.Errorf("generator does not support tool registration")
	}
	return g.registrar.Register(tool, callback)
}

// buildCompactedDialog renders the configured compacted root message and links
// it back to the previous branch tip for persistence and lineage tracking.
func (g *compactionAwareGenerator) buildCompactedDialog(previousDialog gai.Dialog, toolInput map[string]any) (gai.Dialog, error) {
	originalUserMessage, err := firstUserMessageText(previousDialog)
	if err != nil {
		return nil, err
	}

	rendered, err := g.cfg.RenderInitialMessage(config.CompactionTemplateData{
		OriginalUserMessage: originalUserMessage,
		ToolInput:           toolInput,
	})
	if err != nil {
		return nil, err
	}

	root := gai.Message{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock(rendered)},
	}
	if parentID := GetMessageID(previousDialog[len(previousDialog)-1]); parentID != "" {
		root.ExtraFields = map[string]any{storage.MessageCompactionParentIDKey: parentID}
	}

	return gai.Dialog{root}, nil
}

// compactionTool builds the compact_conversation tool definition from the
// resolved compaction config.
func compactionTool(cfg *config.ResolvedCompactionConfig) gai.Tool {
	return gai.Tool{
		Name:        compactConversationToolName,
		Description: cfg.ToolDescription,
		InputSchema: cfg.InputSchema,
	}
}

// wrapToolCallbackWithCompactionWarning prepends the current compaction warning
// to successful tool results once the run has exceeded the configured context
// threshold.
func wrapToolCallbackWithCompactionWarning(callback gai.ToolCallback) gai.ToolCallback {
	if callback == nil {
		return nil
	}
	return toolCallbackFunc(func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
		resultMessage, err := callback.Call(ctx, parametersJSON, toolCallID)
		if err != nil {
			return resultMessage, err
		}
		state, ok := compactionRunStateFromContext(ctx)
		if !ok {
			return resultMessage, nil
		}
		warningText := state.warningText()
		if warningText == "" {
			return resultMessage, nil
		}
		return injectCompactionWarning(resultMessage, toolCallID, warningText), nil
	})
}

// injectCompactionWarning prepends the warning block to a tool-result message
// while preserving the tool call ID association.
func injectCompactionWarning(message gai.Message, toolCallID, warningText string) gai.Message {
	if len(message.Blocks) == 0 || warningText == "" {
		return message
	}
	warningBlock := gai.TextBlock(warningText)
	warningBlock.ID = toolCallID
	message.Blocks = append([]gai.Block{warningBlock}, message.Blocks...)
	return message
}

// extractCompactionToolInput finds the most recent compact_conversation tool
// call at or after startIndex and returns its parameters.
func extractCompactionToolInput(dialog gai.Dialog, startIndex int) (map[string]any, bool, error) {
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > len(dialog) {
		startIndex = len(dialog)
	}
	for i := len(dialog) - 1; i >= startIndex; i-- {
		msg := dialog[i]
		if msg.Role != gai.Assistant {
			continue
		}
		for j := len(msg.Blocks) - 1; j >= 0; j-- {
			block := msg.Blocks[j]
			if block.BlockType != gai.ToolCall || block.ModalityType != gai.Text {
				continue
			}
			var toolCall gai.ToolCallInput
			if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err != nil {
				continue
			}
			if toolCall.Name != compactConversationToolName {
				continue
			}
			if toolCall.Parameters == nil {
				toolCall.Parameters = map[string]any{}
			}
			return toolCall.Parameters, true, nil
		}
	}
	return nil, false, nil
}

// firstUserMessageText returns the text representation of the first user
// message in the active branch, which seeds the compacted root prompt.
func firstUserMessageText(dialog gai.Dialog) (string, error) {
	for _, msg := range dialog {
		if msg.Role != gai.User {
			continue
		}
		return messageTextContent(msg), nil
	}
	return "", fmt.Errorf("compaction requires a user message in the active branch")
}

// messageTextContent renders a message into plain text, preserving text blocks
// directly and representing non-text blocks as MIME placeholders.
func messageTextContent(msg gai.Message) string {
	parts := make([]string, 0, len(msg.Blocks))
	for _, block := range msg.Blocks {
		if block.ModalityType == gai.Text {
			parts = append(parts, block.Content.String())
			continue
		}
		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = block.ModalityType.String()
		}
		parts = append(parts, fmt.Sprintf("[%s content]", mimeType))
	}
	return strings.Join(parts, "\n\n")
}
