package theme

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spachava753/cpe/internal/jsonconfig"
)

// StarterJSON follows generic system appearance with terminal fallback.
// Fixed presets are built into CPE and need no entries in the themes object.
// Config initialization writes it only when themes.json does not already exist.
const StarterJSON = `{
  "active": "desktop",
  "themes": {
    "desktop": {
      "source": "system",
      "colors": {},
      "bold": true,
      "italic": false,
      "input_height": 3
    }
  }
}
`

const terminalSource = "terminal"

type configuration struct {
	Active string                `json:"active"`
	Themes map[string]definition `json:"themes"`
}

type definition struct {
	Source      string            `json:"source"`
	PaletteFile string            `json:"palette_file"`
	Colors      map[string]string `json:"colors"`
	Bold        *bool             `json:"bold"`
	Italic      bool              `json:"italic"`
	InputHeight *int              `json:"input_height"`
}

// colors assigns resolved terminal colors to UI roles. Values are #rrggbb,
// ANSI indices 0–255, or "default" to inherit the terminal's foreground/background.
type colors struct {
	Foreground, Background, Accent, Muted, Border, Error string
	User, Assistant, Tool                                string
	SelectionForeground, SelectionBackground             string
}

// Theme is a resolved immutable presentation snapshot. Name is the active JSON
// entry or built-in name, Source identifies terminal inheritance, a system light/dark palette, a file,
// or a fixed preset. PaletteFile identifies an explicitly configured JSON palette.
// Bold applies to headings/selection; Italic to muted text.
// InputHeight is the preferred editor height in terminal rows, clamped to fit.
type Theme struct {
	Name, Source, PaletteFile string
	Colors                    colors
	Bold, Italic              bool
	InputHeight               int
}

// Default returns a terminal-derived theme without reading any files.
func Default() Theme {
	return Theme{Name: "desktop", Source: terminalSource, Colors: colors{
		Foreground: keyDefault, Background: keyDefault, Accent: "4", Muted: "8",
		Border: "8", Error: "9", User: "6", Assistant: "4", Tool: "3",
		SelectionForeground: "0", SelectionBackground: "4",
	}, Bold: true, InputHeight: 3}
}

// Load resolves themes.json in dir using a built-in or custom theme definition.
// A missing file uses StarterJSON. Invalid configuration or an unreadable palette
// returns an error; callers should retain their previous valid theme on reload.
// System sources use the supplied snapshot and fall back to terminal colors
// when it is empty. Load itself never queries OS services.
// Relative palette_file paths resolve against dir; ~/ uses the user's home.
func Load(dir string, appearance Appearance) (Theme, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Theme{}, err
	}
	return load(dir, home, appearance)
}

func load(dir, home string, appearance Appearance) (Theme, error) {
	c, _, err := readConfiguration(dir)
	if err != nil {
		return Theme{}, err
	}
	return c.resolve(dir, home, appearance)
}

func readConfiguration(dir string) (configuration, []byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, "themes.json"))
	if errors.Is(err, os.ErrNotExist) {
		data = []byte(StarterJSON)
	} else if err != nil {
		return configuration{}, nil, err
	}
	var c configuration
	if err := jsonconfig.Decode(data, &c); err != nil {
		return configuration{}, nil, fmt.Errorf("themes.json: %w", err)
	}
	if c.Themes == nil {
		c.Themes = make(map[string]definition)
	}
	for name, d := range builtinDefinitions() {
		if _, exists := c.Themes[name]; !exists {
			c.Themes[name] = d
		}
	}
	for name, d := range c.Themes {
		if strings.TrimSpace(name) == "" {
			return configuration{}, nil, errors.New("themes.json: theme names must not be empty")
		}
		if err := d.validate(); err != nil {
			return configuration{}, nil, fmt.Errorf("themes.json theme %q: %w", name, err)
		}
	}
	return c, data, nil
}

func (c configuration) resolve(dir, home string, appearance Appearance) (Theme, error) {
	if _, ok := c.Themes[c.Active]; !ok || strings.TrimSpace(c.Active) == "" {
		return Theme{}, fmt.Errorf("themes.json: active must name a built-in or configured theme; unknown theme %q", c.Active)
	}
	d := c.Themes[c.Active]
	t := Default()
	t.Name = c.Active
	p := terminalPalette()
	if preset, ok := presetPalette(d.Source); ok {
		p, t.Source = preset, d.Source
	} else if d.Source == systemSource || d.Source == "auto" {
		if derived, ok := systemPalette(appearance); ok {
			p, t.Source = derived, systemSource+"/"+appearance.Mode
		}
	} else if d.Source == fileSource {
		path := d.PaletteFile
		if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Theme{}, fmt.Errorf("read palette: %w", err)
		}
		p, err = filePalette(data)
		if err != nil {
			return Theme{}, fmt.Errorf("palette %s: %w", path, err)
		}
		t.Source, t.PaletteFile = fileSource, path
	}

	roles := t.Colors.roles()
	for role, dest := range roles {
		*dest = p[role]
	}
	for role, color := range d.Colors {
		if strings.HasPrefix(color, "$") {
			var ok bool
			color, ok = p[color[1:]]
			if !ok || !validColor(color) {
				return Theme{}, fmt.Errorf("theme %q: unknown palette reference %q", c.Active, d.Colors[role])
			}
		}
		*roles[role] = color
	}
	if d.Bold != nil {
		t.Bold = *d.Bold
	}
	t.Italic = d.Italic
	if d.InputHeight != nil {
		t.InputHeight = *d.InputHeight
	}
	return t, nil
}

func (d definition) validate() error {
	if d.Source != "auto" && d.Source != systemSource && d.Source != fileSource && d.Source != terminalSource && !slices.Contains(presetNames, d.Source) {
		return errors.New("source must be system, terminal, file, light, dark, nord, dracula, gruvbox, or auto")
	}
	if (d.Source == fileSource) != (d.PaletteFile != "") {
		return errors.New("source file requires palette_file; other sources do not accept palette_file")
	}

	if d.InputHeight != nil && (*d.InputHeight < 1 || *d.InputHeight > 20) {
		return errors.New("input_height must be between 1 and 20 rows")
	}
	var c colors
	roles := c.roles()
	for role, color := range d.Colors {
		if _, ok := roles[role]; !ok {
			return fmt.Errorf("unknown color role %q", role)
		}
		if !validColor(color) && !reference.MatchString(color) {
			return fmt.Errorf("invalid color %q for %s: use #rrggbb, ANSI 0–255, default, or $palette_key", color, role)
		}
	}
	return nil
}

func (c *colors) roles() map[string]*string {
	return map[string]*string{
		keyForeground: &c.Foreground, keyBackground: &c.Background, keyAccent: &c.Accent,
		keyMuted: &c.Muted, keyBorder: &c.Border, keyError: &c.Error,
		keyUser: &c.User, keyAssistant: &c.Assistant, keyTool: &c.Tool,
		keySelectionForeground: &c.SelectionForeground, keySelectionBackground: &c.SelectionBackground,
	}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var reference = regexp.MustCompile(`^\$[a-zA-Z][a-zA-Z0-9_]*$`)

func validColor(s string) bool {
	if s == keyDefault || hexColor.MatchString(s) {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255 && strconv.Itoa(n) == s
}
