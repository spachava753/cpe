package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

const requestStartEntry = "request_start"

// tokens contains disjoint token buckets. Input excludes cache reads and writes;
// Output includes provider-reported reasoning tokens, never counted separately.
type tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Total counts every reported token once, including reused input on each call.
func (t tokens) Total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

// Usage accumulates all requests in the session file, including compactions and
// inactive branches. Unreported counts requests without complete token reports;
// Unpriced counts requests lacking a complete cost estimate. Legacy indicates
// conversation history created before usage accounting. Cost is an estimate in
// USD at each request's configured rates, not an account bill.
type Usage struct {
	tokens
	Cost       float64
	Requests   int
	Unreported int
	Unpriced   int
	Legacy     bool
}

type usageRecord struct {
	RequestID string          `json:"request_id"`
	Tokens    tokens          `json:"tokens"`
	Complete  bool            `json:"complete"`
	Pricing   *config.Pricing `json:"pricing,omitempty"`
	Cost      *float64        `json:"cost_usd,omitempty"`
	Context   *contextSample  `json:"context,omitempty"`
}

// Usage returns cumulative session accounting between operations. During work,
// callers should consume EventUsage snapshots instead of reading the Agent.
func (a *Agent) Usage() Usage { return a.usage }

func (a *Agent) restoreUsage() error {
	pending := map[string]bool{}
	for _, entry := range a.opts.Store.Entries() {
		switch entry.Type {
		case requestStartEntry:
			pending[entry.ID] = true
			a.usage.Requests++
			a.usage.Unreported++
			a.usage.Unpriced++
		case EventUsage:
			var record usageRecord
			if err := json.Unmarshal(entry.Data, &record); err != nil {
				return fmt.Errorf("restore usage: %w", err)
			}
			if !pending[record.RequestID] {
				return errors.New("usage has a missing or already completed request")
			}
			if err := a.validateUsage(record); err != nil {
				return err
			}
			delete(pending, record.RequestID)
			a.addUsage(record)
		case "message":
			if a.usage.Requests == 0 {
				message, err := decodeMessage(entry.Data)
				if err != nil {
					return err
				}
				if message.Role == gai.Assistant {
					a.usage.Legacy = true
				}
			}
		}
	}
	return nil
}

func (a *Agent) addUsage(record usageRecord) {
	a.usage.Input += record.Tokens.Input
	a.usage.Output += record.Tokens.Output
	a.usage.CacheRead += record.Tokens.CacheRead
	a.usage.CacheWrite += record.Tokens.CacheWrite
	if record.Complete {
		a.usage.Unreported--
	}
	if record.Cost != nil {
		a.usage.Cost += *record.Cost
		a.usage.Unpriced--
	}
}

func (a *Agent) validateUsage(record usageRecord) error {
	total := a.usage.Total()
	for _, count := range []int64{record.Tokens.Input, record.Tokens.Output, record.Tokens.CacheRead, record.Tokens.CacheWrite} {
		if count < 0 || count > math.MaxInt64-total {
			return errors.New("invalid or overflowing saved usage")
		}
		total += count
	}
	if record.Cost != nil && (*record.Cost < 0 || math.IsNaN(*record.Cost) || math.IsInf(a.usage.Cost+*record.Cost, 0)) {
		return errors.New("invalid or overflowing saved cost")
	}
	if s := record.Context; s != nil && (s.Estimate < 0 || s.Input < 0) {
		return errors.New("invalid saved context estimate")
	}
	return nil
}

func (a *Agent) beginRequest(purpose string) (string, error) {
	id, err := a.opts.Store.Append(requestStartEntry, struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Purpose  string `json:"purpose"`
	}{a.opts.Model.Provider, a.opts.Model.ID, purpose})
	if err == nil {
		a.usage.Requests++
		a.usage.Unreported++
		a.usage.Unpriced++
	}
	return id, err
}

