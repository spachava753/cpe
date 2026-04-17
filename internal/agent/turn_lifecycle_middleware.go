package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/ports"
	"github.com/spachava753/cpe/internal/render"
	"github.com/spachava753/cpe/internal/storage"
)

const (
	maxToolResultLines = 20
	unknownToolName    = "unknown"
)

// TurnLifecycleMiddleware wraps one logical generator turn with the side effects
// that must happen in a specific order:
//   - save the incoming dialog so message IDs exist for printers
//   - print trailing tool results once per logical turn
//   - call the inner generator (which may itself retry)
//   - save the assistant response and propagate its message ID
//   - print the assistant response and message ID
//   - print token usage and estimated cost summary
//
// These steps are coupled and intentionally live in one middleware so their
// ordering is explicit in code rather than distributed across separate wrappers.
type TurnLifecycleMiddleware struct {
	gai.GeneratorWrapper
	dialogSaver             storage.DialogSaver
	contentRenderer         ports.Renderer
	thinkingRenderer        ports.Renderer
	toolCallRenderer        ports.Renderer
	metadataRenderer        ports.Renderer
	stdout                  io.Writer
	stderr                  io.Writer
	model                   config.Model
	disableResponsePrinting bool
	cumulativeCostUSD       float64
}

// NewTurnLifecycleMiddleware creates a middleware that performs saving,
// printing, and usage reporting for one logical generator turn.
func NewTurnLifecycleMiddleware(
	wrapped gai.Generator,
	model config.Model,
	dialogSaver storage.DialogSaver,
	stdout io.Writer,
	stderr io.Writer,
	disableResponsePrinting bool,
) *TurnLifecycleMiddleware {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	middleware := &TurnLifecycleMiddleware{
		GeneratorWrapper:        gai.GeneratorWrapper{Inner: wrapped},
		dialogSaver:             dialogSaver,
		metadataRenderer:        render.NewRendererForWriter(stderr),
		stdout:                  stdout,
		stderr:                  stderr,
		model:                   model,
		disableResponsePrinting: disableResponsePrinting,
	}
	if !disableResponsePrinting {
		renderers := render.NewTurnLifecycleRenderersForWriters(stdout, stderr)
		middleware.contentRenderer = renderers.Content
		middleware.thinkingRenderer = renderers.Thinking
		middleware.toolCallRenderer = renderers.ToolCall
	}
	return middleware
}

// WithTurnLifecycle returns a WrapperFunc for use with gai.Wrap.
func WithTurnLifecycle(
	model config.Model,
	dialogSaver storage.DialogSaver,
	stdout io.Writer,
	stderr io.Writer,
	disableResponsePrinting bool,
) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewTurnLifecycleMiddleware(g, model, dialogSaver, stdout, stderr, disableResponsePrinting)
	}
}

func (m *TurnLifecycleMiddleware) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	if m.dialogSaver != nil {
		savedDialog, err := saveDialog(ctx, m.dialogSaver, dialog)
		if err != nil {
			return gai.Response{}, fmt.Errorf("failed to save dialog: %w", err)
		}
		dialog = savedDialog
	}

	for _, toolResultMsg := range trailingToolResults(dialog) {
		m.printToolResult(dialog, toolResultMsg)
	}

	resp, err := m.GeneratorWrapper.Generate(ctx, dialog, options)
	if err == nil && m.dialogSaver != nil {
		savedResp, saveErr := saveAssistantResponse(ctx, m.dialogSaver, dialog, resp)
		if saveErr != nil {
			return gai.Response{}, fmt.Errorf("failed to save assistant message: %w", saveErr)
		}
		resp = savedResp
	}

	if !m.disableResponsePrinting {
		m.printResponse(resp)
	}
	m.printTokenUsage(resp.UsageMetadata)

	return resp, err
}

func saveDialog(ctx context.Context, saver storage.DialogSaver, dialog gai.Dialog) (gai.Dialog, error) {
	idx := 0
	for saved, err := range saver.SaveDialog(ctx, slices.Values(dialog)) {
		if err != nil {
			return nil, err
		}
		dialog[idx] = saved
		idx++
	}
	return dialog, nil
}

