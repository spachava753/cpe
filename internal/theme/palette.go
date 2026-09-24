package theme

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spachava753/cpe/internal/jsonconfig"
)

// Palette keys also name UI roles where their semantics coincide.
const (
	keyForeground          = "foreground"
	keyBackground          = "background"
	keyAccent              = "accent"
	keyMuted               = "muted"
	keyBorder              = "border"
	keyError               = "error"
	keyUser                = "user"
	keyAssistant           = "assistant"
	keyTool                = "tool"
	keySelectionForeground = "selection_foreground"
	keySelectionBackground = "selection_background"
	keyRed                 = "red"
	keyGreen               = "green"
	keyYellow              = "yellow"
	keyBlue                = "blue"
	keyMagenta             = "magenta"
	keyCyan                = "cyan"
	keyBrightRed           = "bright_red"
	keyBrightGreen         = "bright_green"
	keyBrightYellow        = "bright_yellow"
	keyBrightBlue          = "bright_blue"
	keyBrightMagenta       = "bright_magenta"
	keyBrightCyan          = "bright_cyan"
	keyBrightForeground    = "bright_foreground"
	keyDefault             = "default"
)

func terminalPalette() map[string]string {
	p := map[string]string{
		keyForeground: keyDefault, keyBackground: keyDefault, keyAccent: "4", keyMuted: "8",
		keyBorder: "8", keyError: "9", keyUser: "6", keyAssistant: "4", keyTool: "3",
		keySelectionForeground: "0", keySelectionBackground: "4",
		keyRed: "1", keyGreen: "2", keyYellow: "3", keyBlue: "4", keyMagenta: "5", keyCyan: "6",
		keyBrightRed: "9", keyBrightGreen: "10", keyBrightYellow: "11", keyBrightBlue: "12",
		keyBrightMagenta: "13", keyBrightCyan: "14", keyBrightForeground: "15",
	}
	for i := range 16 {
		p[fmt.Sprintf("color%d", i)] = strconv.Itoa(i)
	}
	return p
}

// Read data only. Theme files, hooks, and template commands are never executed.
func filePalette(data []byte) (map[string]string, error) {
	var raw map[string]string
	if err := jsonconfig.Decode(data, &raw); err != nil {
		return nil, err
	}
	p := map[string]string{}
	known := terminalPalette()
	for _, key := range []string{"bg", "fg", "dark_bg", "darker_bg", "lighter_bg", "dark_fg", "light_fg", "bright_fg", "dark_background", "darker_background", "lighter_background", "dark_foreground", "light_foreground", "selection", "purple", "bright_purple"} {
		known[key] = ""
	}
	for key, value := range raw {
		if hexColor.MatchString(value) {
			p[key] = value
		} else if _, ok := known[key]; ok {
			return nil, fmt.Errorf("%s must be a #rrggbb color", key)
		}
	}
	// Canonical semantic names take precedence over short and ANSI names.
	for _, pair := range [][2]string{
		{keyBackground, "bg"}, {keyForeground, "fg"},
		{keyBackground, "color0"}, {keyForeground, "color7"},
		{"dark_background", "dark_bg"}, {"darker_background", "darker_bg"},
		{"lighter_background", "lighter_bg"}, {"dark_foreground", "dark_fg"},
		{"light_foreground", "light_fg"}, {keyBrightForeground, "bright_fg"},
		{keyMagenta, "purple"}, {keyBrightMagenta, "bright_purple"},
		{keyRed, "color1"}, {keyGreen, "color2"}, {keyYellow, "color3"},
		{keyBlue, "color4"}, {keyMagenta, "color5"}, {keyCyan, "color6"},
		{keyMuted, "color8"}, {keyBrightRed, "color9"}, {keyBrightGreen, "color10"},
		{keyBrightYellow, "color11"}, {keyBrightBlue, "color12"},
		{keyBrightMagenta, "color13"}, {keyBrightCyan, "color14"}, {keyBrightForeground, "color15"},
	} {
		if p[pair[0]] == "" {
			p[pair[0]] = p[pair[1]]
		}
	}
	if p[keyBackground] == "" || p[keyForeground] == "" {
		return nil, errors.New("requires #rrggbb background and foreground (or bg/fg, color0/color7)")
	}
	alias := func(key string, fallbacks ...string) {
		for _, fallback := range fallbacks {
			if p[key] == "" {
				p[key] = p[fallback]
			}
		}
	}
	for _, key := range []string{keyRed, keyGreen, keyYellow, keyBlue, keyMagenta, keyCyan} {
		alias(key, keyForeground)
		alias("bright_"+key, key)
	}
	alias(keyAccent, keyBlue, keyForeground)
	alias(keyBrightForeground, keyForeground)
	alias(keyMuted, "dark_foreground", keyForeground)
	alias(keySelectionBackground, "selection", "color8", keyBackground)
	alias(keySelectionForeground, keyBrightForeground)
	alias(keyBorder, keySelectionBackground)
	alias(keyError, keyBrightRed, keyRed)
	alias(keyUser, keyCyan, keyAccent)
	alias(keyAssistant, keyAccent)
	alias(keyTool, keyYellow, keyAccent)
	for i, key := range []string{keyBackground, keyRed, keyGreen, keyYellow, keyBlue, keyMagenta, keyCyan, keyForeground, keyMuted, keyBrightRed, keyBrightGreen, keyBrightYellow, keyBrightBlue, keyBrightMagenta, keyBrightCyan, keyBrightForeground} {
		alias(fmt.Sprintf("color%d", i), key)
	}
	return p, nil
}
