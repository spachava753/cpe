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

type compactionRunState struct {
	contextWindow     uint32
	threshold         float64
	latestUsedTokens  int
	latestUtilization float64
	thresholdExceeded bool
}

type compactionRunStateContextKey struct{}

type toolCallbackFunc func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error)

func (f toolCallbackFunc) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	return f(ctx, parametersJSON, toolCallID)
}

type compactionUsageTrackingGenerator struct {
	gai.GeneratorWrapper
}

type compactionAwareGenerator struct {
	base          ports.Generator
	registrar     ports.ToolRegistrar
	cfg           *config.ResolvedCompactionConfig
	contextWindow uint32
}

func newCompactionRunState(contextWindow uint32, threshold float64) *compactionRunState {
	return &compactionRunState{
		contextWindow: contextWindow,
		threshold:     threshold,
	}
}

func withCompactionRunState(ctx context.Context, state *compactionRunState) context.Context {
	return context.WithValue(ctx, compactionRunStateContextKey{}, state)
}

func compactionRunStateFromContext(ctx context.Context) (*compactionRunState, bool) {
	state, ok := ctx.Value(compactionRunStateContextKey{}).(*compactionRunState)
	return state, ok && state != nil
}

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

func newCompactionUsageTrackingGenerator(g gai.ToolCapableGenerator) *compactionUsageTrackingGenerator {
	return &compactionUsageTrackingGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
	}
}

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

func newCompactionAwareGenerator(base ports.Generator, registrar ports.ToolRegistrar, cfg *config.ResolvedCompactionConfig, contextWindow uint32) *compactionAwareGenerator {
	return &compactionAwareGenerator{
		base:          base,
		registrar:     registrar,
		cfg:           cfg,
		contextWindow: contextWindow,
	}
}

func (g *compactionAwareGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	currentDialog := dialog
	for compactions := 0; ; compactions++ {
		runState := newCompactionRunState(g.contextWindow, g.cfg.AutoTriggerThreshold)
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

func (g *compactionAwareGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if g.registrar == nil {
		return fmt.Errorf("generator does not support tool registration")
	}
	return g.registrar.Register(tool, callback)
}

func (g *compactionAwareGenerator) Inner() ports.Generator {
	return g.base
}

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

func compactionTool(cfg *config.ResolvedCompactionConfig) gai.Tool {
	return gai.Tool{
		Name:        compactConversationToolName,
		Description: cfg.ToolDescription,
		InputSchema: cfg.InputSchema,
	}
}

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

func injectCompactionWarning(message gai.Message, toolCallID, warningText string) gai.Message {
	if len(message.Blocks) == 0 || warningText == "" {
		return message
	}
	warningBlock := gai.TextBlock(warningText)
	warningBlock.ID = toolCallID
	message.Blocks = append([]gai.Block{warningBlock}, message.Blocks...)
	return message
}

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

func firstUserMessageText(dialog gai.Dialog) (string, error) {
	for _, msg := range dialog {
		if msg.Role != gai.User {
			continue
		}
		return messageTextContent(msg), nil
	}
	return "", fmt.Errorf("compaction requires a user message in the active branch")
}

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
