package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinsNeedNoUserDefinitions(t *testing.T) {
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
}

func TestCustomThemeCanDeriveFromPreset(t *testing.T) {
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
}

func TestExistingDesktopAndUserThemeNamesArePreserved(t *testing.T) {
	dir := t.TempDir()
	body := `{"active":"nord","themes":{"nord":{"source":"terminal","colors":{"accent":"2"}},"desktop":{"source":"auto"}}}`
	if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := load(dir, t.TempDir(), Appearance{})
	if err != nil || got.Name != nordTheme || got.Source != terminalSource || got.Colors.Accent != "2" {
		t.Fatalf("built-in must not replace a user definition: %+v %v", got, err)
	}
	for _, body := range []string{
		`{"active":"unknown-preset"}`,
		`{"active":"work","themes":{"work":{"source":"nord","palette_file":"unused.toml"}}}`,
		`{"active":"work","themes":{"work":{"source":"dracula","colors":{"accent":"$typo"}}}}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := load(dir, t.TempDir(), Appearance{}); err == nil {
			t.Fatalf("invalid preset configuration accepted: %s", body)
		}
	}
}
