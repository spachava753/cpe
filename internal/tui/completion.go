package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	modelCommand     = "/model"
	reasoningCommand = "/reasoning"
	themeCommand     = "/theme"
	usageCommand     = "/usage"
	loginCommand     = "/login"
	compactCommand   = "/compact"
	treeCommand      = "/tree"
	branchCommand    = "/branch"
	sessionCommand   = "/session"
	helpCommand      = "/help"
	quitCommand      = "/quit"
	exitCommand      = "/exit"
)

type slashCommand struct {
	name, description string
	argument          bool
}

var slashCommands = []slashCommand{
	{modelCommand, "Choose a model", false},
	{reasoningCommand, "Set reasoning effort", false},
	{themeCommand, "Choose a theme", false},
	{usageCommand, "Show session token totals", false},
	{loginCommand, "Choose a login provider", false},
	{loginDeviceCommand, "Sign in with a device code", false},
	{loginGoCommand, "Save an OpenCode Go API key", false},
	{compactCommand, "Summarize model context", false},
	{treeCommand, "List saved checkpoints", false},
	{branchCommand, "Continue from a checkpoint ID", true},
	{sessionCommand, "Show the session path", false},
	{helpCommand, "Show commands", false},
	{quitCommand, "Exit CPE", false},
	{exitCommand, "Exit CPE (alias)", false},
}

type completion struct {
	query     string
	matches   []slashCommand
	selected  int
	dismissed bool
	literal   bool
}

// syncCompletion runs after editor changes, including asynchronous paste. Only
// a leading slash on a single line, with the cursor at its end, is completed.
func (m *model) syncCompletion() {
	oldHeight, bottom := m.completionHeight(), m.scroll.following
	value := m.input.Value()
	if !strings.HasPrefix(value, "/") {
		m.completion = completion{}
	}
	c := &m.completion
	if value != c.query {
		c.selected = 0
	}
	c.query, c.matches = value, nil
	info := m.input.LineInfo()
	if m.savingDefault == nil && !m.loggingIn && m.picker == nil && !c.dismissed && m.input.LineCount() == 1 && strings.HasPrefix(value, "/") && info.StartColumn+info.ColumnOffset == utf8.RuneCountInString(value) {
		for _, command := range m.commands {
			if m.busy && (len(m.profiles) == 0 || command.name != modelCommand) {
				continue
			}
			if strings.HasPrefix(command.name, value) {
				c.matches = append(c.matches, command)
			}
		}
	}
	c.selected = min(c.selected, max(0, len(c.matches)-1))
	if m.completionHeight() != oldHeight {
		m.layout()
		m.refresh(bottom)
	}
}

func (m *model) closeCompletion(literal bool) {
	bottom := m.scroll.following
	m.completion.matches = nil
	m.completion.dismissed = true
	m.completion.literal = literal
	m.layout()
	m.refresh(bottom)
}

// completionKey reports whether it consumed the key. The submit key may fill a command
// and then fall through to normal dispatch; commands requiring an argument stay
// in the composer. Tab never executes a command.
func (m *model) completionKey(key tea.KeyPressMsg) bool {
	if m.completionHeight() == 0 {
		return false
	}
	c := &m.completion
	switch key.String() {
	case escapeKey:
		m.closeCompletion(true)
		return true
	case "up", "down":
		delta := 1
		if key.Code == tea.KeyUp {
			delta = -1
		}
		c.selected = (c.selected + delta + len(c.matches)) % len(c.matches)
		return true
	case "tab", m.submitKey.String():
		command := c.matches[c.selected]
		text := command.name
		if command.argument || key.Code == tea.KeyTab {
			text += " "
		}
		m.input.SetValue(text)
		m.closeCompletion(false)
		return command.argument || key.Code == tea.KeyTab
	}
	return false
}

func (m model) completionHeight() int {
	if m.loggingIn || m.savingDefault != nil || m.picker != nil || m.width < 24 {
		return 0
	}
	// Leave the editor, normal status/footer rows and one conversation row.
	rows := min(5, len(m.completion.matches), m.height-m.input.Height()-10)
	if rows < 1 {
		return 0
	}
	return rows + 2 // top and bottom borders
}

func (m model) completionView() string {
	count := m.completionHeight() - 2
	if count < 1 {
		return ""
	}
	width := min(64, m.width-2)
	inner := width - 2
	start := max(0, m.completion.selected-count+1)
	var rows []string
	for i := start; i < start+count; i++ {
		command := m.completion.matches[i]
		name := command.name
		if command.argument {
			name += " ID"
		}
		text := "  " + name
		style := m.styles.base
		if i == m.completion.selected {
			text = "› " + name
			style = m.styles.selection
		}
		if inner >= 40 {
			text += strings.Repeat(" ", max(1, 16-ansi.StringWidth(text))) + command.description
		}
		rows = append(rows, style.Width(inner).Render(ansi.Truncate(text, inner, "…")))
	}
	return m.styles.base.Border(lipgloss.RoundedBorder()).
		BorderForeground(color(m.theme.Colors.Border)).
		BorderBackground(color(m.theme.Colors.Background)).Render(strings.Join(rows, "\n"))
}

func (m model) completionHelp() string {
	escape := "dismiss"
	if m.busy {
		escape = "cancel work"
	}
	if m.width < 60 {
		return fmt.Sprintf("↑↓ · Tab/%s · Esc %s · %d/%d", keyLabel(m.submitKey.String()), escape, m.completion.selected+1, len(m.completion.matches))
	}
	return fmt.Sprintf("↑/↓ choose · Tab complete · %s run · Esc %s · %d/%d", keyLabel(m.submitKey.String()), escape, m.completion.selected+1, len(m.completion.matches))
}
