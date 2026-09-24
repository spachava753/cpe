package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const darkPalette = `{"background":"#101315","foreground":"#cacccc","accent":"#798186","muted":"#4b4e55","selection":"#343d41","bright_foreground":"#a5aeb4","bright_red":"#de6145","yellow":"#d9dbdc","cyan":"#707070"}`

func TestLoad(t *testing.T) {
	t.Run("system appearance", func(t *testing.T) {
		dir, home := t.TempDir(), t.TempDir()
		// Even a previous installation's palette must have no influence on detection.
		old := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
		if err := os.MkdirAll(old, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(old, "colors.toml"), []byte("invalid TOML"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct{ name, body string }{
			{"starter", StarterJSON},
			{"builtin", `{"active":"desktop"}`},
			{"auto alias", `{"active":"desktop","themes":{"desktop":{"source":"auto"}}}`},
		} {
			t.Run(test.name, func(t *testing.T) {
				body := test.body
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				got, err := load(dir, home, Appearance{})
				if err != nil || got != Default() {
					t.Fatalf("unknown appearance must inherit terminal: %+v %v", got, err)
				}
				for _, mode := range []string{lightTheme, darkTheme} {
					t.Run(mode, func(t *testing.T) {
						got, err := load(dir, home, Appearance{Mode: mode, Accent: "#8877cc"})
						if err != nil || got.Source != systemSource+"/"+mode || got.PaletteFile != "" || !hexColor.MatchString(got.Colors.Background) {
							t.Fatalf("system appearance: %+v %v", got, err)
						}
						if contrast(got.Colors.Accent, got.Colors.Background) < 4.5 || contrast(got.Colors.SelectionForeground, got.Colors.SelectionBackground) < 4.5 {
							t.Fatalf("illegible derived accent: %+v", got)
						}
					})
				}
			})
		}

		if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(`{"active":"terminal"}`), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := load(dir, home, Appearance{Mode: lightTheme})
		if err != nil || got.Source != terminalSource || got.Colors != Default().Colors {
			t.Fatalf("explicit terminal must ignore OS preference: %+v %v", got, err)
		}
	})
	t.Run("builtin presets", func(t *testing.T) {
		for _, test := range []struct{ name, background, foreground, accent string }{
			{lightTheme, "#fafafa", "#25262b", "#2864c5"},
			{darkTheme, "#181a1f", "#dcdfe4", "#61afef"},
			{nordTheme, "#2e3440", "#d8dee9", "#88c0d0"},
			{draculaTheme, "#282a36", "#f8f8f2", "#bd93f9"},
			{gruvboxTheme, "#282828", "#ebdbb2", "#83a598"},
		} {
			t.Run(test.name, func(t *testing.T) {
				dir := t.TempDir()
				body := fmt.Sprintf(`{"active":%q}`, test.name)
				if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				got, err := load(dir, t.TempDir(), Appearance{})
				if err != nil {
					t.Fatal(err)
				}
				if got.Name != test.name || got.Source != test.name || got.PaletteFile != "" || got.Colors.Background != test.background || got.Colors.Foreground != test.foreground || got.Colors.Accent != test.accent || got.InputHeight != 3 || !got.Bold {
					t.Fatalf("incorrect preset: %+v", got)
				}
				for role, color := range got.Colors.roles() {
					if !hexColor.MatchString(*color) {
						t.Fatalf("%s must have a fixed RGB value, got %q", role, *color)
					}
				}
			})
		}
	})
	t.Run("preset references", func(t *testing.T) {
		dir := t.TempDir()
		body := `{"active":"work","themes":{"work":{"source":"nord","colors":{"accent":"#123456","assistant":"$accent","error":"$red","user":"$color2"},"input_height":4}}}`
		if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := load(dir, t.TempDir(), Appearance{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "work" || got.Source != nordTheme || got.Colors.Accent != "#123456" || got.Colors.Assistant != "#88c0d0" || got.Colors.Error != "#bf616a" || got.Colors.User != "#a3be8c" || got.InputHeight != 4 {
			t.Fatalf("preset references must use the original palette: %+v", got)
		}
	})
	t.Run("custom builtin overrides", func(t *testing.T) {
		dir := t.TempDir()
		body := `{"active":"nord","themes":{"nord":{"source":"terminal","colors":{"accent":"2"}},"desktop":{"source":"auto"}}}`
		if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := load(dir, t.TempDir(), Appearance{})
		if err != nil || got.Name != nordTheme || got.Source != terminalSource || got.Colors.Accent != "2" {
			t.Fatalf("built-in must not replace a user definition: %+v %v", got, err)
		}
	})
	t.Run("invalid configuration", func(t *testing.T) {
		for _, test := range []struct{ name, body string }{
			{"null", `null`},
			{"array", `[]`},
			{"missing active", `{}`},
			{"trailing data", StarterJSON + `{}`},
			{"duplicate active", strings.Replace(StarterJSON, `"active":`, `"active":"other","active":`, 1)},
			{"unknown field", `{"active":"a","themes":{"a":{"source":"terminal","font_size":16}}}`},
			{"unknown source", `{"active":"a","themes":{"a":{"source":"other"}}}`},
			{"zero height", `{"active":"a","themes":{"a":{"source":"terminal","input_height":0}}}`},
			{"excessive height", `{"active":"a","themes":{"a":{"source":"terminal","input_height":21}}}`},
			{"named color", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"red"}}}}`},
			{"color index out of range", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"256"}}}}`},
			{"short hex color", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"#12345"}}}}`},
			{"unknown palette reference", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"$absent"}}}}`},
			{"terminal escape color", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"\u001b[31m"}}}}`},
			{"duplicate color", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"1","accent":"2"}}}}`},
			{"unknown role", `{"active":"a","themes":{"a":{"source":"terminal","colors":{"typo":"2"}}}}`},
			{"unexpected palette path", `{"active":"a","themes":{"a":{"source":"terminal","palette_file":"missing.json"}}}`},
			{"missing palette path", `{"active":"a","themes":{"a":{"source":"file"}}}`},
			{"palette path on auto source", `{"active":"a","themes":{"a":{"source":"auto","palette_file":"missing.json"}}}`},
			{"unknown preset", `{"active":"unknown-preset"}`},
			{"palette path on preset", `{"active":"work","themes":{"work":{"source":"nord","palette_file":"unused.toml"}}}`},
			{"unknown preset reference", `{"active":"work","themes":{"work":{"source":"dracula","colors":{"accent":"$typo"}}}}`},
		} {
			t.Run(test.name, func(t *testing.T) {
				body := test.body
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := load(dir, t.TempDir(), Appearance{}); err == nil {
					t.Fatal("invalid theme accepted")
				}
			})
		}
	})
}

