package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/types"
)

type TokenUsagePrinterGenerator struct {
	gai.GeneratorWrapper
	renderer          types.Renderer
	writer            io.Writer
	model             config.Model
	cumulativeCostUSD float64
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

func NewTokenUsagePrinterGenerator(wrapped gai.Generator, writer io.Writer, model config.Model) *TokenUsagePrinterGenerator {
	return &TokenUsagePrinterGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: wrapped},
		renderer:         NewRenderer(),
		writer:           writer,
		model:            model,
	}
}

// WithTokenUsagePrinting returns a WrapperFunc for use with gai.Wrap.
func WithTokenUsagePrinting(writer io.Writer, model config.Model) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewTokenUsagePrinterGenerator(g, writer, model)
	}
}

func (g *TokenUsagePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	resp, err := g.GeneratorWrapper.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	metrics := extractTokenUsageMetrics(resp.UsageMetadata)
	if !metrics.hasAnyTokens() {
		return resp, nil
	}

	var lines []string
	lines = append(lines, formatUsageLine(metrics))

	if contextLine, ok := formatContextUtilizationLine(metrics, g.model.ContextWindow); ok {
		lines = append(lines, contextLine)
	}

	costs := calculateUsageCosts(metrics, g.model)
	if costs.HasAnyCost {
		g.cumulativeCostUSD += costs.Total
		lines = append(lines, formatCostLine(costs, g.cumulativeCostUSD))
	}

	tokenMsg, _ := g.renderer.Render(strings.Join(lines, "\n"))
	fmt.Fprintln(g.writer, tokenMsg)

	return resp, nil
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

	// Input tokens can include cache read/write tokens. Avoid double charging:
	// - Cache read tokens are excluded from regular input cost.
	// - Cache write tokens are excluded from regular input cost only when a dedicated
	//   cache write price is configured; otherwise they fall back to input pricing.
	if metrics.HasInputTokens {
		billableInputTokens := metrics.InputTokens
		if metrics.HasCacheRead {
			billableInputTokens -= metrics.CacheReadTokens
		}
		if metrics.HasCacheWrite && model.CacheWriteCostPerMillion != nil {
			billableInputTokens -= metrics.CacheWriteTokens
		}
		if billableInputTokens < 0 {
			panic(fmt.Sprintf("invalid token usage metrics: billable input tokens negative (input=%d cache_read=%d cache_write=%d has_cache_write_price=%t)",
				metrics.InputTokens,
				metrics.CacheReadTokens,
				metrics.CacheWriteTokens,
				model.CacheWriteCostPerMillion != nil,
			))
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
