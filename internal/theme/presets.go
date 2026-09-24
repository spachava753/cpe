package theme

import (
	"fmt"
	"slices"
)

const (
	lightTheme   = "light"
	darkTheme    = "dark"
	nordTheme    = "nord"
	draculaTheme = "dracula"
	gruvboxTheme = "gruvbox"
)

var presetNames = []string{lightTheme, darkTheme, nordTheme, draculaTheme, gruvboxTheme}

func builtinDefinitions() map[string]definition {
	definitions := map[string]definition{"desktop": {Source: systemSource}, terminalSource: {Source: terminalSource}}
	for _, name := range presetNames {
		definitions[name] = definition{Source: name}
	}
	return definitions
}

// Fixed CPE role mappings based on the canonical Nord, Dracula, and Gruvbox
// palettes. Light and Dark are CPE's neutral defaults. These are embedded data,
// available without downloads or user configuration files.
// Sources: https://www.nordtheme.com/docs/colors-and-palettes/
// https://spec.draculatheme.com/ and https://github.com/morhetz/gruvbox.
func presetPalette(name string) (map[string]string, bool) {
	if !slices.Contains(presetNames, name) {
		return nil, false
	}
	var base [8]string // background, red, green, yellow, blue, magenta, cyan, foreground
	var muted, border, selection string
	switch name {
	case lightTheme:
		base = [8]string{"#fafafa", "#c72e38", "#397b36", "#986801", "#2864c5", "#8b3ca1", "#087d8b", "#25262b"}
		muted, border, selection = "#626873", "#d6d9df", "#dce7f7"
	case darkTheme:
		base = [8]string{"#181a1f", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#dcdfe4"}
		muted, border, selection = "#9aa2ad", "#3e4451", "#323641"
	case nordTheme:
		base = [8]string{"#2e3440", "#bf616a", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#88c0d0", "#d8dee9"}
		muted, border = base[4], "#434c5e"
		selection = border
	case draculaTheme:
		base = [8]string{"#282a36", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2"}
		muted, border = "#6272a4", "#44475a"
		selection = border
	case gruvboxTheme:
		base = [8]string{"#282828", "#fb4934", "#b8bb26", "#fabd2f", "#83a598", "#d3869b", "#8ec07c", "#ebdbb2"}
		muted, border = "#928374", "#504945"
		selection = border
	}
	bright := base
	bright[0] = muted
	if name == nordTheme {
		bright[7] = "#eceff4"
	}
	if name == draculaTheme {
		bright = [8]string{muted, "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff"}
	}
	p := make(map[string]string)
	for i, key := range []string{keyBackground, keyRed, keyGreen, keyYellow, keyBlue, keyMagenta, keyCyan, keyForeground} {
		p[key] = base[i]
		p[fmt.Sprintf("color%d", i)] = base[i]
		p[fmt.Sprintf("color%d", i+8)] = bright[i]
		if i > 0 {
			p["bright_"+key] = bright[i]
		}
	}
	p[keyAccent] = base[4]
	if name == nordTheme {
		p[keyAccent] = base[6]
	}
	p[keyMuted], p[keyBorder], p[keyError] = muted, border, base[1]
	p[keyUser], p[keyAssistant], p[keyTool] = base[6], p[keyAccent], base[3]
	p[keySelectionForeground], p[keySelectionBackground] = base[7], selection
	return p, true
}
