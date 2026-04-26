package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/render"
	"github.com/spachava753/cpe/internal/storage"
)

const (
	maxToolResultLines = 20
	unknownToolName    = "unknown"
)

func (r *Runtime) configureOutput(disablePrinting bool) {
	normalRenderer := render.Iface(&render.PlainTextRenderer{})
	thinkingRenderer := render.Iface(&render.PlainTextRenderer{})

	if disablePrinting {
		r.stdout = io.Discard
		r.stderr = io.Discard
	} else if render.IsTTYWriter(r.stderr) && render.IsTTYWriter(r.stdout) {
		normalRenderer = render.NewGlamourRenderer()
		thinkingRenderer = render.NewThinkingRenderer()
	}

	r.contentRenderer = normalRenderer
	r.toolCallRenderer = normalRenderer
	r.metadataRenderer = normalRenderer
	r.thinkingRenderer = thinkingRenderer
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

func (r *Runtime) renderContent(content string) string {
	rendered, err := r.contentRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (r *Runtime) renderThinking(content string, reasoningType any) string {
	if reasoningType == "reasoning.encrypted" {
		content = "[Reasoning content is encrypted]\n"
	}
	rendered, err := r.thinkingRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (r *Runtime) renderToolCall(content string) string {
	if input, ok := codemode.ParseToolCall(content); ok {
		result := codemode.FormatToolCallMarkdown(input)
		if rendered, err := r.toolCallRenderer.Render(result); err == nil {
			return rendered
		}
		return result
	}

	result, ok := codemode.FormatGenericToolCallMarkdown(content)
	if !ok {
		return content
	}
	if rendered, err := r.toolCallRenderer.Render(result); err == nil {
		return rendered
	}
	return content
}

type blockContent struct {
	blockType string
	content   string
}

func (r *Runtime) printResponse(response gai.Response) {
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
				content = r.renderContent(content)
			case gai.Thinking:
				content = r.renderThinking(content, block.ExtraFields["reasoning_type"])
			case gai.ToolCall:
				content = r.renderToolCall(content)
			}

			blocks = append(blocks, blockContent{
				blockType: block.BlockType,
				content:   content,
			})
		}
	}

	for _, block := range blocks {
		writer := r.stderr
		if block.blockType == gai.Content {
			writer = r.stdout
		}
		fmt.Fprint(writer, block.content)
	}

	if len(response.Candidates) > 0 {
		if messageID := GetMessageID(response.Candidates[0]); messageID != "" {
			fmt.Fprint(r.stderr, renderMetadataLine(r.metadataRenderer, fmt.Sprintf("> message_id: `%s`", messageID)))
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

func (r *Runtime) printToolResult(dialog gai.Dialog, toolResultMsg gai.Message) {
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

	rendered, err := r.metadataRenderer.Render(markdownContent)
	if err != nil {
		fmt.Fprint(r.stderr, "\n"+markdownContent+"\n")
	} else {
		fmt.Fprint(r.stderr, "\n"+rendered)
	}

	if messageID != "" {
		fmt.Fprint(r.stderr, renderMetadataLine(r.metadataRenderer, fmt.Sprintf("> message_id: `%s`", messageID)))
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

func (r *Runtime) printTokenUsage(metadata gai.Metadata) {
	metrics := extractTokenUsageMetrics(metadata)
	if !metrics.hasAnyTokens() {
		return
	}

	var lines []string
	lines = append(lines, formatUsageLine(metrics))
	if contextLine, ok := formatContextUtilizationLine(metrics, r.cfg.Model.ContextWindow); ok {
		lines = append(lines, contextLine)
	}

	costs := calculateUsageCosts(metrics, r.cfg.Model)
	if costs.HasAnyCost {
		r.cumulativeCostUSD += costs.Total
		lines = append(lines, formatCostLine(costs, r.cumulativeCostUSD))
	}

	fmt.Fprintln(r.stderr, renderMetadataLine(r.metadataRenderer, strings.Join(lines, "\n")))
}

func renderMetadataLine(renderer render.Iface, raw string) string {
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
