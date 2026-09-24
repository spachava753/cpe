package agent

import (
	"context"
	"errors"
	"iter"
	"math"
	"path/filepath"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestUsageSurvivesCompactionModelSwitchBranchAndRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	store, err := session.Open(path, dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	root := store.Path()[0].ID
	in, out, read, write := 2.0, 10.0, 0.2, 2.5
	longIn, longOut, longRead, longWrite := 4.0, 15.0, 0.4, 5.0
	pricing := &config.Pricing{Rates: config.Rates{Input: &in, Output: &out, CacheRead: &read, CacheWrite: &write}, LongContext: &config.PriceTier{AboveInputTokens: 100, Rates: config.Rates{Input: &longIn, Output: &longOut, CacheRead: &longRead, CacheWrite: &longWrite}}}
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("First reply")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 100, gai.UsageMetricGenerationTokens: 10, gai.UsageMetricCacheReadTokens: 40, gai.UsageMetricCacheWriteTokens: 20}}},
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Summary")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 110, gai.UsageMetricGenerationTokens: 12, gai.UsageMetricCacheReadTokens: 50, gai.UsageMetricCacheWriteTokens: 20}}},
	)
	opts := Options{Config: config.Config{System: "Account for all usage", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 5}, Compaction: config.Compaction{Prompt: "Summarize accounting fixture"}}, Model: config.Model{Provider: codexProvider, ID: "metered-first", Cost: pricing}, Generator: gen, Store: store, CWD: dir}
	a, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	updates := 0
	if err := a.Prompt(t.Context(), "hello", func(e Event) {
		if e.Kind == EventUsage {
			updates++
			if e.Usage.Total() != 110 {
				t.Errorf("usage event: %+v", e.Usage)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if updates != 1 || math.Abs(a.Usage().Cost-0.000238) > 1e-12 {
		t.Fatalf("first usage: %+v", a.Usage())
	}
	if err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(a.Messages()) != 1 || a.Usage().Total() != 232 {
		t.Fatal("compaction lost usage")
	}
	flat := 100.0
	nextModel := config.Model{Provider: "responses", ID: "metered-second", Cost: &config.Pricing{Rates: config.Rates{Input: &flat, Output: &flat, CacheRead: &flat, CacheWrite: &flat}}}
	next := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Next reply")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 20, gai.UsageMetricGenerationTokens: 2}}})
	if err := a.SetModel(nextModel, next); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(t.Context(), "continue", nil); err != nil {
		t.Fatal(err)
	}
	want := a.Usage()
	if want.tokens != (tokens{Input: 100, Output: 24, CacheRead: 90, CacheWrite: 40}) || want.Total() != 254 || want.Requests != 3 || want.Unreported != 0 || want.Unpriced != 0 || want.Legacy || math.Abs(want.Cost-0.002898) > 1e-12 {
		t.Fatalf("totals: %+v", want)
	}
	if err := a.Branch(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if a.Usage() != want {
		t.Fatal("branching discarded consumed tokens")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = session.Open(path, dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	opts.Store, opts.Model, opts.Generator = store, nextModel, next
	a, err = Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Usage() != want || len(a.Messages()) != 0 {
		t.Fatalf("restart changed accounting: %+v", a.Usage())
	}
}

type failingUsageStream struct{ report bool }

func (g failingUsageStream) Generate(context.Context, gai.GenerationRequest) (gai.Response, error) {
	return gai.Response{}, errors.New("fixture failure")
}
func (g failingUsageStream) Stream(context.Context, gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		if g.report && !yield(gai.StreamChunk{Block: gai.MetadataBlock(gai.Metadata{gai.UsageMetricInputTokens: 20, gai.UsageMetricGenerationTokens: 5, gai.UsageMetricCacheReadTokens: 10})}) {
			return
		}
		yield(gai.StreamChunk{Err: context.Canceled})
	}
}

func TestUsageTracksFailedStreamsAndInterruptedRequests(t *testing.T) {
	for _, report := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "reported"}[report], func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "failure.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.Append(requestStartEntry, map[string]string{"purpose": "conversation", "model": "crashed"}); err != nil {
				t.Fatal(err)
			}
			a, err := Open(t.Context(), Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}}, Model: config.Model{ID: "failure-fixture"}, Generator: failingUsageStream{report}, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err := a.Prompt(t.Context(), "test failure", nil); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			u := a.Usage()
			if u.Requests != 2 || u.Unpriced != 2 || len(a.Messages()) != 1 {
				t.Fatalf("unexpected usage: %+v", u)
			}
			if report {
				if u.Total() != 25 || u.Input != 10 || u.CacheRead != 10 || u.Unreported != 1 {
					t.Fatalf("lost failure usage: %+v", u)
				}
			} else if u.Total() != 0 || u.Unreported != 2 {
				t.Fatalf("invented usage: %+v", u)
			}
		})
	}
}

