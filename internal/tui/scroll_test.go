package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestModelSetViewportContent(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"ASCII", "abcdefghijkl", "efgh"},
		{"wide characters", "A界BC界DE", "C界D"},
		{"styled wide characters", "\x1b[31mA界BC界DE\x1b[0m", "C界D"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{viewport: viewport.New(viewport.WithWidth(4), viewport.WithHeight(1))}
			m.setViewportContent(tc.text, false)
			m.viewport.SetYOffset(1)
			m.setViewportContent(tc.text, false)
			if ansi.Strip(m.viewport.View()) != tc.want {
				t.Fatalf("initial anchor: %q", m.viewport.View())
			}
			m.viewport.SetWidth(8)
			m.setViewportContent(tc.text, false)
			// Output at the wider size must not replace the logical anchor with
			// the start of the containing, wider row.
			m.setViewportContent(tc.text+"\nnew output", false)
			m.viewport.SetWidth(4)
			m.setViewportContent(tc.text+"\nnew output", false)
			if got := ansi.Strip(m.viewport.View()); got != tc.want || m.scroll.following {
				t.Fatalf("reflow moved the anchor: %q", got)
			}
			m.setViewportContent("replacement", false)
			if !strings.Contains("replacement", ansi.Strip(m.viewport.View())) || m.scroll.following {
				t.Fatalf("missing content was not clamped safely: %q", m.viewport.View())
			}
		})
	}
}
