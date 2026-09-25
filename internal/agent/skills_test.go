package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/skills"
)

func TestSkillInvocationAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, flags, prompt string
		modelVisible        bool
	}{
		{"user invokes shared skill", "", "/skill:review check the staged changes", true},
		{"user invokes user-only skill", "disable-model-invocation: true\n", "/skill:review check the staged changes", false},
		{"model chooses shared skill", "", "Check the staged changes", true},
		{"model chooses model-only skill", "user-invocable: false\n", "Check the staged changes", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "skills")
			skillDir := filepath.Join(root, "review")
			if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0700); err != nil {
				t.Fatal(err)
			}
			skillPath := filepath.Join(skillDir, "SKILL.md")
			body := "Read references/check.txt. Skill body is loaded on demand."
			if err := os.WriteFile(skillPath, []byte("---\nname: review\ndescription: Inspect staged changes\n"+tc.flags+"---\n"+body), 0600); err != nil {
				t.Fatal(err)
			}
			reference := filepath.Join(skillDir, "references", "check.txt")
			if err := os.WriteFile(reference, []byte("Check passed"), 0600); err != nil {
				t.Fatal(err)
			}
			catalog, warnings := skills.Discover(root)
			if len(warnings) != 0 {
				t.Fatal(warnings)
			}
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			code := fmt.Sprintf(`load("os.star", "os")
f = open(%q)
skill_text = f.read()
f.close()
f = open(%q)
skill_check = f.read()
f.close()
fd = os.open("effect", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
os.write(fd, "once\n")
os.close(fd)
print(skill_text, skill_check)`, skillPath, reference)
			call, err := gai.ToolCallBlock("read-skill", "starlark_repl", map[string]any{codeParameter: code})
			if err != nil {
				t.Fatal(err)
			}
			gen := agenttest.NewScriptedGenerator(
				agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					if len(req.Tools) != 1 || req.Tools[0].Name != "starlark_repl" {
						t.Errorf("exposed tools = %v", req.Tools)
					}
					instructions := req.Instructions.Blocks[0].Content.String()
					if strings.Contains(instructions, skillPath) != tc.modelVisible || strings.Contains(instructions, "Inspect staged changes") != tc.modelVisible || strings.Contains(instructions, body) {
						t.Error("model catalog violated disclosure policy")
					}
					input := req.Dialog[0].Blocks[0].Content.String()
					if !strings.HasPrefix(input, tc.prompt) || strings.Contains(input, body) {
						t.Errorf("skill input = %q", input)
					}
					if strings.HasPrefix(tc.prompt, "/skill:") && !strings.Contains(input, skillPath) {
						t.Error("explicit invocation did not resolve SKILL.md")
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
				agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					last := req.Dialog[len(req.Dialog)-1]
					if last.Role != gai.ToolResult || !strings.Contains(last.Blocks[0].Content.String(), body) || !strings.Contains(last.Blocks[0].Content.String(), "Check passed") {
						t.Errorf("skill and reference not delivered through REPL: %+v", last)
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Reviewed")}}}, FinishReason: gai.EndTurn}},
			)
			opts := Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir, Skills: catalog}
			a, err := Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = a.Close() }()
			if err := a.Prompt(t.Context(), tc.prompt, nil); err != nil {
				t.Fatal(err)
			}
			accepted := a.Messages()[0].Blocks[0].Content.String()
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			// Replay must read recorded results even with the entire skill gone.
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			store, err = session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			opts.Store, opts.Skills = store, skills.Catalog{}
			opts.Generator = agenttest.NewScriptedGenerator()
			a, err = Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if a.Messages()[0].Blocks[0].Content.String() != accepted {
				t.Fatal("reopening re-expanded or lost the accepted invocation")
			}
			result, err := a.repl.Eval(t.Context(), "restored-skill", "print(skill_text, skill_check)")
			if err != nil || result.Error != "" || !strings.Contains(result.Output, body) || !strings.Contains(result.Output, "Check passed") {
				t.Fatalf("skill state not restored: %+v, %v", result, err)
			}
			effect, err := os.ReadFile(filepath.Join(dir, "effect"))
			if err != nil || string(effect) != "once\n" {
				t.Fatalf("skill effect repeated: %q, %v", effect, err)
			}
		})
	}
}
