package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spachava753/cpe/internal/theme"
)

type styles struct {
	base, accent, muted, border, failure lipgloss.Style
	user, assistant, tool, selection     lipgloss.Style
}

type themeUpdate struct {
	theme      theme.Theme
	appearance theme.Appearance
	err        error
	revision   uint64
}

func pollTheme(ctx context.Context, dir string, revision uint64) tea.Cmd {
	if dir == "" {
		return nil
	}
	return func() tea.Msg {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			appearance := theme.SystemAppearance(ctx)
			t, err := theme.Load(dir, appearance)
			return themeUpdate{theme: t, appearance: appearance, err: err, revision: revision}
		}
	}
}

func (m *model) configureTheme(name string) {
	if m.themeDir == "" {
		m.notice = "Error: theme configuration directory is not set"
		return
	}
	if name != "" {
		t, err := theme.Select(m.themeDir, name, m.appearance)
		if err != nil {
			m.notice = "Error: " + err.Error() + "; use /theme to choose"
			return
		}
		// An in-flight poll may still contain the theme from before this selection.
		m.themeRevision++
		m.themeError = ""
		m.applyTheme(t)
		m.notice = m.themeStatus()
		return
	}
	names, err := theme.Names(m.themeDir)
	if err != nil {
		m.notice = "Error: " + err.Error()
		return
	}
	items := make([]list.Item, 0, len(names))
	selected := 0
	for _, name := range names {
		label := name
		if name == m.theme.Name {
			selected = len(items)
			label += currentChoiceSuffix
		}
		items = append(items, choice{name, oneline(label)})
	}
	m.openPicker(themeCommand, "Choose theme · Enter saves for future sessions", items, selected)
}

func color(value string) lipgloss.TerminalColor {
	if value == "default" {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color(value)
}

func (m *model) applyTheme(t theme.Theme) {
	bottom := m.viewport.AtBottom()
	m.theme = t
	base := m.renderer.NewStyle().Foreground(color(t.Colors.Foreground)).Background(color(t.Colors.Background))
	m.styles = styles{
		base: base, accent: base.Foreground(color(t.Colors.Accent)).Bold(t.Bold),
		muted:     base.Foreground(color(t.Colors.Muted)).Italic(t.Italic),
		border:    base.Foreground(color(t.Colors.Border)),
		failure:   base.Foreground(color(t.Colors.Error)),
		user:      base.Foreground(color(t.Colors.User)).Bold(t.Bold),
		assistant: base.Foreground(color(t.Colors.Assistant)).Bold(t.Bold),
		tool:      base.Foreground(color(t.Colors.Tool)),
		selection: base.Foreground(color(t.Colors.SelectionForeground)).Background(color(t.Colors.SelectionBackground)).Bold(t.Bold),
	}
	m.spinner.Style = m.styles.accent
	m.viewport.Style = base
	m.input.FocusedStyle = textarea.Style{
		Base: base, Text: base, CursorLine: base, CursorLineNumber: m.styles.muted,
		LineNumber: m.styles.muted, EndOfBuffer: m.styles.muted,
		Placeholder: m.styles.muted, Prompt: m.styles.accent,
	}
	m.input.BlurredStyle = m.input.FocusedStyle
	// Textarea retains a pointer to the active style across value copies. Rebind
	// it after replacing styles so a live reload also updates the editor.
	m.input.Focus()
	m.input.Cursor.Style = base.Foreground(color(t.Colors.Accent))
	m.input.Cursor.TextStyle = base
	if m.picker != nil {
		m.picker.list.SetDelegate(m.pickerDelegate())
	}
	m.layout()
	m.refresh(bottom)
}

func (m model) pickerDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	d.Styles.NormalTitle = m.styles.base.PaddingLeft(2)
	d.Styles.SelectedTitle = m.styles.selection.
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(color(m.theme.Colors.Accent)).
		BorderBackground(color(m.theme.Colors.Background)).PaddingLeft(1)
	d.Styles.DimmedTitle = m.styles.muted.PaddingLeft(2)
	return d
}

func (m model) themeStatus() string {
	if m.themeError != "" {
		return "Theme error: " + m.themeError
	}
	return "Theme: " + m.theme.Name + " (" + m.theme.Source + ") · ~/.cpe/themes.json"
}

func (m model) canvas(content string) string {
	const reset = "\x1b[0m"
	view := m.styles.base.Width(m.width).Height(m.height).Render(content)
	// Lip Gloss v1 resets nested styles to the terminal defaults. Restore the
	// canvas colors after those resets so padding following a heading/tool block
	// does not show the terminal's background through an explicit theme background.
	prefix, _, _ := strings.Cut(m.styles.base.Render("x"), "x")
	if prefix != "" {
		view = strings.ReplaceAll(view, reset, reset+prefix)
		view = strings.TrimSuffix(view, prefix)
	}
	// NoColor emits no SGR code. Reset the frame boundaries so switching back to
	// terminal inheritance cannot retain colors from a previously rendered frame.
	return reset + view + reset
}
