package theme

import (
	"context"
	"math"
	"runtime"
	"testing"

	"github.com/spachava753/cpe/internal/testutil/testgate"
)

const sampleSystemAccent = "#3366cc"

func TestDerivedSystemPalettes(t *testing.T) {
	for _, mode := range []string{lightTheme, darkTheme} {
		for _, accent := range []string{"#000000", "#ffffff", "#ffff00", "#0066cc", "#ff0088", "#808080"} {
			p, ok := systemPalette(Appearance{Mode: mode, Accent: accent})
			if !ok || contrast(p[keyAccent], p[keyBackground]) < 4.5 || contrast(p[keySelectionForeground], p[keySelectionBackground]) < 4.5 {
				t.Fatalf("unreadable %s palette for %s: %v", mode, accent, p)
			}
			if p[keyAssistant] != p[keyAccent] {
				t.Fatal("assistant role lost derived accent")
			}
		}
		plain, _ := presetPalette(mode)
		p, ok := systemPalette(Appearance{Mode: mode, Accent: "invalid"})
		if !ok || p[keyAccent] != plain[keyAccent] {
			t.Fatal("invalid accent must keep preset accent")
		}
	}
	for _, mode := range []string{"", "unknown"} {
		if _, ok := systemPalette(Appearance{Mode: mode, Accent: "#112233"}); ok {
			t.Fatal("missing mode must fall back to terminal")
		}
	}
}

func TestAppearanceColorValidation(t *testing.T) {
	for _, rgb := range [][]float64{nil, {0, 0}, {0, 0, 0, 0}, {-1, 0, 0}, {0, 1.1, 0}, {0, math.NaN(), 0}, {0, 0, math.Inf(1)}} {
		if got := rgbColor(rgb); got != "" {
			t.Fatalf("invalid RGB accepted: %v %s", rgb, got)
		}
	}
	if got := rgbColor([]float64{.2, .4, .8}); got != sampleSystemAccent {
		t.Fatalf("incorrect sRGB conversion: %s", got)
	}
}

func TestMacOSAppearanceDecoding(t *testing.T) {
	for _, test := range []struct {
		data string
		want Appearance
	}{
		{`{"mode":"dark","accent":[0.2,0.4,0.8]}`, Appearance{Mode: darkTheme, Accent: sampleSystemAccent}},
		{`{"mode":"light","accent":[2,0,0]}`, Appearance{Mode: lightTheme}},
		{`{"mode":"light"}`, Appearance{Mode: lightTheme}},
		{`{"mode":"unknown","accent":[0,0,0]}`, Appearance{}},
		{`not JSON`, Appearance{}},
	} {
		if got := decodeMacOSAppearance([]byte(test.data)); got != test.want {
			t.Fatalf("%s: %+v, want %+v", test.data, got, test.want)
		}
	}
}

func TestAppearanceDoesNotQueryRemoteDesktop(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "remote-session")
			if got := SystemAppearance(t.Context()); got != (Appearance{}) {
				t.Fatalf("SSH must inherit client terminal: %+v", got)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := SystemAppearance(ctx); got != (Appearance{}) {
		t.Fatal("canceled probe returned a preference")
	}
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
