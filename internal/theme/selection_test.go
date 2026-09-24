package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestSelect(t *testing.T) {
	t.Run("create and persist builtins", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cpe")
		names, err := Names(dir)
		if err != nil || !slices.Equal(names, []string{"dark", "desktop", "dracula", "gruvbox", "light", "nord", "terminal"}) {
			t.Fatalf("built-in choices: %v %v", names, err)
		}
		for _, name := range names {
			selected, err := Select(dir, name, Appearance{})
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(dir, Appearance{})
			if err != nil || loaded != selected || loaded.Name != name {
				t.Fatalf("selection must survive reload: %+v %v", loaded, err)
			}
		}
		info, err := os.Stat(filepath.Join(dir, "themes.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("new config permissions: %v", info.Mode())
		}
	})
	t.Run("preserve custom definitions and symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "settings.json")
		path := filepath.Join(dir, "themes.json")
		original := `{"active":"desktop","themes":{"work theme":{"source":"nord","colors":{"accent":"#345678"},"bold":false,"italic":true,"input_height":5},"nord":{"source":"terminal","colors":{"accent":"2"}}}}`
		if err := os.WriteFile(target, []byte(original), 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		names, err := Names(dir)
		if err != nil || len(names) != 8 || !slices.Contains(names, "work theme") {
			t.Fatalf("custom choices: %v %v", names, err)
		}
		selected, err := Select(dir, "work theme", Appearance{})
		if err != nil || selected.Name != "work theme" || selected.Bold || !selected.Italic || selected.InputHeight != 5 || selected.Colors.Accent != "#345678" {
			t.Fatalf("custom selection: %+v %v", selected, err)
		}
		link, err := os.Readlink(path)
		if err != nil || link != target {
			t.Fatalf("symlink replaced: %q %v", link, err)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]any
		if err := json.Unmarshal([]byte(original), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &after); err != nil {
			t.Fatal(err)
		}
		before["active"] = "work theme"
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("selection changed definitions: %s", data)
		}
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0640 {
			t.Fatalf("existing config permissions changed: %v", info.Mode())
		}
		selected, err = Select(dir, nordTheme, Appearance{})
		if err != nil || selected.Source != terminalSource || selected.Colors.Accent != "2" {
			t.Fatalf("user preset override lost: %+v %v", selected, err)
		}
	})
	t.Run("failed selection leaves configuration intact", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "themes.json")
		for _, body := range []string{
			`{"active":"desktop","themes":{"broken":{"source":"file","palette_file":"missing.json"}}}`,
			`{"active":"desktop","active":"dark"}`,
			`{"active":`,
		} {
			for _, name := range []string{"unknown", "broken"} {
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Select(dir, name, Appearance{}); err == nil {
					t.Fatalf("invalid selection accepted: %s %s", name, body)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != body {
					t.Fatalf("failed selection changed config: %s %v", data, err)
				}
			}
		}
		// A broken active palette must not stop the user from choosing a working one.
		body := `{"active":"broken","themes":{"broken":{"source":"file","palette_file":"missing.json"}}}`
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		names, err := Names(dir)
		if err != nil || !slices.Contains(names, "broken") {
			t.Fatalf("cannot list with broken active palette: %v %v", names, err)
		}
		if _, err := Select(dir, lightTheme, Appearance{}); err != nil {
			t.Fatal(err)
		}
	})
}
