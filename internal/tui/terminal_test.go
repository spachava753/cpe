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
	in, out, read, write := 2.0, 10.0, 0.2, 2.5
	pricing := &config.Pricing{Rates: config.Rates{Input: &in, Output: &out, CacheRead: &read, CacheWrite: &write}}
	profiles := map[string]config.Model{
		"terminal-fixture":  {Provider: "codex", ID: "terminal-fixture", ReasoningEffort: lowEffort},
		"alternate":         {Provider: "codex", ID: "alternate-fixture", ReasoningEffort: "medium"},
		"anthropic-fixture": {Provider: "anthropic", ID: "anthropic-fixture", APIKeyEnv: "CPE_FIXTURE_KEY"},
		"api-fixture":       {Provider: "openai", ID: "api-fixture", APIKeyEnv: "CPE_FIXTURE_KEY"},
	}
	for name, profile := range profiles {
		profile.Cost, profile.ContextWindow = pricing, 272000
		profiles[name] = profile
	}
	// Defaults are persisted in the fixture directory and reused on restart.
	configPath := filepath.Join(dir, "config.json")
	initial := config.Config{DefaultModel: "terminal-fixture", Models: profiles, Agent: config.Agent{ToolTimeout: "5s", OutputLimit: 32000, MaxRounds: 5}, Compaction: config.Compaction{Prompt: "Summarize"}}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &initial); err != nil {
			t.Fatal(err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		data, err := json.Marshal(initial)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("Terminal test"), 0600); err != nil {
		t.Fatal(err)
	}
	profiles = initial.Models
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
	sessions := agent.NewSessions(agent.SessionOptions{
		Directory: filepath.Join(dir, "sessions"), CWD: dir, Skills: catalog,
		LoadConfig: func(context.Context) (config.Config, error) {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return config.Config{}, err
			}
			var cfg config.Config
			if err := json.Unmarshal(data, &cfg); err != nil {
				return cfg, err
			}
			cfg.Dir, cfg.System = dir, "Terminal test"
			return cfg, nil
		},
		NewGenerator: func(context.Context, config.Model, string) (gai.Generator, error) { return gen, nil },
	})
	defer sessions.Close()
	conversation, err := sessions.Open(t.Context(), agent.SessionStart{Path: os.Getenv("CPE_TUI_TEST_SESSION")})
	if err != nil {
		t.Fatal(err)
	}
	a := conversation.Agent
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
	options := Options{Sessions: sessions, ConfigDir: dir, Models: profiles, SubmitKey: submitKey, MouseMode: initial.TUI.MouseMode, PromptMode: initial.TUI.PromptMode,
		ThemeDir:      dir,
		NewGenerator:  sessions.Generator,
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
		LoginCopilot: func(ctx context.Context, notify func(string)) (map[string]config.Model, error) {
			notify("Sign in to GitHub Copilot\n\nOpen https://example.com/device\n\nEnter code: COPILOT-TEST\n\nTerminal fixture: waiting for authorization. Esc cancels.")
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(3 * time.Second):
			}
			return config.AddModels(ctx, dir, map[string]config.Model{"github-copilot/fixture": {Provider: "openai", Credential: "github-copilot", ID: "copilot-fixture", ContextWindow: 128000, MaxOutputTokens: 16384}})
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
	if err := Run(t.Context(), a, initial.DefaultModel, options); err != nil {
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
			if text == "handoff" {
				if !yield(gai.StreamChunk{Block: gai.TextBlock("Preparing handoff…\n")}) {
					return
				}
				select {
				case <-ctx.Done():
					yield(gai.StreamChunk{Err: ctx.Err()})
					return
				case <-time.After(5 * time.Second):
				}
			}
			g.next++
			code := `answer = 6 * 7; print(answer)`
			if text == "verbose" {
				code = `for i in range(60): print("Output fixture line %03d" % i)`
			}
			if text == "slow" {
				code = `load("time.star", "time"); time.sleep(3); answer = 6 * 7; print(answer)`
			}
			if strings.Contains(text, "restore") {
				code = `print("Restored answer:", answer)`
			}
			if text == "steer-tool" {
				code = `load("time.star", "time")
partial = [1]; partial.append(7)
load("os.star", "os")
fd = os.open("steering-effect", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
os.write(fd, "once\n"); os.close(fd)
print("partial ready")
time.sleep(10)
partial.append(999)`
			}
			if text == "restore-partial" {
				code = `print("Restored partial:", partial)`
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
		parts := []string{"The answer ", "is 42. ", "The Starlark state has been saved.", "\nHandled by " + req.Model}
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
