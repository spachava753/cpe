package theme

import (
	"context"
	"math"
	"runtime"
	"testing"

	"github.com/spachava753/cpe/internal/testutil/testgate"
)

const sampleSystemAccent = "#3366cc"

func TestSystemPalette(t *testing.T) {
	for _, mode := range []string{lightTheme, darkTheme} {
		t.Run(mode, func(t *testing.T) {
			for _, test := range []struct{ name, accent string }{
				{"black", "#000000"}, {"white", "#ffffff"}, {"yellow", "#ffff00"},
				{"blue", "#0066cc"}, {"pink", "#ff0088"}, {"gray", "#808080"},
			} {
				t.Run(test.name, func(t *testing.T) {
					p, ok := systemPalette(Appearance{Mode: mode, Accent: test.accent})
					if !ok || contrast(p[keyAccent], p[keyBackground]) < 4.5 || contrast(p[keySelectionForeground], p[keySelectionBackground]) < 4.5 {
						t.Fatalf("unreadable %s palette for %s: %v", mode, test.accent, p)
					}
					if p[keyAssistant] != p[keyAccent] {
						t.Fatal("assistant role lost derived accent")
					}
				})
			}
			t.Run("invalid accent uses preset", func(t *testing.T) {
				plain, _ := presetPalette(mode)
				p, ok := systemPalette(Appearance{Mode: mode, Accent: "invalid"})
				if !ok || p[keyAccent] != plain[keyAccent] {
					t.Fatal("invalid accent must keep preset accent")
				}
			})
		})
	}
	for _, test := range []struct{ name, mode string }{{"missing mode", ""}, {"unknown mode", "unknown"}} {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := systemPalette(Appearance{Mode: test.mode, Accent: "#112233"}); ok {
				t.Fatal("missing mode must fall back to terminal")
			}
		})
	}
}

func TestRGBColor(t *testing.T) {
	for _, test := range []struct {
		name string
		rgb  []float64
		want string
	}{
		{name: "missing"},
		{name: "too few channels", rgb: []float64{0, 0}},
		{name: "too many channels", rgb: []float64{0, 0, 0, 0}},
		{name: "negative", rgb: []float64{-1, 0, 0}},
		{name: "above one", rgb: []float64{0, 1.1, 0}},
		{name: "NaN", rgb: []float64{0, math.NaN(), 0}},
		{name: "infinite", rgb: []float64{0, 0, math.Inf(1)}},
		{name: "accent", rgb: []float64{.2, .4, .8}, want: sampleSystemAccent},
		{name: "black", rgb: []float64{0, 0, 0}, want: "#000000"},
		{name: "white", rgb: []float64{1, 1, 1}, want: "#ffffff"},
		{name: "rounding", rgb: []float64{.5, .5, .5}, want: "#808080"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := rgbColor(test.rgb); got != test.want {
				t.Fatalf("rgbColor(%v) = %q, want %q", test.rgb, got, test.want)
			}
		})
	}
}

func TestDecodeMacOSAppearance(t *testing.T) {
	for _, test := range []struct {
		name, data string
		want       Appearance
	}{
		{"dark with accent", `{"mode":"dark","accent":[0.2,0.4,0.8]}`, Appearance{Mode: darkTheme, Accent: sampleSystemAccent}},
		{"invalid accent", `{"mode":"light","accent":[2,0,0]}`, Appearance{Mode: lightTheme}},
		{"missing accent", `{"mode":"light"}`, Appearance{Mode: lightTheme}},
		{"unknown mode", `{"mode":"unknown","accent":[0,0,0]}`, Appearance{}},
		{"malformed", `not JSON`, Appearance{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := decodeMacOSAppearance([]byte(test.data)); got != test.want {
				t.Fatalf("decodeMacOSAppearance() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestSystemAppearance(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "remote-session")
			if got := SystemAppearance(t.Context()); got != (Appearance{}) {
				t.Fatalf("SSH must inherit client terminal: %+v", got)
			}
		})
	}
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if got := SystemAppearance(ctx); got != (Appearance{}) {
			t.Fatal("canceled probe returned a preference")
		}
	})
}

func TestNativeSystemAppearance(t *testing.T) {
	testgate.RequireIntegration(t)
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires a supported desktop OS")
	}
	got := SystemAppearance(t.Context())
	if got.Mode == "" {
		t.Skip("desktop appearance service unavailable or no preference")
	}
	if got.Mode != lightTheme && got.Mode != darkTheme {
		t.Fatalf("invalid appearance: %+v", got)
	}
	t.Logf("native system appearance: %+v", got)
}
