package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/testutil/testgate"
)

// TestTerminalHarness runs the real TUI against a deterministic local generator.
// Compile with go test -c ./internal/tui and run -test.run=TestTerminalHarness
// inside a PTY with CPE_RUN_INTEGRATION_TESTS=1 and CPE_TUI_TEST_DIR set.
// This keeps credentials and the user's ~/.cpe configuration out of UI tests.
func TestTerminalHarness(t *testing.T) {
	testgate.RequireIntegration(t)
	dir := os.Getenv("CPE_TUI_TEST_DIR")
	if dir == "" {
		t.Skip("set CPE_TUI_TEST_DIR to use the terminal harness")
	}
	store, err := session.Open(filepath.Join(dir, "terminal.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	in, out, read, write := 2.0, 10.0, 0.2, 2.5
	pricing := &config.Pricing{Rates: config.Rates{Input: &in, Output: &out, CacheRead: &read, CacheWrite: &write}}
	profiles := map[string]config.Model{
		"terminal-fixture": {Provider: "codex", ID: "terminal-fixture", ReasoningEffort: lowEffort},
		"alternate":        {Provider: "codex", ID: "alternate-fixture", ReasoningEffort: "medium"},
		"api-fixture":      {Provider: "openai", ID: "api-fixture"},
	}
	for name, profile := range profiles {
		profile.Cost, profile.ContextWindow = pricing, 272000
		profiles[name] = profile
	}
	skillRoot := filepath.Join(dir, "agents", "skills")
	for _, fixture := range []struct{ name, description, flags string }{
		{"review", "Review the current changes", ""},
		{"publish", "Publish only when explicitly requested", "disable-model-invocation: true\n"},
		{"background", "Background knowledge for the model", "user-invocable: false\n"},
	} {
		skillDir := filepath.Join(skillRoot, fixture.name)
		if err := os.MkdirAll(skillDir, 0700); err != nil {
			t.Fatal(err)
		}
		data := "---\nname: " + fixture.name + "\ndescription: " + fixture.description + "\n" + fixture.flags + "---\nTerminal skill fixture: " + fixture.name
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, warnings := skills.Discover(skillRoot)
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	gen := &terminalGenerator{skillRoot: skillRoot}
	a, err := agent.Open(t.Context(), agent.Options{
		Config: config.Config{System: "Terminal test", Agent: config.Agent{ToolTimeout: "5s", OutputLimit: 32000, MaxRounds: 5}, Compaction: config.Compaction{Prompt: "Summarize"}},
		Model:  profiles["terminal-fixture"], Generator: gen, Store: store, CWD: dir, Skills: catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	signedIn := false
	var submitKey config.SubmitKey
	if value := os.Getenv("CPE_TUI_TEST_SUBMIT_KEY"); value != "" {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &submitKey); err != nil {
			t.Fatal(err)
		}
	}
	options := Options{Models: profiles, SubmitKey: submitKey,
		ThemeDir:      dir,
		NewGenerator:  func(context.Context, config.Model) (gai.Generator, error) { return gen, nil },
		LoginRequired: func(profile config.Model) bool { return profile.Provider == "codex" && !signedIn },
		LoginGo: func(ctx context.Context, key string) (map[string]config.Model, error) {
			if key != "fixture-go-key" {
				return nil, errors.New("fixture expects fixture-go-key")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
			updated := maps.Clone(profiles)
			for _, provider := range []string{"openai", "responses", "anthropic"} {
				updated["opencode-go/"+provider] = config.Model{Provider: provider, ID: provider + "-fixture", Credential: "opencode-go", ContextWindow: 128000, MaxOutputTokens: 16384}
			}
			return updated, nil
		},
		Login: func(ctx context.Context, method string, notify func(string)) error {
			notify("Sign in to OpenAI Codex\n\nOpen https://example.com/device\n\nEnter code: CPE-TEST\n\nTerminal fixture: waiting for authorization. Esc cancels.")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(8 * time.Second):
				signedIn = true
				return nil
			}
		}}
	if err := Run(t.Context(), a, "terminal-fixture", options); err != nil {
		t.Fatal(err)
	}
}

type terminalGenerator struct {
	next      int
	skillRoot string
}

func (g *terminalGenerator) Generate(_ context.Context, _ gai.GenerationRequest) (gai.Response, error) {
	return gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("The user tested CPE. Persistent variable answer is 42.")}}}, FinishReason: gai.EndTurn}, nil
}
func (g *terminalGenerator) Stream(ctx context.Context, req gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return func(yield func(gai.StreamChunk) bool) {
		usage := gai.StreamChunk{Block: gai.MetadataBlock(gai.Metadata{gai.UsageMetricInputTokens: 1000, gai.UsageMetricGenerationTokens: 60, gai.UsageMetricCacheReadTokens: 800, gai.UsageMetricCacheWriteTokens: 100})}
		if len(req.Instructions.Blocks) > 0 && req.Instructions.Blocks[0].Content.String() == "Summarize" {
			if yield(gai.StreamChunk{Block: gai.TextBlock("The user tested CPE. Persistent variable answer is 42.")}) {
				yield(usage)
			}
			return
		}
		last := req.Dialog[len(req.Dialog)-1]
		if last.Role == gai.User {
			text := last.Blocks[0].Content.String()
			if text == "scroll" {
				for i := range 100 {
					select {
					case <-ctx.Done():
						yield(gai.StreamChunk{Err: ctx.Err()})
						return
					case <-time.After(75 * time.Millisecond):
					}
					if !yield(gai.StreamChunk{Block: gai.TextBlock(fmt.Sprintf("Scroll fixture line %03d\n", i))}) {
						return
					}
				}
				yield(usage)
				return
			}
			if strings.Contains(text, "wait") {
				if !yield(gai.StreamChunk{Block: gai.TextBlock("Waiting for cancellation…")}) {
					return
				}
				<-ctx.Done()
				yield(gai.StreamChunk{Err: ctx.Err()})
				return
			}
			g.next++
			code := `answer = 6 * 7; print(answer)`
			if text == "slow" {
				code = `load("time.star", "time"); time.sleep(3); answer = 6 * 7; print(answer)`
			}
			if strings.Contains(text, "restore") {
				code = `print("Restored answer:", answer)`
			}
			if strings.Contains(text, "image") {
				code = `load("repl.star", "emit_image"); emit_image("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII=", mime_type="image/png")`
			}
			if strings.HasPrefix(text, skills.CommandPrefix) {
				name := strings.TrimPrefix(strings.Fields(text)[0], skills.CommandPrefix)
				code = fmt.Sprintf("skill_file = open(%q)\nskill_text = skill_file.read()\nskill_file.close()\nprint(\"Skill instructions:\", skill_text)", filepath.Join(g.skillRoot, name, "SKILL.md"))
			}
			params, err := json.Marshal(map[string]any{"code": code})
			if err != nil {
				yield(gai.StreamChunk{Err: err})
				return
			}
			if !yield(gai.StreamChunk{Block: gai.Block{ID: fmt.Sprintf("terminal-%d-%d", len(req.Dialog), g.next), BlockType: gai.ToolCall, Content: gai.Str("starlark_repl")}}) {
				return
			}
			if yield(gai.StreamChunk{Block: gai.Block{BlockType: gai.ToolCall, Content: gai.Str(params)}}) {
				yield(usage)
			}
			return
		}
		parts := []string{"The answer ", "is 42. ", "The Starlark state has been saved."}
		if last.Role == gai.ToolResult && strings.Contains(last.Blocks[0].Content.String(), "Skill instructions:") {
			parts = []string{"Skill loaded through the REPL."}
		}
		for _, text := range parts {
			select {
			case <-ctx.Done():
				yield(gai.StreamChunk{Err: ctx.Err()})
				return
			case <-time.After(120 * time.Millisecond):
			}
			if !yield(gai.StreamChunk{Block: gai.TextBlock(text)}) {
				return
			}
		}
		yield(usage)
	}
}
