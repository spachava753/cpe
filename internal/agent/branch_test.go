package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestAgentBranch(t *testing.T) {
	for _, test := range []struct {
		name, source string
		cancel       bool
	}{
		{name: "canceled replay", source: "for i in range(100000):\n    x = i", cancel: true},
		{name: "invalid recorded program", source: `fail("corrupt committed program")`},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			root := store.Path()[0].ID
			gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{
				Check: func(req gai.GenerationRequest) error {
					if len(req.Dialog) != 1 || req.Dialog[0].Blocks[0].Content.String() != "fresh" {
						t.Errorf("recovered generation contains abandoned messages: %+v", req.Dialog)
					}
					return nil
				},
				Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("ready")}}}, FinishReason: gai.EndTurn},
			})
			a, err := Open(t.Context(), Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err := a.append(gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("selected history")}}); err != nil {
				t.Fatal(err)
			}
			// Construct a committed replay fixture without executing the invalid
			// program in the live interpreter. The session envelope remains valid.
			id, err := store.Append("eval_start", map[string]any{"callId": "recorded", "code": test.source})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Append("eval_end", repl.Result{EvalID: id, CallID: "recorded", Committed: true}); err != nil {
				t.Fatal(err)
			}
			checkpoint, err := store.Append("checkpoint", map[string]string{"label": "selected"})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.append(gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("abandoned future")}}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			if err := a.Branch(ctx, checkpoint); err == nil {
				t.Fatal("failed replay accepted")
			}
			path := store.Path()
			if path[len(path)-1].ParentID != checkpoint || len(a.Messages()) != 1 || a.Messages()[0].Blocks[0].Content.String() != "selected history" {
				t.Fatalf("head and displayed dialog disagree: %+v, %+v", path, a.Messages())
			}
			before := len(store.Entries())
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{"prompt", func() error { return a.Prompt(t.Context(), "must not send", nil) }},
				{"compact", func() error { return a.Compact(t.Context()) }},
				{"model", func() error { return a.SetModel(config.Model{ID: "new"}, gen) }},
				{"reasoning", func() error { return a.SetReasoningEffort("") }},
			} {
				t.Run(operation.name, func(t *testing.T) {
					if err := operation.run(); err == nil || !strings.Contains(err.Error(), "restoration failed") {
						t.Fatalf("operation was not blocked: %v", err)
					}
				})
			}
			if len(store.Entries()) != before || a.Usage().Requests != 0 {
				t.Fatal("blocked operation modified the session")
			}
			if err := a.Branch(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			if err := a.Prompt(t.Context(), "fresh", nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
