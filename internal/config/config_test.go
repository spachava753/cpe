package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONConfigurationAndSystemMarkdown(t *testing.T) {
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
}

func TestInvalidJSONConfiguration(t *testing.T) {
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
}

func TestAPIProfileAndDefaults(t *testing.T) {
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
}

func TestContextAndPricingConfiguration(t *testing.T) {
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
}
