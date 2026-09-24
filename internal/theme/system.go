package theme

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"time"
)

const systemSource = "system"
const fileSource = "file"

// Appearance is a desktop preference snapshot. Mode is "light", "dark", or
// empty when unavailable. Accent is an optional sRGB #rrggbb color. The OS does
// not provide a complete application palette; CPE derives roles from these hints.
type Appearance struct {
	Mode   string
	Accent string
}

// SystemAppearance queries the local desktop with a one-second deadline. Linux
// uses the XDG Settings portal; macOS uses AppKit through the built-in osascript
// bridge. Unavailable services, unsupported platforms, and SSH sessions return
// an empty snapshot so the caller can inherit terminal colors. No desktop files
// are read, no preferences are changed, and no credentials are involved.
func SystemAppearance(ctx context.Context) Appearance {
	if ctx.Err() != nil || os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != "" {
		return Appearance{}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "linux":
		return portalAppearance(ctx)
	case "darwin":
		return macOSAppearance(ctx)
	default:
		return Appearance{}
	}
}

func systemPalette(a Appearance) (map[string]string, bool) {
	if a.Mode != lightTheme && a.Mode != darkTheme {
		return nil, false
	}
	p, _ := presetPalette(a.Mode)
	if hexColor.MatchString(a.Accent) {
		// Keep the OS accent recognizable while ensuring text is legible on the
		// chosen surface. The selection uses the same accent as its fill.
		accent := readableAccent(a.Accent, p[keyBackground], p[keyForeground])
		p[keyAccent], p[keyAssistant] = accent, accent
		p[keySelectionBackground] = accent
		p[keySelectionForeground] = "#000000"
		if contrast(accent, "#ffffff") > contrast(accent, "#000000") {
			p[keySelectionForeground] = "#ffffff"
		}
	}
	return p, true
}

func rgbColor(rgb []float64) string {
	if len(rgb) != 3 {
		return ""
	}
	for _, channel := range rgb {
		if math.IsNaN(channel) || math.IsInf(channel, 0) || channel < 0 || channel > 1 {
			return ""
		}
	}
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(rgb[0]*255)), int(math.Round(rgb[1]*255)), int(math.Round(rgb[2]*255)))
}

func channels(hex string) [3]float64 {
	var rgb [3]float64
	for i := range rgb {
		value, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		rgb[i] = float64(value) / 255
	}
	return rgb
}

func luminance(hex string) float64 {
	rgb := channels(hex)
	for i, value := range rgb {
		if value <= .04045 {
			rgb[i] = value / 12.92
		} else {
			rgb[i] = math.Pow((value+.055)/1.055, 2.4)
		}
	}
	return .2126*rgb[0] + .7152*rgb[1] + .0722*rgb[2]
}

func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	return (max(x, y) + .05) / (min(x, y) + .05)
}

func readableAccent(accent, background, foreground string) string {
	original, target := channels(accent), channels(foreground)
	for step := 1; contrast(accent, background) < 4.5 && step <= 100; step++ {
		amount := float64(step) / 100
		var mixed [3]float64
		for i := range mixed {
			mixed[i] = original[i]*(1-amount) + target[i]*amount
		}
		accent = rgbColor(mixed[:])
	}
	return accent
}
