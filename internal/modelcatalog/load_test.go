package modelcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	json := `[
	  {"name":"claude","id":"claude-3-5-sonnet","type":"anthropic","api_key_env":"ANTHROPIC_API_KEY","context_window":200000,"max_output":8192},
	  {"name":"gpt4o","id":"gpt-4o","type":"openai","api_key_env":"OPENAI_API_KEY"}
	]`
	p := writeTemp(t, dir, "models.json", json)
	ms, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("expected 2 models, got %d", len(ms))
	}
	if ms[0].Name != "claude" || ms[1].Name != "gpt4o" {
		t.Fatalf("unexpected names: %+v", ms)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, "bad.json", `{"not":"array"}`)
	if _, err := Load(p); err == nil {
		t.Fatalf("expected error for invalid JSON array")
	}
}

func TestValidate_DuplicateName(t *testing.T) {
	ms := []Model{{Name: "a", ID: "id1", Type: "openai"}, {Name: "a", ID: "id2", Type: "openai"}}
	if err := Validate(ms); err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestValidate_BadType(t *testing.T) {
	ms := []Model{{Name: "a", ID: "id1", Type: "nope"}}
	if err := Validate(ms); err == nil {
		t.Fatalf("expected bad type error")
	}
}

func TestValidate_ReasoningCleared(t *testing.T) {
	ms := []Model{{Name: "a", ID: "id1", Type: "openai", SupportsReasoning: false, DefaultReasoningEffort: "medium"}}
	if err := Validate(ms); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms[0].DefaultReasoningEffort != "" {
		t.Fatalf("expected DefaultReasoningEffort cleared, got %q", ms[0].DefaultReasoningEffort)
	}
}
