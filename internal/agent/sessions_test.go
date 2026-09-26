package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestSessionsOpen(t *testing.T) {
	for _, name := range []string{"new and resume", "same session", "locked", "missing", "malformed", "canceled", "provider failure"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Config{Dir: dir, DefaultModel: "first", Models: map[string]config.Model{"first": {ID: "first"}, "second": {ID: "second"}}, Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}
			var ids []string
			calls := 0
			providerFailure := false
			sessions := NewSessions(SessionOptions{Directory: filepath.Join(dir, "sessions"), CWD: dir,
				LoadConfig: func(context.Context) (config.Config, error) { return cfg, nil },
				NewGenerator: func(_ context.Context, _ config.Model, id string) (gai.Generator, error) {
					if providerFailure {
						return nil, errors.New("provider unavailable")
					}
					ids = append(ids, id)
					return agenttest.NewScriptedGenerator(), nil
				}, Tools: []repl.Tool{{Name: "counter", Execute: func(context.Context, map[string]any) (any, error) { calls++; return 42, nil }}},
			})
			defer sessions.Close()
			first, err := sessions.Open(t.Context(), SessionStart{})
			if err != nil {
				t.Fatal(err)
			}
			path := first.Agent.SessionFile()
			result, err := first.Agent.repl.Eval(t.Context(), "fixture", `load("tools.star", "counter"); answer = counter(); print(answer)`)
			if err != nil || result.Output != "42\n" {
				t.Fatalf("%+v %v", result, err)
			}
			if name == "new and resume" {
				cfg.DefaultModel = "second"
				second, err := sessions.Open(t.Context(), SessionStart{})
				if err != nil {
					t.Fatal(err)
				}
				if second.Agent.SessionFile() == path || second.Name != "second" || len(second.Agent.Messages()) != 0 {
					t.Fatal("new session retained old state/default")
				}
				next, err := sessions.Open(t.Context(), SessionStart{Path: path})
				if err != nil {
					t.Fatal(err)
				}
				got, err := next.Agent.repl.Eval(t.Context(), "read", `print(answer)`)
				if err != nil || got.Output != "42\n" || calls != 1 {
					t.Fatalf("%+v %v effects=%d", got, err, calls)
				}
				if len(ids) != 3 || ids[0] == ids[1] || ids[0] != ids[2] {
					t.Fatalf("wrong provider session IDs: %v", ids)
				}
				if _, err := sessions.Generator(t.Context(), cfg.Models["second"]); err != nil {
					t.Fatal(err)
				}
				if ids[3] != ids[0] {
					t.Fatal("model switch used wrong session ID")
				}
				if err := sessions.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := session.Resume(path, dir, repl.Runtime)
				if err != nil {
					t.Fatal(err)
				}
				_ = reopened.Close()
				return
			}
			if name == "same session" {
				next, err := sessions.Open(t.Context(), SessionStart{Path: path})
				if err != nil || next != first || len(ids) != 1 {
					t.Fatal("same session replaced agent", err)
				}
				return
			}
			target := filepath.Join(dir, "target.jsonl")
			switch name {
			case "locked":
				s, err := session.Open(target, dir, repl.Runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
			case "malformed":
				if err := os.WriteFile(target, []byte("bad\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "provider failure":
				target = ""
				providerFailure = true
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "canceled" {
				cancel()
			}
			before := first.Agent.opts.Store.Entries()
			next, err := sessions.Open(ctx, SessionStart{Path: target})
			if err == nil || next != nil || sessions.current != first || !reflect.DeepEqual(before, first.Agent.opts.Store.Entries()) {
				t.Fatalf("failed switch replaced old state: %v", err)
			}
			got, err := first.Agent.repl.Eval(t.Context(), "still usable", `print(answer)`)
			if err != nil || got.Output != "42\n" || calls != 1 {
				t.Fatalf("old session unusable: %+v %v", got, err)
			}
		})
	}
}