func TestLegacyUsageIsUnknownAndMalformedRecordsFailClosed(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "negative"}[malformed], func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "legacy.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.Append("message", encodeMessage(gai.Message{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Old response")}})); err != nil {
				t.Fatal(err)
			}
			if malformed {
				id, err := store.Append(requestStartEntry, map[string]string{"model": "corrupt-fixture"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Append(EventUsage, usageRecord{RequestID: id, Tokens: tokens{Input: -1}}); err != nil {
					t.Fatal(err)
				}
			}
			a, err := Open(t.Context(), Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024}}, Model: config.Model{ID: "legacy-fixture"}, Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
			if malformed {
				if err == nil {
					a.Close()
					t.Fatal("negative usage accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if !a.Usage().Legacy || a.Usage().Total() != 0 {
				t.Fatalf("legacy consumption was invented: %+v", a.Usage())
			}
		})
	}
}

func TestNormalizeTokens(t *testing.T) {
	tests := []struct {
		name              string
		metadata          gai.Metadata
		want              tokens
		complete, wantErr bool
	}{
		{name: "missing report"},
		{name: "explicit zero", metadata: gai.Metadata{gai.UsageMetricInputTokens: 0, gai.UsageMetricGenerationTokens: 0}, complete: true},
		{name: "disjoint buckets", metadata: gai.Metadata{gai.UsageMetricInputTokens: 100, gai.UsageMetricGenerationTokens: 20, gai.UsageMetricCacheReadTokens: 40, gai.UsageMetricCacheWriteTokens: 10}, want: tokens{Input: 50, Output: 20, CacheRead: 40, CacheWrite: 10}, complete: true},
		{name: "input only", metadata: gai.Metadata{gai.UsageMetricInputTokens: 100}, want: tokens{Input: 100}},
		{name: "output only", metadata: gai.Metadata{gai.UsageMetricGenerationTokens: 20}, want: tokens{Output: 20}},
		{name: "output and caches without input", metadata: gai.Metadata{gai.UsageMetricGenerationTokens: 20, gai.UsageMetricCacheReadTokens: 40, gai.UsageMetricCacheWriteTokens: 10}, want: tokens{Output: 20, CacheRead: 40, CacheWrite: 10}},
		{name: "explicit zero contradicts cache", metadata: gai.Metadata{gai.UsageMetricInputTokens: 0, gai.UsageMetricCacheReadTokens: 1}, wantErr: true},
		{name: "cache reads exceed input", metadata: gai.Metadata{gai.UsageMetricInputTokens: 10, gai.UsageMetricCacheReadTokens: 11}, wantErr: true},
		{name: "combined caches exceed input", metadata: gai.Metadata{gai.UsageMetricInputTokens: 10, gai.UsageMetricCacheReadTokens: 6, gai.UsageMetricCacheWriteTokens: 5}, wantErr: true},
		{name: "negative", metadata: gai.Metadata{gai.UsageMetricGenerationTokens: -1}, wantErr: true},
		{name: "wrong type", metadata: gai.Metadata{gai.UsageMetricInputTokens: float64(10)}, wantErr: true},
	}
	// Metadata counters are native ints. Only 64-bit hosts can individually
	// represent counts large enough to overflow the int64 accounting buckets.
	maxCount := int(^uint(0) >> 1)
	if int64(maxCount) == math.MaxInt64 {
		for _, test := range []struct {
			name     string
			metadata gai.Metadata
		}{
			{"input plus output overflow", gai.Metadata{gai.UsageMetricInputTokens: maxCount, gai.UsageMetricGenerationTokens: 1}},
			{"partial cache overflow", gai.Metadata{gai.UsageMetricCacheReadTokens: maxCount, gai.UsageMetricCacheWriteTokens: 1}},
			{"partial output overflow", gai.Metadata{gai.UsageMetricCacheReadTokens: maxCount, gai.UsageMetricGenerationTokens: 1}},
		} {
			t.Run(test.name, func(t *testing.T) {
				if _, _, err := normalizeTokens(test.metadata); err == nil {
					t.Fatal("overflow accepted")
				}
			})
		}
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, complete, err := normalizeTokens(test.metadata)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeTokens() error = %v, want error %t", err, test.wantErr)
			}
			if err == nil && (got != test.want || complete != test.complete) {
				t.Fatalf("normalizeTokens() = %+v, complete=%t; want %+v, complete=%t", got, complete, test.want, test.complete)
			}
		})
	}
}
