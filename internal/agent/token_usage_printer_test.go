package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

type stubUsageGenerator struct {
	responses []gai.Response
	idx       int
}

func (s *stubUsageGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	_ = dialog
	_ = options

	resp := s.responses[s.idx]
	s.idx++
	return resp, nil
}

func TestTokenUsagePrinterGenerator_PrintsContextAndCostsWithCumulativeTracking(t *testing.T) {
	t.Parallel()

	model := config.Model{
		ContextWindow:            128000,
		InputCostPerMillion:      float64Ptr(3),
		OutputCostPerMillion:     float64Ptr(15),
		CacheReadCostPerMillion:  float64Ptr(0.3),
		CacheWriteCostPerMillion: float64Ptr(3.75),
	}

	gen := &stubUsageGenerator{responses: []gai.Response{
		{
			UsageMetadata: gai.Metadata{
				gai.UsageMetricInputTokens:      1500,
				gai.UsageMetricGenerationTokens: 200,
				gai.UsageMetricCacheReadTokens:  1000,
				gai.UsageMetricCacheWriteTokens: 500,
			},
		},
		{
			UsageMetadata: gai.Metadata{
				gai.UsageMetricInputTokens:      500,
				gai.UsageMetricGenerationTokens: 100,
			},
		},
	}}

	var out bytes.Buffer
	printer := NewTokenUsagePrinterGenerator(gen, &out, model)
	printer.renderer = &PlainTextRenderer{}

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() first call error = %v", err)
	}
	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() second call error = %v", err)
	}

	want := "> input: `1500`, output: `200`, cache read: `1000`, cache write: `500`\n" +
		"> context: `1700 / 128000` (`1.33%`)\n" +
		"> estimated cost: input: `$0.000000`, output: `$0.003000`, cache read: `$0.000300`, cache write: `$0.001875`, total: `$0.005175`, cumulative: `$0.005175`\n" +
		"> input: `500`, output: `100`\n" +
		"> context: `600 / 128000` (`0.47%`)\n" +
		"> estimated cost: input: `$0.001500`, output: `$0.001500`, total: `$0.003000`, cumulative: `$0.008175`\n"

	got := out.String()
	if got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestTokenUsagePrinterGenerator_UsesOnlyUncachedInputForInputPricing(t *testing.T) {
	t.Parallel()

	model := config.Model{
		ContextWindow:           200000,
		InputCostPerMillion:     float64Ptr(3),
		CacheReadCostPerMillion: float64Ptr(0.3),
	}
	gen := &stubUsageGenerator{responses: []gai.Response{{
		UsageMetadata: gai.Metadata{
			gai.UsageMetricInputTokens:     20000,
			gai.UsageMetricCacheReadTokens: 10000,
		},
	}}}

	var out bytes.Buffer
	printer := NewTokenUsagePrinterGenerator(gen, &out, model)
	printer.renderer = &PlainTextRenderer{}

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := "> input: `20000`, cache read: `10000`\n" +
		"> context: `20000 / 200000` (`10.00%`)\n" +
		"> estimated cost: input: `$0.030000`, cache read: `$0.003000`, total: `$0.033000`, cumulative: `$0.033000`\n"
	got := out.String()
	if got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestTokenUsagePrinterGenerator_FallsBackToInputPricingWhenCacheWritePricingMissing(t *testing.T) {
	t.Parallel()

	model := config.Model{
		ContextWindow:           50000,
		InputCostPerMillion:     float64Ptr(2),
		CacheReadCostPerMillion: float64Ptr(0.2),
	}
	gen := &stubUsageGenerator{responses: []gai.Response{{
		UsageMetadata: gai.Metadata{
			gai.UsageMetricInputTokens:      10000,
			gai.UsageMetricCacheReadTokens:  4000,
			gai.UsageMetricCacheWriteTokens: 3000,
		},
	}}}

	var out bytes.Buffer
	printer := NewTokenUsagePrinterGenerator(gen, &out, model)
	printer.renderer = &PlainTextRenderer{}

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := "> input: `10000`, cache read: `4000`, cache write: `3000`\n" +
		"> context: `10000 / 50000` (`20.00%`)\n" +
		"> estimated cost: input: `$0.012000`, cache read: `$0.000800`, total: `$0.012800`, cumulative: `$0.012800`\n"
	got := out.String()
	if got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestCalculateUsageCosts_PanicsWhenBillableInputTokensNegative(t *testing.T) {
	t.Parallel()

	metrics := tokenUsageMetrics{
		InputTokens:     100,
		HasInputTokens:  true,
		CacheReadTokens: 200,
		HasCacheRead:    true,
	}
	model := config.Model{InputCostPerMillion: float64Ptr(3)}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got nil")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic string, got %T", r)
		}
		if !strings.Contains(msg, "invalid token usage metrics") {
			t.Fatalf("unexpected panic message: %q", msg)
		}
	}()

	_ = calculateUsageCosts(metrics, model)
}

func TestTokenUsagePrinterGenerator_SkipsCostWhenPricingMissing(t *testing.T) {
	t.Parallel()

	model := config.Model{ContextWindow: 1000}
	gen := &stubUsageGenerator{responses: []gai.Response{{
		UsageMetadata: gai.Metadata{
			gai.UsageMetricInputTokens:      100,
			gai.UsageMetricGenerationTokens: 50,
		},
	}}}

	var out bytes.Buffer
	printer := NewTokenUsagePrinterGenerator(gen, &out, model)
	printer.renderer = &PlainTextRenderer{}

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := "> input: `100`, output: `50`\n> context: `150 / 1000` (`15.00%`)\n"
	got := out.String()
	if got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestTokenUsagePrinterGenerator_SkipsContextLineWhenContextWindowUnknown(t *testing.T) {
	t.Parallel()

	model := config.Model{ContextWindow: 0}
	gen := &stubUsageGenerator{responses: []gai.Response{{
		UsageMetadata: gai.Metadata{
			gai.UsageMetricInputTokens:      100,
			gai.UsageMetricGenerationTokens: 25,
		},
	}}}

	var out bytes.Buffer
	printer := NewTokenUsagePrinterGenerator(gen, &out, model)
	printer.renderer = &PlainTextRenderer{}

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := "> input: `100`, output: `25`\n"
	got := out.String()
	if got != want {
		t.Fatalf("output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestTokenUsagePrinterGenerator_SkipsPrintingWhenUsageMetadataEmpty(t *testing.T) {
	t.Parallel()

	model := config.Model{ContextWindow: 1000}
	gen := &stubUsageGenerator{responses: []gai.Response{{}}}

	var out bytes.Buffer
	printer := NewTokenUsagePrinterGenerator(gen, &out, model)
	printer.renderer = &PlainTextRenderer{}

	if _, err := printer.Generate(context.Background(), nil, nil); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if out.String() != "" {
		t.Fatalf("expected no output, got %q", out.String())
	}
}

func float64Ptr(v float64) *float64 { return &v }
