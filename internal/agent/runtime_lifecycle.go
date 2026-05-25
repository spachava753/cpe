package agent

import (
	"encoding/json"
	"fmt"
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

// configureOutput sets up renderers and configures output writers. Call after
// constructing a Runtime with a struct literal when the constructor is not used.
func (r *Runtime) configureOutput() {
	var normalRenderer render.Iface = render.NewPlainTextRenderer()
	var thinkingRenderer render.Iface = render.NewPlainTextRenderer()

	toolCallRenderer := normalRenderer
	metadataRenderer := normalRenderer

	if render.IsTTYWriter(r.Stdout) {
		normalRenderer = render.NewGlamourRendererForWriter(r.Stdout)
	}
	if render.IsTTYWriter(r.Stderr) {
		toolCallRenderer = render.NewGlamourRendererForWriter(r.Stderr)
		metadataRenderer = toolCallRenderer
		thinkingRenderer = render.NewThinkingRendererForWriter(r.Stderr)
	}

	r.ContentRenderer = normalRenderer
	r.ToolCallRenderer = toolCallRenderer
	r.MetadataRenderer = metadataRenderer
	r.ThinkingRenderer = thinkingRenderer
}

func (r *Runtime) renderContent(content string) string {
	rendered, err := r.ContentRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (r *Runtime) renderThinking(content string, reasoningType any) string {
	if reasoningType == "reasoning.encrypted" {
		content = "[Reasoning content is encrypted]\n"
	}
	rendered, err := r.ThinkingRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (r *Runtime) renderToolCall(content string) string {
	if input, ok := codemode.ParseToolCall(content); ok {
		result := codemode.FormatToolCallMarkdown(input)
		if rendered, err := r.ToolCallRenderer.Render(result); err == nil {
			return rendered
		}
		return result
	}

	result, ok := codemode.FormatGenericToolCallMarkdown(content)
	if !ok {
		return content
	}
	if rendered, err := r.ToolCallRenderer.Render(result); err == nil {
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
		writer := r.Stderr
		if block.blockType == gai.Content {
			writer = r.Stdout
		}
		fmt.Fprint(writer, block.content)
	}

	if len(response.Candidates) > 0 {
		if messageID := GetMessageID(response.Candidates[0]); messageID != "" {
			fmt.Fprint(r.Stderr, renderMetadataLine(r.MetadataRenderer, fmt.Sprintf("> message_id: `%s`", messageID)))
		}
	}
}

func trailingToolResults(dialog gai.Dialog) []gai.Message {
	if len(dialog) == 0 || dialog[len(dialog)-1].Role != gai.ToolResult {
		return nil
	}

	lastAssistantIdx := -1
	for i, msg := range slices.Backward(dialog) {
		if msg.Role == gai.Assistant {
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

	rendered, err := r.MetadataRenderer.Render(markdownContent)
	if err != nil {
		fmt.Fprint(r.Stderr, "\n"+markdownContent+"\n")
	} else {
		fmt.Fprint(r.Stderr, "\n"+rendered)
	}

	if messageID != "" {
		fmt.Fprint(r.Stderr, renderMetadataLine(r.MetadataRenderer, fmt.Sprintf("> message_id: `%s`", messageID)))
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
	if !hasAnyTokenUsage(metadata) {
		return
	}

	var lines []string
	lines = append(lines, formatUsageLine(metadata))
	if contextLine, ok := formatContextUtilizationLine(metadata, r.Cfg.Model.ContextWindow); ok {
		lines = append(lines, contextLine)
	}

	if costLine, total, ok := formatCostLine(metadata, r.Cfg.Model, r.cumulativeCostUSD); ok {
		r.cumulativeCostUSD += total
		lines = append(lines, costLine)
	}

	fmt.Fprintln(r.Stderr, renderMetadataLine(r.MetadataRenderer, strings.Join(lines, "\n")))
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

func hasAnyTokenUsage(metadata gai.Metadata) bool {
	_, hasInputTokens := gai.InputTokens(metadata)
	_, hasOutputTokens := gai.OutputTokens(metadata)
	_, hasCacheRead := gai.CacheReadTokens(metadata)
	_, hasCacheWrite := gai.CacheWriteTokens(metadata)
	return hasInputTokens || hasOutputTokens || hasCacheRead || hasCacheWrite
}

func formatUsageLine(metadata gai.Metadata) string {
	parts := make([]string, 0, 4)
	if inputTokens, ok := gai.InputTokens(metadata); ok {
		parts = append(parts, fmt.Sprintf("input: `%d`", inputTokens))
	}
	if outputTokens, ok := gai.OutputTokens(metadata); ok {
		parts = append(parts, fmt.Sprintf("output: `%d`", outputTokens))
	}
	if cacheRead, ok := gai.CacheReadTokens(metadata); ok {
		parts = append(parts, fmt.Sprintf("cache read: `%d`", cacheRead))
	}
	if cacheWrite, ok := gai.CacheWriteTokens(metadata); ok {
		parts = append(parts, fmt.Sprintf("cache write: `%d`", cacheWrite))
	}
	return "> " + strings.Join(parts, ", ")
}

func formatContextUtilizationLine(metadata gai.Metadata, contextWindow uint32) (string, bool) {
	if contextWindow == 0 {
		return "", false
	}

	used := 0
	if inputTokens, ok := gai.InputTokens(metadata); ok {
		used += inputTokens
	}
	if outputTokens, ok := gai.OutputTokens(metadata); ok {
		used += outputTokens
	}
	if used == 0 {
		return "", false
	}

	pct := (float64(used) / float64(contextWindow)) * 100
	return fmt.Sprintf("> context: `%d / %d` (`%.2f%%`)", used, contextWindow, pct), true
}

func formatCostLine(metadata gai.Metadata, model config.Model, cumulative float64) (string, float64, bool) {
	parts := make([]string, 0, 6)
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
			parts = append(parts, fmt.Sprintf("input: `$%.6f`", cost))
			total += cost
			hasAnyCost = true
		}
	}
	if outputTokens, ok := gai.OutputTokens(metadata); ok {
		if cost, ok := calculateComponentCost(outputTokens, model.OutputCostPerMillion); ok {
			parts = append(parts, fmt.Sprintf("output: `$%.6f`", cost))
			total += cost
			hasAnyCost = true
		}
	}
	if cacheRead, ok := gai.CacheReadTokens(metadata); ok {
		if cost, ok := calculateComponentCost(cacheRead, model.CacheReadCostPerMillion); ok {
			parts = append(parts, fmt.Sprintf("cache read: `$%.6f`", cost))
			total += cost
			hasAnyCost = true
		}
	}
	if cacheWrite, ok := gai.CacheWriteTokens(metadata); ok {
		if cost, ok := calculateComponentCost(cacheWrite, model.CacheWriteCostPerMillion); ok {
			parts = append(parts, fmt.Sprintf("cache write: `$%.6f`", cost))
			total += cost
			hasAnyCost = true
		}
	}

	if !hasAnyCost {
		return "", 0, false
	}

	parts = append(parts, fmt.Sprintf("total: `$%.6f`", total))
	parts = append(parts, fmt.Sprintf("cumulative: `$%.6f`", cumulative+total))
	return "> estimated cost: " + strings.Join(parts, ", "), total, true
}

func calculateComponentCost(tokens int, costPerMillion *float64) (float64, bool) {
	if costPerMillion == nil {
		return 0, false
	}
	return (float64(tokens) * *costPerMillion) / 1_000_000, true
}
