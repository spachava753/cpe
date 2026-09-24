package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/theme"
)

func TestInitFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".cpe")
	if _, err := initFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "system.md", themesFile} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s must be private: %v %v", name, info, err)
		}
		if name == themesFile {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != theme.StarterJSON {
				t.Fatalf("theme starter: %s %v", data, err)
			}
		}
		if err := os.WriteFile(path, []byte("existing user content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := initFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "system.md", themesFile} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "existing user content" {
			t.Fatalf("init overwrote %s: %v", name, err)
		}
	}
}

func TestLoad(t *testing.T) {
	t.Run("reasoning across providers", func(t *testing.T) {
		for _, provider := range []string{"openai", "responses", "anthropic", "gemini", "codex"} {
			t.Run(provider, func(t *testing.T) {
				efforts := []string{"", "high"}
				if provider == "anthropic" {
					efforts = append(efforts, "adaptive", "disabled")
				}
				for _, effort := range efforts {
					name := effort
					if name == "" {
						name = "omitted"
					}
					t.Run(name, func(t *testing.T) {
						dir := t.TempDir()
						profile := `"provider":"` + provider + `","id":"custom-model","reasoning_effort":"` + effort + `"`
						if provider != "codex" {
							profile += `,"api_key_env":"FIXTURE_KEY","base_url":"https://compatible.example/v1"`
						}
						body := `{"default_model":"fixture","models":{"fixture":{` + profile + `}}}`
						if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("test"), 0600); err != nil {
							t.Fatal(err)
						}
						cfg, err := load(dir)
						if err != nil || cfg.Models["fixture"].ReasoningEffort != effort {
							t.Fatalf("profile=%+v err=%v", cfg.Models["fixture"], err)
						}
					})
				}
			})
		}
	})
	t.Run("saved credentials", func(t *testing.T) {
		for _, test := range []struct {
			name, fields string
			invalid      bool
		}{
			{"Go chat", `"provider":"openai","credential":"opencode-go"`, false},
			{"Go responses", `"provider":"responses","credential":"opencode-go"`, false},
			{"Go messages", `"provider":"anthropic","credential":"opencode-go"`, false},
			{"unknown credential", `"provider":"openai","credential":"unknown"`, true},
			{"Go Gemini", `"provider":"gemini","credential":"opencode-go"`, true},
			{"Go Codex", `"provider":"codex","credential":"opencode-go"`, true},
			{"ambiguous credentials", `"provider":"openai","credential":"opencode-go","api_key_env":"KEY"`, true},
			{"endpoint override", `"provider":"openai","credential":"opencode-go","base_url":"https://example.com"`, true},
		} {
			t.Run(test.name, func(t *testing.T) {
				dir := t.TempDir()
				body := `{"default_model":"go","models":{"go":{"id":"fixture",` + test.fields + `}}}`
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
				_, err := load(dir)
				if (err != nil) != test.invalid {
					t.Fatalf("load error: %v", err)
				}
			})
		}
	})
	t.Run("MCP transports", func(t *testing.T) {
		for _, test := range []struct {
			name, server string
			invalid      bool
		}{
			{"stdio", `{"command":"fixture","args":["--stdio"],"env":{"MODE":"test"}}`, false},
			{"HTTP", `{"url":"http://127.0.0.1:8000/mcp"}`, false},
			{"HTTPS", `{"url":"https://example.com/mcp"}`, false},
			{"empty", `{}`, true},
			{"null", `null`, true},
			{"both", `{"command":"fixture","url":"https://example.com/mcp"}`, true},
			{"wrong scheme", `{"url":"file:///tmp/mcp"}`, true},
			{"URL credentials", `{"url":"https://user:secret@example.com/mcp"}`, true},
			{"fragment", `{"url":"https://example.com/mcp#fragment"}`, true},
			{"args without command", `{"url":"https://example.com/mcp","args":["arg"]}`, true},
			{"env without command", `{"url":"https://example.com/mcp","env":{"X":"Y"}}`, true},
			{"unknown field", `{"command":"fixture","typo":true}`, true},
		} {
			t.Run(test.name, func(t *testing.T) {
				dir := t.TempDir()
				body := strings.Replace(defaultJSON, "{", `{"mcp_servers":{"fixture":`+test.server+`},`, 1)
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(defaultSystem), 0600); err != nil {
					t.Fatal(err)
				}
				c, err := load(dir)
				if (err != nil) != test.invalid {
					t.Fatalf("config=%+v err=%v", c, err)
				}
				if !test.invalid && len(c.MCPServers) != 1 {
					t.Fatal("MCP server lost during loading")
				}
			})
		}
	})
	t.Run("system markdown", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(defaultJSON), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("Custom instructions\n"), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if c.System != "Custom instructions\n" || c.Agent.ToolTimeout != "1m" || c.Models["default"].Provider != "codex" {
			t.Fatal(c)
		}
	})
	t.Run("invalid configuration", func(t *testing.T) {
		for _, test := range []struct{ name, body string }{
			{"unknown", strings.Replace(defaultJSON, "max_rounds", "max_round", 1)},
			{"duplicate", strings.Replace(defaultJSON, `"max_rounds": 50`, `"max_rounds": 50, "max_rounds": 3`, 1)},
			{"trailing", defaultJSON + `{}`},
			{"null", `null`}, {"array", `[]`}, {"truncated", `{"models":`},
			{"default", strings.Replace(defaultJSON, `"default_model": "default"`, `"default_model": "missing"`, 1)},
			{"timeout", strings.Replace(defaultJSON, `"tool_timeout": "1m"`, `"tool_timeout": "0s"`, 1)},
			{"limit", strings.Replace(defaultJSON, `"output_limit": 32000`, `"output_limit": -1`, 1)},
			{"provider", strings.Replace(defaultJSON, `"provider": "codex"`, `"provider": "unknown"`, 1)},
			{"effort", strings.Replace(defaultJSON, `"reasoning_effort": "low"`, `"reasoning_effort": "unknown"`, 1)},
			{"old-pi-file", strings.Replace(defaultJSON, `"reasoning_effort": "low"`, `"oauth_file": "~/.pi/agent/auth.json"`, 1)},
			{"codex-api-key", strings.Replace(defaultJSON, `"reasoning_effort": "low"`, `"api_key_env": "KEY"`, 1)},
			{"codex-endpoint", strings.Replace(defaultJSON, `"reasoning_effort": "low"`, `"base_url": "https://example.com"`, 1)},
			{"codex-output", strings.Replace(defaultJSON, `"reasoning_effort": "low"`, `"max_output_tokens": 100`, 1)},
			{"codex-temperature", strings.Replace(defaultJSON, `"reasoning_effort": "low"`, `"temperature": 0.2`, 1)},
			{"api-missing-key", `{"default_model":"a","models":{"a":{"provider":"openai","id":"test"}}}`},
		} {
			t.Run(test.name, func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(test.body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(defaultSystem), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := load(dir); err == nil {
					t.Fatal("invalid configuration accepted")
				}
			})
		}
	})
	t.Run("API profile defaults", func(t *testing.T) {
		dir := t.TempDir()
		body := `{"default_model":"api","models":{"api":{"provider":"responses","id":"test","api_key_env":"TEST_KEY","base_url":"https://example.com/v1","reasoning_effort":"high","max_output_tokens":8192}}}`
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(defaultSystem), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if c.Agent.MaxRounds != 50 || c.Models["api"].APIKeyEnv != "TEST_KEY" || c.Compaction.Prompt == "" {
			t.Fatal("missing defaults or profile fields")
		}
	})
	t.Run("context and pricing", func(t *testing.T) {
		for _, test := range []struct {
			name, settings string
			valid          bool
		}{
			{"budget", `"context_window":272000`, true},
			{"negative-budget", `"context_window":-1`, false},
			{"rates", `"cost":{"input":2,"output":10,"cache_read":0.2,"cache_write":2.5}`, true},
			{"free", `"cost":{"input":0,"output":0,"cache_read":0,"cache_write":0}`, true},
			{"missing-rate", `"cost":{"input":2,"output":10,"cache_read":0.2}`, false},
			{"negative-rate", `"cost":{"input":-1,"output":10,"cache_read":0.2,"cache_write":2.5}`, false},
			{"null-rate", `"cost":{"input":null,"output":10,"cache_read":0.2,"cache_write":2.5}`, false},
			{"tier", `"cost":{"input":2,"output":10,"cache_read":0.2,"cache_write":2.5,"long_context":{"above_input_tokens":272000,"input":4,"output":15,"cache_read":0.4,"cache_write":5}}`, true},
			{"missing-tier-threshold", `"cost":{"input":2,"output":10,"cache_read":0.2,"cache_write":2.5,"long_context":{"input":4,"output":15,"cache_read":0.4,"cache_write":5}}`, false},
		} {
			t.Run(test.name, func(t *testing.T) {
				dir := t.TempDir()
				body := `{"default_model":"metered","models":{"metered":{"provider":"codex","id":"usage-model",` + test.settings + `}}}`
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(defaultSystem), 0600); err != nil {
					t.Fatal(err)
				}
				_, err := load(dir)
				if (err == nil) != test.valid {
					t.Fatalf("valid=%t err=%v", test.valid, err)
				}
			})
		}
	})
}
