package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func defaultFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"default_model":"alpha","models":{"alpha":{"provider":"codex","id":"a","reasoning_effort":"low"},"beta":{"provider":"codex","id":"b","reasoning_effort":"high"}},"tui":{"submit_key":"shift+enter"},"agent":{"max_rounds":7}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSaveDefaultModel(t *testing.T) {
	for _, test := range []struct {
		name, model                string
		symlink, canceled, invalid bool
	}{
		{name: "change", model: "beta"}, {name: "same", model: "alpha"},
		{name: "symlink", model: "beta", symlink: true}, {name: "unknown", model: "missing", invalid: true},
		{name: "canceled", model: "beta", canceled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := defaultFixture(t)
			path := filepath.Join(dir, "config.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if test.symlink {
				target := filepath.Join(t.TempDir(), "config.json")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skip(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.canceled {
				cancel()
			}
			c, err := SaveDefaultModel(ctx, dir, test.model)
			if (err != nil) != (test.invalid || test.canceled) {
				t.Fatal(err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err != nil || test.model == "alpha" {
				if string(before) != string(after) {
					t.Fatal("failed/no-op selection changed config")
				}
				return
			}
			if c.DefaultModel != test.model || c.Models["alpha"].ReasoningEffort != "low" || c.Models["beta"].ReasoningEffort != "high" || c.TUI.SubmitKey != SubmitShiftEnter || c.Agent.MaxRounds != 7 {
				t.Fatalf("lost settings: %+v", c)
			}
			loaded, err := load(t.Context(), dir)
			if err != nil || loaded.DefaultModel != test.model {
				t.Fatal(loaded, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0640 {
				t.Fatal("permissions", err)
			}
			if test.symlink {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("symlink replaced", err)
				}
			}
		})
	}
}

func TestSaveReasoning(t *testing.T) {
	for _, test := range []struct {
		name, model, effort string
		alias               bool
		invalid             bool
	}{
		{"nondefault profile", "beta", "low", false, false}, {"omit", "beta", "", false, false},
		{"explicit none", "beta", "none", false, false}, {"same", "beta", "high", false, false},
		{"default profile", "alpha", "high", false, false}, {"unknown model", "missing", "high", false, true},
		{"unknown effort", "beta", "invalid", false, true},
		{"aliased models save", "beta", "high", true, true},
		{"aliased models remove", "beta", "", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := defaultFixture(t)
			path := filepath.Join(dir, "config.json")
			if test.alias {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"models"`, `"Models"`, 1)), 0640); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c, err := SaveReasoning(t.Context(), dir, test.model, test.effort)
			if (err != nil) != test.invalid {
				t.Fatal(err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if test.invalid {
				if string(before) != string(after) {
					t.Fatal("failed selection changed file")
				}
				return
			}
			if c.DefaultModel != "alpha" || c.Models[test.model].ReasoningEffort != test.effort || c.TUI.SubmitKey != SubmitShiftEnter || c.Agent.MaxRounds != 7 {
				t.Fatalf("settings=%+v", c)
			}
			if test.effort == "" && strings.Count(string(after), "reasoning_effort") != 1 {
				t.Fatal("omitted effort still serialized")
			}
			loaded, err := load(t.Context(), dir)
			if err != nil || loaded.Models[test.model].ReasoningEffort != test.effort || loaded.DefaultModel != "alpha" {
				t.Fatal(loaded, err)
			}
		})
	}
}

func TestConcurrentConfigEdits(t *testing.T) {
	dir := defaultFixture(t)
	var wg sync.WaitGroup
	for _, edit := range []func() error{
		func() error { _, err := SaveDefaultModel(t.Context(), dir, "beta"); return err },
		func() error { _, err := SaveReasoning(t.Context(), dir, "alpha", "max"); return err },
		func() error {
			_, err := AddModels(t.Context(), dir, map[string]Model{"gamma": {Provider: "codex", ID: "g"}})
			return err
		},
	} {
		wg.Go(func() {
			if err := edit(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	c, err := load(t.Context(), dir)
	if err != nil || c.DefaultModel != "beta" || c.Models["alpha"].ReasoningEffort != "max" || c.Models["gamma"].ID != "g" {
		t.Fatal(c, err)
	}
}