func (a *Agent) finishRequest(id string, metadata gai.Metadata, req gai.GenerationRequest, purpose string) error {
	record := usageRecord{RequestID: id, Pricing: a.opts.Model.Cost}
	tokens, complete, err := normalizeTokens(metadata)
	if err == nil {
		record.Tokens, record.Complete = tokens, complete
		if input, ok := metadata[gai.UsageMetricInputTokens].(int); ok && purpose != compactionEntry {
			record.Context = &contextSample{Provider: a.opts.Model.Provider, Model: req.Model, Estimate: estimateTokens(req), Input: input}
		}
	}
	if record.Complete && record.Pricing != nil {
		rates := record.Pricing.Rates
		input := tokens.Input + tokens.CacheRead + tokens.CacheWrite
		if tier := record.Pricing.LongContext; tier != nil && input > tier.AboveInputTokens {
			rates = tier.Rates
		}
		cost := (float64(tokens.Input)*(*rates.Input) + float64(tokens.Output)*(*rates.Output) + float64(tokens.CacheRead)*(*rates.CacheRead) + float64(tokens.CacheWrite)*(*rates.CacheWrite)) / 1_000_000
		if !math.IsInf(cost, 0) && !math.IsNaN(cost) {
			record.Cost = &cost
		}
	}
	if validationErr := a.validateUsage(record); validationErr != nil {
		return errors.Join(err, validationErr)
	}
	if _, saveErr := a.opts.Store.Append(EventUsage, record); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	a.addUsage(record)
	if record.Context != nil {
		a.contextSample = record.Context
	}
	snapshot := a.usage
	a.notify(Event{Kind: EventUsage, Usage: &snapshot})
	return err
}

func normalizeTokens(metadata gai.Metadata) (tokens, bool, error) {
	var counts [4]int64
	keys := []string{gai.UsageMetricInputTokens, gai.UsageMetricGenerationTokens, gai.UsageMetricCacheReadTokens, gai.UsageMetricCacheWriteTokens}
	complete := true
	for i, key := range keys {
		value, exists := metadata[key]
		if !exists {
			if i < 2 {
				complete = false
			}
			continue
		}
		count, ok := value.(int)
		if !ok || count < 0 {
			return tokens{}, false, fmt.Errorf("invalid token metric %s", key)
		}
		counts[i] = int64(count)
	}
	input, output, read, write := counts[0], counts[1], counts[2], counts[3]
	if read > input || write > input-read || output > math.MaxInt64-input {
		return tokens{}, false, errors.New("inconsistent provider token counts")
	}
	return tokens{Input: input - read - write, Output: output, CacheRead: read, CacheWrite: write}, complete, nil
}

// Meter outside gai's acceptance hooks: usage is still billable when a response
// is rejected, canceled after completion, or fails after reporting usage.
type meteredGenerator struct {
	agent   *Agent
	purpose string
}

func (a *Agent) metered(purpose string) gai.Generator {
	g := meteredGenerator{agent: a, purpose: purpose}
	if source, ok := a.opts.Generator.(gai.StreamingGenerator); ok {
		return meteredStream{meteredGenerator: g, source: source}
	}
	return g
}

func (g meteredGenerator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	if err := ctx.Err(); err != nil {
		return gai.Response{}, err
	}
	id, err := g.agent.beginRequest(g.purpose)
	if err != nil {
		return gai.Response{}, err
	}
	response, err := g.agent.opts.Generator.Generate(ctx, req)
	return response, errors.Join(err, g.agent.finishRequest(id, response.UsageMetadata, req, g.purpose))
}

type meteredStream struct {
	meteredGenerator
	source gai.StreamingGenerator
}

func (g meteredStream) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	return (&gai.StreamingAdapter{S: g}).Generate(ctx, req)
}

func (g meteredStream) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		if err := ctx.Err(); err != nil {
			yield(gai.StreamChunk{Err: err})
			return
		}
		id, err := g.agent.beginRequest(g.purpose)
		if err != nil {
			yield(gai.StreamChunk{Err: err})
			return
		}
		metadata := gai.Metadata{}
		for chunk := range g.source.Stream(ctx, req) {
			if chunk.Err == nil && chunk.Block.BlockType == gai.MetadataBlockType && chunk.Block.Content != nil {
				// Metadata blocks contain JSON numbers. Decode only the known integer
				// counters so provider-specific floats and strings remain irrelevant.
				var raw map[string]json.RawMessage
				if err := json.Unmarshal([]byte(chunk.Block.Content.String()), &raw); err != nil {
					chunk.Err = err
				} else {
					for _, key := range []string{gai.UsageMetricInputTokens, gai.UsageMetricGenerationTokens, gai.UsageMetricCacheReadTokens, gai.UsageMetricCacheWriteTokens} {
						if value, ok := raw[key]; ok {
							var count int
							if err := json.Unmarshal(value, &count); err != nil {
								chunk.Err = err
								break
							}
							metadata[key] = count
						}
					}
				}
			}
			if !yield(chunk) || chunk.Err != nil {
				// Even if the consumer stops, save any usage already reported.
				_ = g.agent.finishRequest(id, metadata, req, g.purpose)
				return
			}
		}
		if err := g.agent.finishRequest(id, metadata, req, g.purpose); err != nil {
			yield(gai.StreamChunk{Err: err})
		}
	}
}