func saveAssistantResponse(ctx context.Context, saver storage.DialogSaver, dialog gai.Dialog, resp gai.Response) (gai.Response, error) {
	if len(resp.Candidates) == 0 {
		return resp, nil
	}

	fullDialog := append(dialog, resp.Candidates[0])
	idx := 0
	for saved, err := range saver.SaveDialog(ctx, slices.Values(fullDialog)) {
		if err != nil {
			return gai.Response{}, err
		}
		fullDialog[idx] = saved
		idx++
	}
	resp.Candidates[0] = fullDialog[len(fullDialog)-1]
	return resp, nil
}

func (m *TurnLifecycleMiddleware) renderContent(content string) string {
	rendered, err := m.contentRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (m *TurnLifecycleMiddleware) renderThinking(content string, reasoningType any) string {
	if reasoningType == "reasoning.encrypted" {
		content = "[Reasoning content is encrypted]\n"
	}
	rendered, err := m.thinkingRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (m *TurnLifecycleMiddleware) renderToolCall(content string) string {
	if input, ok := codemode.ParseToolCall(content); ok {
		result := codemode.FormatToolCallMarkdown(input)
		if rendered, err := m.toolCallRenderer.Render(result); err == nil {
			return rendered
		}
		return result
	}

	result, ok := codemode.FormatGenericToolCallMarkdown(content)
	if !ok {
		return content
	}
	if rendered, err := m.toolCallRenderer.Render(result); err == nil {
		return rendered
	}
	return content
}

type blockContent struct {
	blockType string
	content   string
}

func (m *TurnLifecycleMiddleware) printResponse(response gai.Response) {
	var blocks []blockContent
	for _, candidate := range response.Candidates {
		for _, block := range candidate.Blocks {
			if block.ModalityType != gai.Text {
				blocks = append(blocks, blockContent{
					blockType: block.BlockType,
					content:   fmt.Sprintf("Received non-text block of type: %s\n", block.ModalityType),
				})
				continue
			}

			content := block.Content.String()
			switch block.BlockType {
			case gai.Content:
				content = m.renderContent(content)
			case gai.Thinking:
				content = m.renderThinking(content, block.ExtraFields["reasoning_type"])
			case gai.ToolCall:
				content = m.renderToolCall(content)
			}

			blocks = append(blocks, blockContent{
				blockType: block.BlockType,
				content:   content,
			})
		}
	}

	for _, block := range blocks {
		writer := m.stderr
		if block.blockType == gai.Content {
			writer = m.stdout
		}
		fmt.Fprint(writer, block.content)
	}

	if len(response.Candidates) > 0 {
		if messageID := GetMessageID(response.Candidates[0]); messageID != "" {
			fmt.Fprint(m.stderr, renderMetadataLine(m.metadataRenderer, fmt.Sprintf("> message_id: `%s`", messageID)))
		}
	}
}

func trailingToolResults(dialog gai.Dialog) []gai.Message {
	if len(dialog) == 0 || dialog[len(dialog)-1].Role != gai.ToolResult {
		return nil
	}

	lastAssistantIdx := -1
	for i := len(dialog) - 1; i >= 0; i-- {
		if dialog[i].Role == gai.Assistant {
			lastAssistantIdx = i
			break
		}
	}
	if lastAssistantIdx < 0 {
		return nil
	}

	var results []gai.Message
	for i := lastAssistantIdx + 1; i < len(dialog); i++ {
		if dialog[i].Role == gai.ToolResult {
			results = append(results, dialog[i])
		}
	}
	return results
}

func (m *TurnLifecycleMiddleware) printToolResult(dialog gai.Dialog, toolResultMsg gai.Message) {
	toolName := findToolName(dialog, toolResultMsg)
	messageID := GetMessageID(toolResultMsg)
	if len(toolResultMsg.Blocks) == 0 {
		return
	}

	sections := []string{fmt.Sprintf("#### Tool \"%s\" result:", toolName)}
	for _, block := range toolResultMsg.Blocks {
		sections = append(sections, formatToolResultBlockMarkdown(toolName, block))
	}
	markdownContent := strings.Join(sections, "\n\n")

	rendered, err := m.metadataRenderer.Render(markdownContent)
	if err != nil {
		fmt.Fprint(m.stderr, "\n"+markdownContent+"\n")
	} else {
		fmt.Fprint(m.stderr, "\n"+rendered)
	}

	if messageID != "" {
		fmt.Fprint(m.stderr, renderMetadataLine(m.metadataRenderer, fmt.Sprintf("> message_id: `%s`", messageID)))
	}
}

func findToolName(dialog gai.Dialog, toolResultMsg gai.Message) string {
	if len(toolResultMsg.Blocks) == 0 {
		return unknownToolName
	}
	toolCallID := toolResultMsg.Blocks[0].ID
	if toolCallID == "" {
		return unknownToolName
	}

	for i := len(dialog) - 2; i >= 0; i-- {
		msg := dialog[i]
		if msg.Role != gai.Assistant {
			continue
		}
		for _, block := range msg.Blocks {
			if block.BlockType != gai.ToolCall || block.ID != toolCallID {
				continue
			}
			var toolCall gai.ToolCallInput
			if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err == nil && toolCall.Name != "" {
				return toolCall.Name
			}
			return unknownToolName
		}
		return unknownToolName
	}
	return unknownToolName
}

func formatToolResultBlockMarkdown(toolName string, block gai.Block) string {
	if block.ModalityType != gai.Text {
		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = block.ModalityType.String()
		}
		return fmt.Sprintf("[%s content]", mimeType)
	}

	contentStr := block.Content.String()
	if toolName == codemode.ExecuteGoCodeToolName {
		return codemode.FormatResultMarkdown(contentStr, maxToolResultLines)
	}

	var jsonData any
	if err := json.Unmarshal([]byte(contentStr), &jsonData); err == nil {
		formatted, err := json.MarshalIndent(jsonData, "", "  ")
		if err == nil {
			contentStr = string(formatted)
		}
		truncated := truncateToolResultToMaxLines(contentStr, maxToolResultLines)
		return fmt.Sprintf("```json\n%s\n```", truncated)
	}

	truncated := truncateToolResultToMaxLines(contentStr, maxToolResultLines)
	return fmt.Sprintf("```\n%s\n```", truncated)
}

func truncateToolResultToMaxLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return content
	}
	if content == "" {
		return content
	}

	trailingNewline := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimSuffix(content, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= maxLines {
		return content
	}

	truncated := strings.Join(lines[:maxLines], "\n")
	if trailingNewline {
		truncated += "\n"
	}
	return truncated + "... (truncated)"
}

