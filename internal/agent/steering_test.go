package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestSteeredConversation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "Generate"
		if stream {
			name = "Stream"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(context.Canceled)
			calls := 0
			host := repl.Tool{Name: "hold", Execute: func(context.Context, map[string]any) (any, error) {
				calls++
				cancel(ErrSteered)
				return nil, context.Canceled
			}}
			first, err := gai.ToolCallBlock("first", "starlark_repl", map[string]any{codeParameter: `load("tools.star", "hold")
x = [1]; x.append(7); print("partial tool output")
load("repl.star", "emit_image")
emit_image("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII=", mime_type="image/png")
hold(); x.append(999)`})
			if err != nil {
				t.Fatal(err)
			}
			second, err := gai.ToolCallBlock("skipped", "starlark_repl", map[string]any{codeParameter: `x.append(999); hold()`})
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{Agent: config.Agent{ToolTimeout: "2s", MaxRounds: 4}, Compaction: config.Compaction{Prompt: "Summarize"}}
			profile := config.Model{Provider: "responses", ID: "fixture"}
			opts := Options{Config: cfg, Model: profile, Generator: switchSource(stream, nil, []gai.Block{first, second}, gai.ToolUse), Store: store, CWD: dir, Tools: []repl.Tool{host}}
			a, err := Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err := a.Prompt(ctx, "initial", nil); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			messages := a.Messages()
			if len(messages) != 4 || calls != 1 {
				t.Fatalf("messages=%v calls=%d", messages, calls)
			}
			for i, id := range []string{"first", "skipped"} {
				result := messages[i+2]
				if result.Role != gai.ToolResult || result.Blocks[0].ID != id || !result.ToolResultError || !strings.Contains(result.Blocks[0].Content.String(), "steered prompt") {
					t.Fatalf("missing reconciled result: %+v", result)
				}
			}
			if !strings.Contains(messages[2].Blocks[0].Content.String(), "partial tool output") || len(messages[2].Blocks) != 2 || messages[2].Blocks[1].ModalityType != gai.Image {
				t.Fatal("lost partial output/image")
			}
			points := a.Checkpoints()
			checkpoint := points[len(points)-1].ID
			follow := switchSource(stream, func(req gai.GenerationRequest) error {
				if len(req.Dialog) != 6 || req.Dialog[4].Blocks[0].Content.String() != "new direction" || req.Dialog[5].Blocks[0].Content.String() != "and this" {
					return errors.New("steering preceded tool reconciliation or batch lost")
				}
				return nil
			}, []gai.Block{gai.TextBlock("redirected")}, gai.EndTurn)
			if err := a.QueueModel("new", config.Model{Provider: "anthropic", ID: "new"}, follow); err != nil {
				t.Fatal(err)
			}
			if err := a.Prompts(t.Context(), []string{"new direction", "and this"}, nil); err != nil {
				t.Fatal(err)
			}
			if a.Model().ID != "new" {
				t.Fatal("pending model lost")
			}
			if err := a.SetModel(profile, agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("summary")}}}, FinishReason: gai.EndTurn}})); err != nil {
				t.Fatal(err)
			}
			if err := a.Compact(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			opts.Store = reopened
			opts.Generator = agenttest.NewScriptedGenerator()
			opts.Tools = []repl.Tool{{Name: "hold", Execute: func(context.Context, map[string]any) (any, error) {
				t.Error("replayed host effect")
				return nil, errors.New("disabled")
			}}}
			restored, err := Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			for _, branch := range []bool{false, true} {
				if branch {
					if err := restored.Branch(t.Context(), checkpoint); err != nil {
						t.Fatal(err)
					}
				}
				result, err := restored.repl.Eval(t.Context(), "inspect", `print(x)`)
				if err != nil || result.Error != "" || result.Output != "[1, 7]\n" || calls != 1 {
					t.Fatal(result, err, calls)
				}
			}
		})
	}
}
