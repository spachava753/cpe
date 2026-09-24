package theme

import (
	"context"
	"encoding/json"
	"os/exec"
)

// AppKit provides appearance and accent preferences on macOS. The system's JXA
// bridge makes these APIs available without CGO, an SDK, or a bundled helper.
// This fixed script only reads AppKit objects; it sends no automation events to
// other applications and never reads code from user configuration.
const macOSAppearanceScript = `ObjC.import('AppKit');
var app = $.NSApplication.sharedApplication;
app.setActivationPolicy($.NSApplicationActivationPolicyProhibited);
var match = app.effectiveAppearance.bestMatchFromAppearancesWithNames(
    $([$.NSAppearanceNameAqua, $.NSAppearanceNameDarkAqua]));
var mode = ObjC.unwrap(match) === ObjC.unwrap($.NSAppearanceNameDarkAqua) ? 'dark' : 'light';
var accent = $.NSColor.controlAccentColor.colorUsingColorSpace($.NSColorSpace.sRGBColorSpace);
JSON.stringify({mode: mode, accent: [accent.redComponent, accent.greenComponent, accent.blueComponent]});`

func macOSAppearance(ctx context.Context) Appearance {
	data, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", macOSAppearanceScript).Output()
	if err != nil {
		return Appearance{}
	}
	return decodeMacOSAppearance(data)
}

func decodeMacOSAppearance(data []byte) Appearance {
	var raw struct {
		Mode   string    `json:"mode"`
		Accent []float64 `json:"accent"`
	}
	if json.Unmarshal(data, &raw) != nil || (raw.Mode != lightTheme && raw.Mode != darkTheme) {
		return Appearance{}
	}
	return Appearance{Mode: raw.Mode, Accent: rgbColor(raw.Accent)}
}