func (m *TurnLifecycleMiddleware) printTokenUsage(metadata gai.Metadata) {
	metrics := extractTokenUsageMetrics(metadata)
	if !metrics.hasAnyTokens() {
		return
	}

	var lines []string
	lines = append(lines, formatUsageLine(metrics))
	if contextLine, ok := formatContextUtilizationLine(metrics, m.model.ContextWindow); ok {
		lines = append(lines, contextLine)
	}

	costs := calculateUsageCosts(metrics, m.model)
	if costs.HasAnyCost {
		m.cumulativeCostUSD += costs.Total
		lines = append(lines, formatCostLine(costs, m.cumulativeCostUSD))
	}

	fmt.Fprintln(m.stderr, renderMetadataLine(m.metadataRenderer, strings.Join(lines, "\n")))
}

func renderMetadataLine(renderer ports.Renderer, raw string) string {
	rendered, err := renderer.Render(raw)
	if err != nil {
		return raw
	}
	return rendered
}

// GetMessageID retrieves the message ID from a message's ExtraFields.
// Returns an empty string if no ID is set.
func GetMessageID(msg gai.Message) string {
	if msg.ExtraFields == nil {
		return ""
	}
	id, _ := msg.ExtraFields[storage.MessageIDKey].(string)
	return id
}

type tokenUsageMetrics struct {
	InputTokens      int
	HasInputTokens   bool
	OutputTokens     int
	HasOutputTokens  bool
	CacheReadTokens  int
	HasCacheRead     bool
	CacheWriteTokens int
	HasCacheWrite    bool
}

type usageCostBreakdown struct {
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
	Total      float64
	HasAnyCost bool
}

func extractTokenUsageMetrics(metadata gai.Metadata) tokenUsageMetrics {
	metrics := tokenUsageMetrics{}

	if inputTokens, ok := gai.InputTokens(metadata); ok {
		metrics.InputTokens = inputTokens
		metrics.HasInputTokens = true
	}
	if outputTokens, ok := gai.OutputTokens(metadata); ok {
		metrics.OutputTokens = outputTokens
		metrics.HasOutputTokens = true
	}
	if cacheRead, ok := gai.CacheReadTokens(metadata); ok {
		metrics.CacheReadTokens = cacheRead
		metrics.HasCacheRead = true
	}
	if cacheWrite, ok := gai.CacheWriteTokens(metadata); ok {
		metrics.CacheWriteTokens = cacheWrite
		metrics.HasCacheWrite = true
	}

	return metrics
}