func TestLoadReloadWorkflow(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	configPath := filepath.Join(dir, "themes.json")
	body := `{"active":"custom","themes":{"custom":{"source":"file","palette_file":"current.json","colors":{"assistant":"$accent","background":"default","error":"#ff0000","user":"$color6"},"bold":false,"italic":true,"input_height":5}}}`
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dark.json", "light.json"} {
		data := darkPalette
		if name == "light.json" {
			data = strings.ReplaceAll(data, "#798186", "#0055aa")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(dir, "current.json")
	if err := os.Symlink("dark.json", current); err != nil {
		t.Fatal(err)
	}
	got, err := load(dir, home, Appearance{})
	if err != nil || got.Colors.Assistant != "#798186" || got.Colors.User != "#707070" || got.Colors.Background != "default" || got.Colors.Error != "#ff0000" || got.Bold || !got.Italic || got.InputHeight != 5 {
		t.Fatalf("overrides: %+v %v", got, err)
	}
	if err := os.Symlink("light.json", current+".next"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(current+".next", current); err != nil {
		t.Fatal(err)
	}
	got, err = load(dir, home, Appearance{})
	if err != nil || got.Colors.Assistant != "#0055aa" || got.Colors.Error != "#ff0000" {
		t.Fatalf("replaced symlink: %+v %v", got, err)
	}
	body = `{"active":"plain","themes":{"plain":{"source":"terminal","colors":{"accent":"5"}}}}`
	if err := os.WriteFile(configPath+".next", []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(configPath+".next", configPath); err != nil {
		t.Fatal(err)
	}
	got, err = load(dir, home, Appearance{})
	if err != nil || got.Source != terminalSource || got.Name != "plain" || got.Colors.Accent != "5" {
		t.Fatalf("replaced config: %+v %v", got, err)
	}
}

func TestFilePalette(t *testing.T) {
	for _, test := range []struct {
		name, data string
		wantErr    bool
	}{
		{"empty", "", true},
		{"null", "null", true},
		{"array", "[]", true},
		{"missing colors", "{}", true},
		{"missing foreground", `{"background":"#123456"}`, true},
		{"invalid color", `{"background":"#123456","foreground":"invalid"}`, true},
		{"duplicate color", `{"background":"#123456","background":"#ffffff"}`, true},
		{"wrong value type", `{"foreground":"#ffffff","background":"#111111","red":42}`, true},
		{"semantic colors", `{"foreground":"#ffffff","background":"#111111"}`, false},
		{"short aliases", `{"fg":"#ffffff","bg":"#111111"}`, false},
		{"ANSI aliases", `{"color7":"#ffffff","color0":"#111111"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := filePalette([]byte(test.data))
			if (err != nil) != test.wantErr {
				t.Fatalf("filePalette() error = %v, want error %t", err, test.wantErr)
			}
			if err == nil && (got[keyForeground] != "#ffffff" || got[keyBackground] != "#111111") {
				t.Fatalf("resolved palette = %v", got)
			}
		})
	}
}
