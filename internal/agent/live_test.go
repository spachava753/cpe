package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/testutil/testgate"
)

func TestLiveConfiguredAgent(t *testing.T) {
	testgate.RequireLive(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	model := cfg.Models[cfg.DefaultModel]
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "live.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen, err := Provider(t.Context(), model, cfg.Dir, store.Entries()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Open(t.Context(), Options{Config: cfg, Model: model, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "Use starlark_repl to calculate 6 * 7, print it, then reply with the result.", nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range a.Messages() {
		if m.Role == gai.ToolResult {
			for _, b := range m.Blocks {
				if b.Content != nil && strings.Contains(b.Content.String(), "42") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("model did not return 42 from the REPL")
	}
	usage := a.Usage()
	if usage.Requests == 0 || usage.Total() == 0 || usage.Unreported != 0 {
		t.Fatalf("provider did not return complete usage: %+v", usage)
	}
	t.Logf("reported usage: input=%d output=%d cache_read=%d cache_write=%d total=%d requests=%d estimated_usd=%.6f", usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite, usage.Total(), usage.Requests, usage.Cost)
}