func (m tokenUsageMetrics) hasAnyTokens() bool {
	return m.HasInputTokens || m.HasOutputTokens || m.HasCacheRead || m.HasCacheWrite
}

func formatUsageLine(metrics tokenUsageMetrics) string {
	parts := make([]string, 0, 4)
	if metrics.HasInputTokens {
		parts = append(parts, fmt.Sprintf("input: `%d`", metrics.InputTokens))
	}
	if metrics.HasOutputTokens {
		parts = append(parts, fmt.Sprintf("output: `%d`", metrics.OutputTokens))
	}
	if metrics.HasCacheRead {
		parts = append(parts, fmt.Sprintf("cache read: `%d`", metrics.CacheReadTokens))
	}
	if metrics.HasCacheWrite {
		parts = append(parts, fmt.Sprintf("cache write: `%d`", metrics.CacheWriteTokens))
	}
	return "> " + strings.Join(parts, ", ")
}

func formatContextUtilizationLine(metrics tokenUsageMetrics, contextWindow uint32) (string, bool) {
	if contextWindow == 0 {
		return "", false
	}

	used := 0
	if metrics.HasInputTokens {
		used += metrics.InputTokens
	}
	if metrics.HasOutputTokens {
		used += metrics.OutputTokens
	}
	if used == 0 {
		return "", false
	}

	pct := (float64(used) / float64(contextWindow)) * 100
	return fmt.Sprintf("> context: `%d / %d` (`%.2f%%`)", used, contextWindow, pct), true
}

func calculateUsageCosts(metrics tokenUsageMetrics, model config.Model) usageCostBreakdown {
	breakdown := usageCostBreakdown{}

	if metrics.HasInputTokens {
		billableInputTokens := metrics.InputTokens
		if metrics.HasCacheRead {
			billableInputTokens -= metrics.CacheReadTokens
		}
		if metrics.HasCacheWrite && model.CacheWriteCostPerMillion != nil {
			billableInputTokens -= metrics.CacheWriteTokens
		}
		if billableInputTokens < 0 {
			billableInputTokens = 0
		}
		breakdown.Input = calculateComponentCost(billableInputTokens, model.InputCostPerMillion)
	}
	if metrics.HasOutputTokens {
		breakdown.Output = calculateComponentCost(metrics.OutputTokens, model.OutputCostPerMillion)
	}
	if metrics.HasCacheRead {
		breakdown.CacheRead = calculateComponentCost(metrics.CacheReadTokens, model.CacheReadCostPerMillion)
	}
	if metrics.HasCacheWrite {
		breakdown.CacheWrite = calculateComponentCost(metrics.CacheWriteTokens, model.CacheWriteCostPerMillion)
	}

	for _, component := range []*float64{breakdown.Input, breakdown.Output, breakdown.CacheRead, breakdown.CacheWrite} {
		if component == nil {
			continue
		}
		breakdown.Total += *component
		breakdown.HasAnyCost = true
	}

	return breakdown
}

func calculateComponentCost(tokens int, costPerMillion *float64) *float64 {
	if costPerMillion == nil {
		return nil
	}
	cost := (float64(tokens) * *costPerMillion) / 1_000_000
	return &cost
}

func formatCostLine(costs usageCostBreakdown, cumulative float64) string {
	parts := make([]string, 0, 6)
	if costs.Input != nil {
		parts = append(parts, fmt.Sprintf("input: `$%.6f`", *costs.Input))
	}
	if costs.Output != nil {
		parts = append(parts, fmt.Sprintf("output: `$%.6f`", *costs.Output))
	}
	if costs.CacheRead != nil {
		parts = append(parts, fmt.Sprintf("cache read: `$%.6f`", *costs.CacheRead))
	}
	if costs.CacheWrite != nil {
		parts = append(parts, fmt.Sprintf("cache write: `$%.6f`", *costs.CacheWrite))
	}
	parts = append(parts, fmt.Sprintf("total: `$%.6f`", costs.Total))
	parts = append(parts, fmt.Sprintf("cumulative: `$%.6f`", cumulative))
	return "> estimated cost: " + strings.Join(parts, ", ")
}
