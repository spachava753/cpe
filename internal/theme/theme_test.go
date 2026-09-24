package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const darkPalette = `{"background":"#101315","foreground":"#cacccc","accent":"#798186","muted":"#4b4e55","selection":"#343d41","bright_foreground":"#a5aeb4","bright_red":"#de6145","yellow":"#d9dbdc","cyan":"#707070"}`

func TestGenericDesktopUsesAppearanceWithoutDesktopFiles(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	// Even a previous installation's palette must have no influence on detection.
	old := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "colors.toml"), []byte("invalid TOML"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{StarterJSON, `{"active":"desktop"}`, `{"active":"desktop","themes":{"desktop":{"source":"auto"}}}`} {
		if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := load(dir, home, Appearance{})
		if err != nil || got != Default() {
			t.Fatalf("unknown appearance must inherit terminal: %+v %v", got, err)
		}
		for _, mode := range []string{lightTheme, darkTheme} {
			got, err := load(dir, home, Appearance{Mode: mode, Accent: "#8877cc"})
			if err != nil || got.Source != systemSource+"/"+mode || got.PaletteFile != "" || !hexColor.MatchString(got.Colors.Background) {
				t.Fatalf("system appearance: %+v %v", got, err)
			}
			if contrast(got.Colors.Accent, got.Colors.Background) < 4.5 || contrast(got.Colors.SelectionForeground, got.Colors.SelectionBackground) < 4.5 {
				t.Fatalf("illegible derived accent: %+v", got)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(`{"active":"terminal"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := load(dir, home, Appearance{Mode: lightTheme})
	if err != nil || got.Source != terminalSource || got.Colors != Default().Colors {
		t.Fatalf("explicit terminal must ignore OS preference: %+v %v", got, err)
	}
}

func TestOverridesFollowPaletteSymlinkAndAtomicConfigChanges(t *testing.T) {
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

func TestInvalidThemeFiles(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{}`, StarterJSON + `{}`, strings.Replace(StarterJSON, `"active":`, `"active":"other","active":`, 1),
		`{"active":"a","themes":{"a":{"source":"terminal","font_size":16}}}`,
		`{"active":"a","themes":{"a":{"source":"other"}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","input_height":0}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","input_height":21}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"red"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"256"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"#12345"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"$absent"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"\u001b[31m"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"accent":"1","accent":"2"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","colors":{"typo":"2"}}}}`,
		`{"active":"a","themes":{"a":{"source":"terminal","palette_file":"missing.json"}}}`,
		`{"active":"a","themes":{"a":{"source":"file"}}}`,
		`{"active":"a","themes":{"a":{"source":"auto","palette_file":"missing.json"}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := load(dir, t.TempDir(), Appearance{}); err == nil {
				t.Fatal("invalid theme accepted")
			}
		})
	}
}

func TestInvalidFilePalette(t *testing.T) {
	for _, data := range []string{"", "null", "[]", "{}", `{"background":"#123456"}`, `{"background":"#123456","foreground":"invalid"}`, `{"background":"#123456","background":"#ffffff"}`, `{"foreground":"#ffffff","background":"#111111","red":42}`} {
		if _, err := filePalette([]byte(data)); err == nil {
			t.Fatalf("accepted invalid palette %q", data)
		}
	}
}
