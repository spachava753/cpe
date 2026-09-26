package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

type titleTestGenerator func(context.Context, gai.GenerationRequest) (gai.Response, error)

func (f titleTestGenerator) Generate(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
	return f(ctx, req)
}

func TestSessionTitle(t *testing.T) {
	for _, name := range []string{"concurrent title and conversation", "invalid title", "title provider failure", "invalid title usage", "canceled title", "manual before first prompt", "model switch pricing"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "title.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			rate := 1.0
			profile := config.Model{Provider: "fixture", ID: "first", ReasoningEffort: "low", Cost: &config.Pricing{Rates: config.Rates{Input: &rate, Output: &rate, CacheRead: &rate, CacheWrite: &rate}}}
			cfg := config.Config{System: "private system instructions", Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}
			started, finish := make(chan struct{}), make(chan struct{})
			titleCalls := 0
			mainCalls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			response := func(text string, input int) gai.Response {
				return gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(text)}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: input, gai.UsageMetricGenerationTokens: 2}}
			}
			var a *Agent
			main := titleTestGenerator(func(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
				mainCalls++
				if mainCalls == 1 && name != "manual before first prompt" {
					select {
					case <-started:
					case <-ctx.Done():
						return gai.Response{}, ctx.Err()
					}
					if name == "canceled title" {
						cancel()
						return gai.Response{}, ctx.Err()
					}
					if name == "model switch pricing" {
						second := profile
						second.ID = "second"
						second.Cost = nil
						err := a.QueueModel("second", second, titleTestGenerator(func(context.Context, gai.GenerationRequest) (gai.Response, error) {
							close(finish)
							return response("done", 10), nil
						}))
						if err != nil {
							return gai.Response{}, err
						}
						block, err := gai.ToolCallBlock("fixture", "starlark_repl", map[string]any{"code": "answer = 42"})
						if err != nil {
							return gai.Response{}, err
						}
						return gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{block}}}, FinishReason: gai.ToolUse}, nil
					}
					close(finish)
				}
				return response("done", 10), nil
			})
			opts := Options{Config: cfg, Model: profile, Generator: main, Store: store, CWD: dir, NewTitleGenerator: func(_ context.Context, got config.Model) (gai.Generator, error) {
				if got.ID != "first" || got.ReasoningEffort != "low" {
					t.Errorf("wrong title profile %+v", got)
				}
				return titleTestGenerator(func(ctx context.Context, req gai.GenerationRequest) (gai.Response, error) {
					titleCalls++
					close(started)
					if req.Model != "first" || len(req.Tools) != 0 || len(req.Dialog) != 1 || req.Dialog[0].Blocks[0].Content.String() != "Investigate auth" || strings.Contains(req.Instructions.Blocks[0].Content.String(), cfg.System) {
						return gai.Response{}, errors.New("incorrect title request")
					}
					select {
					case <-finish:
					case <-ctx.Done():
						return gai.Response{}, ctx.Err()
					}
					if name == "invalid title usage" {
						return response("", -1), errors.New("fixture title unavailable")
					}
					if name == "title provider failure" {
						return response("", 5000), errors.New("fixture title unavailable")
					}
					if name == "invalid title" {
						return response("two\nlines", 5000), nil
					}
					return response("Authentication investigation", 5000), nil
				}), nil
			}}
			a, err = Open(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { a.Close() }()
			if name == "manual before first prompt" {
				if err := a.Rename(ctx, "My title"); err != nil {
					t.Fatal(err)
				}
			}
			var events []Event
			err = a.Prompt(ctx, "Investigate auth", func(e Event) { events = append(events, e) })
			if name == "canceled title" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantTitle := "Authentication investigation"
			wantCalls := 1
			switch name {
			case "manual before first prompt":
				wantTitle = "My title"
				wantCalls = 0
			case "invalid title", "title provider failure", "invalid title usage", "canceled title":
				wantTitle = ""
			}
			if a.Title().String() != wantTitle || titleCalls != wantCalls {
				t.Fatalf("title=%q calls=%d", a.Title().String(), titleCalls)
			}
			if name == "invalid title" || name == "title provider failure" || name == "invalid title usage" {
				found := false
				for _, e := range events {
					found = found || e.Kind == EventWarning
				}
				if !found {
					t.Fatal("missing title warning")
				}
			}
			if a.contextSample != nil && a.contextSample.Input != 10 {
				t.Fatal("title changed context calibration")
			}
			if name == "model switch pricing" {
				found := false
				for _, e := range store.Entries() {
					if e.Type == EventUsage {
						var u usageRecord
						if err := json.Unmarshal(e.Data, &u); err != nil {
							t.Fatal(err)
						}
						if u.Tokens.Input == 5000 {
							found = true
							if u.Pricing == nil || u.Cost == nil || *u.Cost != 0.005002 || u.Context != nil {
								t.Fatalf("wrong title accounting: %+v", u)
							}
						}
					}
				}
				if !found || a.Model().ID != "second" {
					t.Fatal("title usage/model switch missing")
				}
			}
			for _, m := range a.Messages() {
				for _, b := range m.Blocks {
					if b.Content != nil && b.Content.String() == "Authentication investigation" {
						t.Fatal("title entered dialog")
					}
				}
			}
			if wantTitle == "" {
				if err := a.Prompt(t.Context(), "short follow-up", nil); err != nil {
					t.Fatal(err)
				}
				if titleCalls != 1 || a.Title().String() != "" {
					t.Fatal("retried failed title generation")
				}
			}
			if name == "invalid title usage" && (a.Usage().Unreported != 1 || a.Usage().Unpriced != 1) {
				t.Fatalf("invalid title report was treated as complete: %+v", a.Usage())
			}
			before := a.Usage()
			if err := a.Rename(t.Context(), "User renamed title"); err != nil {
				t.Fatal(err)
			}
			if err := a.Branch(t.Context(), store.Entries()[0].ID); err != nil {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = session.Resume(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			opts.Store = store
			opts.NewTitleGenerator = func(context.Context, config.Model) (gai.Generator, error) {
				t.Error("regenerated title on reopen")
				return nil, errors.New("unexpected title")
			}
			a, err = Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if a.Title().String() != "User renamed title" || a.Usage() != before {
				t.Fatal("reopen lost title/usage")
			}
			if err := a.Prompt(t.Context(), "continue", nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentRename(t *testing.T) {
	for _, name := range []string{"rename", "invalid", "canceled"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			a := &Agent{opts: Options{Store: s}}
			if err := a.Rename(t.Context(), "First"); err != nil {
				t.Fatal(err)
			}
			text := "Second"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "invalid" {
				text = ""
			}
			if name == "canceled" {
				cancel()
			}
			err = a.Rename(ctx, text)
			if name == "rename" {
				if err != nil || a.Title().String() != "Second" {
					t.Fatal(err)
				}
			} else if err == nil || a.Title().String() != "First" {
				t.Fatal("failed rename changed title", err)
			}
		})
	}
}

func TestAgentCollectTitle(t *testing.T) {
	for _, name := range []string{"usage write failure", "title write failure"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			a := &Agent{opts: Options{Store: s, Model: config.Model{ID: "fixture"}}}
			id, err := a.beginRequest(titlePurpose)
			if err != nil {
				t.Fatal(err)
			}
			if name == "usage write failure" {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var events []Event
			a.emit = func(e Event) {
				events = append(events, e)
				if name == "title write failure" && e.Kind == EventUsage {
					if err := s.Close(); err != nil {
						t.Error(err)
					}
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			results := make(chan titleResult, 1)
			results <- titleResult{response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("A title")}}}}}
			a.titleJob = &titleJob{id: id, profile: a.opts.Model, request: gai.GenerationRequest{Model: "fixture"}, cancel: cancel, result: results}
			a.collectTitle(true)
			if a.titleErr == nil || !strings.Contains(a.titleErr.Error(), "session is closed") || a.titleJob != nil || ctx.Err() != context.Canceled {
				t.Fatalf("save failure or task cleanup lost: %v", a.titleErr)
			}
			for _, e := range events {
				if e.Kind == EventTitle {
					t.Fatal("published unsaved title")
				}
			}
		})
	}
}
