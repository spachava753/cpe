package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAddModels(t *testing.T) {
	for _, test := range []struct {
		name                       string
		symlink, canceled, invalid bool
	}{
		{name: "preserve existing settings"}, {name: "follow config symlink", symlink: true},
		{name: "canceled", canceled: true}, {name: "invalid addition", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := initFiles(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			original, err := load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if test.symlink {
				target := filepath.Join(t.TempDir(), "my-config.json")
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
			addition := Model{Provider: "anthropic", ID: "fixture", Credential: "opencode-go", ContextWindow: 128000, MaxOutputTokens: 16384}
			if test.invalid {
				addition.Credential = "unsupported"
			}
			models, err := AddModels(ctx, dir, map[string]Model{"default": addition, "opencode-go/fixture": addition})
			if (err != nil) != (test.canceled || test.invalid) {
				t.Fatalf("import error: %v", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if test.canceled || test.invalid {
				if string(before) != string(after) {
					t.Fatal("failed import changed config")
				}
				return
			}
			want := map[string]Model{"default": original.Models["default"], "opencode-go/fixture": addition}
			if !reflect.DeepEqual(models, want) {
				t.Fatalf("profiles %+v", models)
			}
			reloaded, err := load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.DefaultModel != original.DefaultModel || reloaded.Agent != original.Agent || reloaded.Compaction != original.Compaction || reloaded.System != original.System {
				t.Fatal("import changed existing settings")
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(after, &document); err != nil {
				t.Fatal(err)
			}
			var oldDocument map[string]json.RawMessage
			if err := json.Unmarshal(before, &oldDocument); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"agent", "compaction", "default_model"} {
				var oldValue, newValue any
				if err := json.Unmarshal(oldDocument[field], &oldValue); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(document[field], &newValue); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(oldValue, newValue) {
					t.Fatalf("%s changed", field)
				}
			}
			if test.symlink {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("config symlink replaced", err)
				}
			}
			if _, err := AddModels(t.Context(), dir, map[string]Model{"opencode-go/fixture": {Provider: "openai", ID: "replacement"}}); err != nil {
				t.Fatal(err)
			}
			repeated, err := os.ReadFile(path)
			if err != nil || string(repeated) != string(after) {
				t.Fatal("existing profile overwritten or file rewritten")
			}
		})
	}
}
